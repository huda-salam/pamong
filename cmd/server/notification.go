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
	"io/fs"
	"sync"

	"github.com/huda-salam/pamong/core/domain"
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

	// moduleTemplates adalah default GLOBAL milik modul, sudah di-parse & divalidasi saat BOOT
	// (collectNotificationSeeds). Disimpan sebagai nilai, bukan ref: file YAML yang rusak harus
	// menjatuhkan boot, bukan menunggu tenant pertama memakai notifikasi.
	moduleTemplates []coreNotif.Template

	mu       sync.Mutex
	prepared map[*db.Pool]struct{} // DB yang skema notifikasinya sudah dipastikan
}

func newNotificationFactory(pools tenantPoolProvider, crypto port.CryptoPort,
	messaging port.MessagingPort, moduleTemplates []coreNotif.Template,
	metrics port.MetricsPort, logger port.Logger) *notificationFactory {
	return &notificationFactory{
		pools:           pools,
		crypto:          crypto,
		messaging:       messaging,
		moduleTemplates: moduleTemplates,
		metrics:         metrics,
		logger:          logger,
		prepared:        make(map[*db.Pool]struct{}),
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
	if err := seedTemplates(ctx, pool, f.moduleTemplates); err != nil {
		return fmt.Errorf("seed template notifikasi modul tenant %q: %w", tenantID, err)
	}
	f.mu.Lock()
	f.prepared[pool] = struct{}{}
	f.mu.Unlock()
	if f.logger != nil {
		f.logger.Info(ctx, "skema & seed notifikasi tenant disiapkan",
			port.F("tenant", tenantID), port.F("seed_template_modul", len(f.moduleTemplates)))
	}
	return nil
}

// TransitionNotifierFor mengembalikan notifier transisi untuk satu tenant, di atas pool yang SUDAH
// di-resolve pemanggil. Tak mengembalikan error, dan perakitannya TERTUNDA sampai ada transisi
// yang benar-benar ber-`notify:`.
//
// Keduanya disengaja. Merakit di muka berarti ensure-schema + seed notifikasi ikut dijalankan
// pada SETIAP pembangunan runtime workflow — termasuk untuk GET riwayat instance — dan
// kegagalannya (DB notifikasi bermasalah) akan menjatuhkan SELURUH endpoint workflow tenant itu,
// yang read-only sekalipun. Notifikasi adalah efek samping satu transisi; ia tak boleh menjadi
// syarat hidup permukaan yang tak memakainya.
func (f *notificationFactory) TransitionNotifierFor(tenantID string, pool *db.Pool) coreWf.TransitionNotifier {
	return &tolerantTransitionNotifier{
		build: func(ctx context.Context) (coreWf.TransitionNotifier, error) {
			n, err := f.forPool(ctx, tenantID, pool)
			if err != nil {
				return nil, err
			}
			return infrawf.NewNotifierTransition(n, coreNotif.ChannelInApp), nil
		},
		metrics: f.metrics,
		logger:  f.logger,
	}
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
	// build merakit notifier sebenarnya saat pertama dibutuhkan. Kegagalan perakitan
	// diperlakukan SAMA dengan kegagalan pengiriman — keduanya sama-sama tak boleh menjatuhkan
	// transisi yang sudah tersimpan.
	build   func(context.Context) (coreWf.TransitionNotifier, error)
	inner   coreWf.TransitionNotifier // dipakai langsung bila diset (test); jika nil, dari build
	metrics port.MetricsPort
	logger  port.Logger
}

var _ coreWf.TransitionNotifier = (*tolerantTransitionNotifier)(nil)

func (n *tolerantTransitionNotifier) NotifyTransition(ctx context.Context, tenantID string,
	spec coreWf.NotifySpec, inst coreWf.WorkflowInstance) error {

	inner := n.inner
	if inner == nil {
		var err error
		if inner, err = n.build(ctx); err != nil {
			n.laporkan(ctx, tenantID, spec, inst, err)
			return nil
		}
	}
	err := inner.NotifyTransition(ctx, tenantID, spec, inst)
	if err == nil {
		return nil
	}
	n.laporkan(ctx, tenantID, spec, inst, err)
	return nil
}

// laporkan mencatat kegagalan notifikasi ke metrik & log — satu-satunya jejak yang tersisa, karena
// error-nya sengaja tidak dikembalikan ke pemanggil.
func (n *tolerantTransitionNotifier) laporkan(ctx context.Context, tenantID string,
	spec coreWf.NotifySpec, inst coreWf.WorkflowInstance, err error) {

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
// Cakupannya berhenti di template FRAMEWORK. Template milik MODUL punya jalur sendiri —
// domain.NotificationRef di manifest → collectNotificationSeeds saat boot → seedTemplates di
// prepare — karena sumbernya berbeda: yang ini dirujuk kode framework, yang itu dirujuk definisi
// alur milik modul. Menggabungkannya berarti framework harus tahu template modul mana yang ada.
func seedFrameworkTemplates(ctx context.Context, pool *db.Pool) error {
	defaults := []coreNotif.Template{{
		TenantID: "", // global: berlaku semua tenant, bisa di-override per tenant
		Key:      EscalationTemplateKey,
		Locale:   coreNotif.DefaultLocale,
		Subject:  "Batas waktu terlampaui",
		Body: "Alur {{.instance_id}} masih berada di tahap \"{{.state}}\" melewati batas waktu " +
			"dan dieskalasikan kepada {{.role}}.",
	}}
	return seedTemplates(ctx, pool, defaults)
}

// seedTemplates menanam sekumpulan template ke DB ini bila belum ada.
//
// InsertIfAbsent, BUKAN Upsert: seeder ini jalan di setiap boot proses. Upsert akan
// mengembalikan template ke bunyi bawaan tiap restart — menghapus suntingan operator tanpa
// jejak. Pola yang sama dengan coreWf.SeedIfAbsent untuk definisi workflow.
func seedTemplates(ctx context.Context, pool *db.Pool, tmpls []coreNotif.Template) error {
	if len(tmpls) == 0 {
		return nil
	}
	store := infraNotif.NewDBTemplateStore(pool)
	for _, t := range tmpls {
		if _, err := store.InsertIfAbsent(ctx, t); err != nil {
			return fmt.Errorf("template %q locale %q: %w", t.Key, t.LocaleOrDefault(), err)
		}
	}
	return nil
}

// validateNotifyTemplatesSeeded memastikan SETIAP `notify.template` yang dirujuk definisi alur
// modul benar-benar punya default yang diseed — dan menjatuhkan BOOT bila tidak.
//
// Ini yang mengubah lubang lama dari "developer harus ingat" menjadi "mustahil lupa". Sebelum
// jalur seeding modul ada, disposisi.yaml merujuk template yang tak seorang pun tulis dan
// akibatnya baru terlihat sebagai ErrTemplateNotFound pada transisi pertama di instalasi baru —
// setelah rilis, hanya di satu jalur, dan (karena kegagalan notifikasi sengaja tidak menjatuhkan
// transisi) tanpa satu pun permintaan yang gagal. Menambal satu modul tidak menutup itu; modul
// berikutnya akan lupa dengan cara yang sama.
//
// Diperiksa terhadap GABUNGAN template modul + key milik framework: alur boleh merujuk template
// framework, dan modul boleh (kelak) merujuk template modul lain. Yang ditolak hanyalah rujukan
// yang tak punya default di mana pun.
//
// Override tenant sengaja tidak dihitung: baseline harus lengkap sendiri. Tenant yang menulis
// barisnya sendiri adalah penyesuaian, bukan syarat agar instalasi baru berfungsi.
func validateNotifyTemplatesSeeded(workflowSeeds []domain.WorkflowRef,
	moduleTemplates []coreNotif.Template, frameworkKeys ...string) error {

	tersedia := make(map[string]struct{}, len(moduleTemplates)+len(frameworkKeys))
	for _, t := range moduleTemplates {
		tersedia[t.Key] = struct{}{}
	}
	for _, k := range frameworkKeys {
		tersedia[k] = struct{}{}
	}

	for _, ref := range workflowSeeds {
		data, err := fs.ReadFile(ref.FS, ref.Path)
		if err != nil {
			return fmt.Errorf("baca definisi alur %q: %w", ref.Path, err)
		}
		def, err := coreWf.ParseYAML(data)
		if err != nil {
			return fmt.Errorf("definisi alur %q: %w", ref.Path, err)
		}
		for _, tr := range def.Transitions {
			if tr.Notify == nil || tr.Notify.Template == "" {
				continue
			}
			if _, ada := tersedia[tr.Notify.Template]; !ada {
				return fmt.Errorf(
					"alur %q (%s) transisi %s→%s merujuk template notifikasi %q yang tak punya default; "+
						"tambahkan entri di file NotificationRef modul",
					def.ID, ref.Path, tr.From, tr.To, tr.Notify.Template)
			}
		}
	}
	return nil
}

// collectNotificationSeeds mengumpulkan + mem-parse template baseline dari manifest seluruh
// modul terdaftar. Dijalankan SEKALI saat boot.
//
// Di-parse di muka, bukan disimpan sebagai ref lalu di-parse per tenant seperti WorkflowRef:
// file YAML yang rusak atau key yang salah namespace harus menjatuhkan BOOT, bukan menunggu
// tenant pertama memakai notifikasi. Kegagalan yang muncul pada satu tenant saja — berjam-jam
// setelah rilis, hanya di jalur `notify:` — adalah kelas kegagalan yang paling mahal.
//
// Tabrakan key ANTAR modul ditolak di sini. Template modul di-seed sebagai baris global, jadi
// seluruh modul berbagi satu ruang nama; InsertIfAbsent membuat tabrakan itu diam (yang lebih
// dulu menang). ParseYAML sudah menegakkan awalan `{modul}.` sehingga tabrakan jujur mustahil —
// pemeriksaan ini menangkap sisanya: dua modul bernama sama, atau satu modul yang mendaftarkan
// file yang sama dua kali.
func collectNotificationSeeds(reg *domain.Registry) ([]coreNotif.Template, error) {
	var out []coreNotif.Template
	asal := make(map[string]string) // "key\x00locale" → "modul:path"
	for _, m := range reg.Modules() {
		mf := m.Manifest()
		for _, ref := range mf.Notifications {
			if ref.FS == nil {
				return nil, fmt.Errorf("modul %q: NotificationRef %q tanpa FS ter-embed", mf.Name, ref.Path)
			}
			if ref.Path == "" {
				return nil, fmt.Errorf("modul %q: NotificationRef tanpa Path", mf.Name)
			}
			tmpls, err := coreNotif.ParseFS(ref.FS, mf.Name, ref.Path)
			if err != nil {
				return nil, fmt.Errorf("modul %q: %w", mf.Name, err)
			}
			for _, t := range tmpls {
				id := t.Key + "\x00" + t.LocaleOrDefault()
				if lain, dup := asal[id]; dup {
					return nil, fmt.Errorf(
						"template %q locale %q didefinisikan dua kali: %s dan %s:%s",
						t.Key, t.LocaleOrDefault(), lain, mf.Name, ref.Path)
				}
				asal[id] = mf.Name + ":" + ref.Path
				out = append(out, t)
			}
		}
	}
	return out, nil
}
