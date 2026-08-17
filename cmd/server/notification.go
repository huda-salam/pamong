// notification.go merakit tumpukan NOTIFIKASI per tenant (PR-W4b). Pasangan dari workflow.go,
// dan dengan alasan struktural yang sama: inbox in-app, template, catatan pengiriman, dan
// direktori penerima semuanya hidup di TENANT DB (gov.notifications, gov.notification_templates,
// gov.user_profiles, gov.tenant_roles) — sementara port core/notification tak membawa tenant di
// signature-nya. Isolasinya karena itu STRUKTURAL: objek yang melayani tenant A secara fisik tak
// terhubung ke DB tenant B.
//
// Dua pemakai, satu factory:
//   - workflowFactory.RuntimeFor → NotifierTransition (notifikasi transisi, jalur request)
//   - escalationHandler          → NotifierEscalator  (eskalasi SLA, jalur scheduler)
//
// Keduanya HARUS memakai tumpukan yang sama supaya notifikasi dari kedua jalur mendarat di inbox
// yang sama; itulah kenapa perakitannya di sini, bukan digandakan di masing-masing pemanggil.
package main

import (
	"context"
	"fmt"
	"sync"

	coreNotif "github.com/huda-salam/pamong/core/notification"
	coreWf "github.com/huda-salam/pamong/core/workflow"
	"github.com/huda-salam/pamong/infra/db"
	infraNotif "github.com/huda-salam/pamong/infra/notification"
	infrawf "github.com/huda-salam/pamong/infra/workflow"
	"github.com/huda-salam/pamong/port"
)

// notificationFactory merakit RoleNotifier untuk satu tenant.
//
// messaging (driver email) dan crypto sengaja DIBAGI semua tenant: keduanya adalah kemampuan
// proses, bukan data tenant. Kripto memang membaca kunci per-realm, tapi pemilihan realm terjadi
// di dalam Service berdasarkan tenant yang diminta pemanggil — bukan dengan menggandakan Service.
type notificationFactory struct {
	pools     tenantPoolProvider
	crypto    port.CryptoPort
	messaging port.MessagingPort
	metrics   port.MetricsPort
	logger    port.Logger

	mu       sync.Mutex
	prepared map[*db.Pool]struct{} // DB yang skema notifikasinya sudah dipastikan
}

func newNotificationFactory(pools tenantPoolProvider, crypto port.CryptoPort,
	messaging port.MessagingPort, metrics port.MetricsPort, logger port.Logger) *notificationFactory {
	return &notificationFactory{
		pools:     pools,
		crypto:    crypto,
		messaging: messaging,
		metrics:   metrics,
		logger:    logger,
		prepared:  make(map[*db.Pool]struct{}),
	}
}

// ForTenant merakit RoleNotifier milik satu tenant, dari pool yang di-resolve SAAT ITU.
//
// Seperti workflowFactory.RuntimeFor, tumpukannya sengaja tidak di-cache per tenant:
// db.TenantConnManager.Tenant() membaca id.tenant_registry pada setiap panggilan, jadi tumpukan
// ter-cache akan terus menulis notifikasi ke DB LAMA setelah tenant naik ke Tier 2/3 — inbox
// yang tak pernah dilihat siapa pun, tanpa satu pun error. Yang mahal (ensure schema) tetap
// sekali per DB lewat prepare.
func (f *notificationFactory) ForTenant(ctx context.Context, tenantID string) (*coreNotif.RoleNotifier, error) {
	if tenantID == "" {
		return nil, fmt.Errorf("tenant kosong: tumpukan notifikasi selalu milik satu tenant")
	}
	pool, err := f.pools.Tenant(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("pool tenant %q: %w", tenantID, err)
	}
	return f.forPool(ctx, tenantID, pool)
}

