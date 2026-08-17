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
	"github.com/huda-salam/pamong/infra/db"
	infraNotif "github.com/huda-salam/pamong/infra/notification"
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
	logger    port.Logger

	mu       sync.Mutex
	prepared map[*db.Pool]struct{} // DB yang skema notifikasinya sudah dipastikan
}

func newNotificationFactory(pools tenantPoolProvider, crypto port.CryptoPort,
	messaging port.MessagingPort, logger port.Logger) *notificationFactory {
	return &notificationFactory{
		pools:     pools,
		crypto:    crypto,
		messaging: messaging,
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
	f.mu.Lock()
	f.prepared[pool] = struct{}{}
	f.mu.Unlock()
	if f.logger != nil {
		f.logger.Info(ctx, "skema notifikasi tenant disiapkan", port.F("tenant", tenantID))
	}
	return nil
}