// forPool adalah ForTenant untuk pemanggil yang SUDAH me-resolve pool tenant ini.
//
// Ia ada demi satu hal yang mudah luput: TenantConnManager.Tenant() membaca id.tenant_registry
// pada SETIAP panggilan (tak ada cache — itu yang membuat kenaikan tier terlihat tanpa restart).
// workflowFactory.RuntimeFor sudah memanggilnya, jadi meminta ForTenant me-resolve ulang akan
// menambah satu query ke DB sentral pada SETIAP request workflow — termasuk transisi yang tak
// punya `notify:` sama sekali.
func (f *notificationFactory) forPool(ctx context.Context, tenantID string, pool *db.Pool) (*coreNotif.RoleNotifier, error) {
	if err := f.prepare(ctx, tenantID, pool); err != nil {
		return nil, err
	}

	// Channel registry dibangun PER TENANT karena in-app menulis ke inbox di tenant DB. Email
	// tak ber-tenant (driver proses), tapi ikut didaftarkan di sini agar satu Hub melayani
	// keduanya — Hub memilih channel dari nama, bukan dari asal perakitannya.
	channels := coreNotif.NewChannelRegistry()
	if err := channels.Register(coreNotif.NewInAppChannel(infraNotif.NewDBInAppInbox(pool))); err != nil {
		return nil, fmt.Errorf("channel in-app tenant %q: %w", tenantID, err)
	}
	if f.messaging != nil {
		if err := channels.Register(coreNotif.NewEmailChannel(f.messaging)); err != nil {
			return nil, fmt.Errorf("channel email tenant %q: %w", tenantID, err)
		}
	}

	dir, err := infraNotif.NewDBRecipientDirectory(pool, tenantID, f.crypto)
	if err != nil {
		return nil, fmt.Errorf("direktori penerima tenant %q: %w", tenantID, err)
	}
	hub := coreNotif.NewHub(
		channels,
		coreNotif.NewTemplateEngine(infraNotif.NewDBTemplateStore(pool)),
		infraNotif.NewDBDeliveryRecorder(pool),
	)
	return coreNotif.NewRoleNotifier(coreNotif.NewRouter(dir), hub), nil
}

// prepare memastikan skema notifikasi ada di DB ini — sekali per DB, bukan sekali per panggilan.
// Penandanya ber-kunci POOL (bukan tenant) dengan alasan yang sama seperti workflowFactory:
// pool identik dengan (host, dbname), jadi tenant yang pindah DB otomatis dianggap belum siap.
// Kegagalan TIDAK ditandai — percobaan berikutnya mencoba ulang.
func (f *notificationFactory) prepare(ctx context.Context, tenantID string, pool *db.Pool) error {
	f.mu.Lock()
	_, sudah := f.prepared[pool]
	f.mu.Unlock()
	if sudah {
		return nil
	}
	if err := infraNotif.EnsureSchema(ctx, pool); err != nil {
		return fmt.Errorf("schema notifikasi tenant %q: %w", tenantID, err)
	}
	if err := seedFrameworkTemplates(ctx, pool); err != nil {
		return fmt.Errorf("seed template notifikasi framework tenant %q: %w", tenantID, err)
	}
	f.mu.Lock()
	f.prepared[pool] = struct{}{}
	f.mu.Unlock()
	if f.logger != nil {
		f.logger.Info(ctx, "skema notifikasi tenant disiapkan", port.F("tenant", tenantID))
	}
	return nil
}

// TransitionNotifierFor merakit notifier transisi untuk satu tenant, dari pool yang SUDAH
// di-resolve pemanggil, dibungkus agar kegagalan notifikasi tidak menggagalkan request.
func (f *notificationFactory) TransitionNotifierFor(ctx context.Context, tenantID string,
	pool *db.Pool) (coreWf.TransitionNotifier, error) {

	notifier, err := f.forPool(ctx, tenantID, pool)
	if err != nil {
		return nil, err
	}
	return &tolerantTransitionNotifier{
		inner:   infrawf.NewNotifierTransition(notifier, coreNotif.ChannelInApp),
		metrics: f.metrics,
		logger:  f.logger,
	}, nil
}

// tolerantTransitionNotifier mencatat kegagalan notifikasi transisi lalu MENGEMBALIKAN nil.
//
// Ini bukan menelan error, dan bukan pula melemahkan kontrak core secara diam-diam. Kontrak
// coreWf.TransitionNotifier sengaja mempropagasi error "agar caller async bisa retry" — dan
// pemanggil di sini justru satu-satunya yang BUKAN async: sebuah request HTTP, dengan transisi
// yang pada titik ini sudah otoritatif dan sudah disimpan handler (gateway/workflow: instance
// di-Save lebih dulu, baru execErr dikembalikan).
//
// Jadi pilihannya bukan "gagal vs diam", melainkan: memberi tahu klien bahwa transisinya GAGAL
// padahal berhasil, atau memberi tahu operator lewat log+metrik. Yang pertama adalah kebohongan
// yang responsnya wajar — retry — justru berbahaya: aksi bisnisnya sudah dijalankan sekali.
//
// Kegagalan yang paling mungkin bukan transport, melainkan konfigurasi: gov.notification_templates
// hanya diisi seeder framework untuk template MILIK framework (lihat prepare). Template milik
// MODUL (mis. `surat_selesai` di disposisi.yaml) belum punya jalur seeding sama sekali, jadi
// ErrTemplateNotFound adalah keadaan normal hari ini — bukan insiden. Peran yang ter-binding tapi
// tak punya pemegang (ErrNoRecipient) juga. Keduanya tak boleh menjatuhkan transisi.
//
// Ketika outbox punya penulis produksi, retry asinkron yang sesungguhnya menggantikan pembungkus
// ini dan errornya kembali bermakna bagi pemanggil.
type tolerantTransitionNotifier struct {
	inner   coreWf.TransitionNotifier
	metrics port.MetricsPort
	logger  port.Logger
}

var _ coreWf.TransitionNotifier = (*tolerantTransitionNotifier)(nil)

func (n *tolerantTransitionNotifier) NotifyTransition(ctx context.Context, tenantID string,
	spec coreWf.NotifySpec, inst coreWf.WorkflowInstance) error {

	err := n.inner.NotifyTransition(ctx, tenantID, spec, inst)
	if err == nil {
		return nil
	}
	if n.metrics != nil {
		n.metrics.IncrCounter("workflow_transition_notify_failed_total", map[string]string{
			"tenant":   tenantID,
			"template": spec.Template,
			"role":     spec.ToRole,
		})
	}
	if n.logger != nil {
		n.logger.Error(ctx, "notifikasi transisi gagal; transisi TETAP tersimpan",
			port.F("tenant", tenantID),
			port.F("instance", inst.ID.String()),
			port.F("state", inst.CurrentState),
			port.F("template", spec.Template),
			port.F("role", spec.ToRole),
			port.F("err", err.Error()))
	}
	return nil
}

// seedFrameworkTemplates menanam template notifikasi MILIK FRAMEWORK sebagai default GLOBAL
// (tenant_id = ”), yang menurut DBTemplateStore.Candidates berlaku untuk semua tenant sampai
// tenant meng-override-nya dengan baris ber-tenant sendiri.
//
// Ada karena PR-W4b memperkenalkan pemakai pertama sebuah template yang tak seorang pun tulis:
// eskalasi SLA merujuk EscalationTemplateKey, dan tanpa baris ini SETIAP eskalasi di SETIAP
// tenant gagal di render — komponen yang dinyatakan hidup tapi mustahil berhasil pada instalasi
// baru mana pun. Framework yang merujuk sebuah template bertanggung jawab menyediakan defaultnya.
//
// Cakupannya berhenti di template framework. Template milik MODUL (mis. `surat_selesai` yang
// dirujuk disposisi.yaml) belum punya jalur seeding sama sekali — manifest modul tak mengenal
// notifikasi. Itu lubang nyata, dicatat di backlog ROADMAP, dan sementara ini ditutup oleh
// tolerantTransitionNotifier agar tak menjatuhkan transisi.
func seedFrameworkTemplates(ctx context.Context, pool *db.Pool) error {
	store := infraNotif.NewDBTemplateStore(pool)
	defaults := []coreNotif.Template{{
		TenantID: "", // global: berlaku semua tenant, bisa di-override per tenant
		Key:      EscalationTemplateKey,
		Locale:   coreNotif.DefaultLocale,
		Subject:  "Batas waktu terlampaui",
		Body: "Alur {{.instance_id}} masih berada di tahap \"{{.state}}\" melewati batas waktu " +
			"dan dieskalasikan kepada {{.role}}.",
	}}
	for _, t := range defaults {
		if err := store.Upsert(ctx, t); err != nil {
			return err
		}
	}
	return nil
}
