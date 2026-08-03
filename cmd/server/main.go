// Package main adalah entry point binary server Pamong.
// Satu-satunya tempat modul bisnis "dipasang" ke framework — lihat CLAUDE.md #10 — dan
// satu-satunya composition root yang merakit driven adapter konkret ke App (PR-5.1.1).
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/huda-salam/pamong/core/config"
	"github.com/huda-salam/pamong/core/domain"
	"github.com/huda-salam/pamong/gateway"
	identitydb "github.com/huda-salam/pamong/identity/adapter/db"
	identitytoken "github.com/huda-salam/pamong/identity/adapter/token"
	"github.com/huda-salam/pamong/infra/crypto"
	"github.com/huda-salam/pamong/infra/db"
	"github.com/huda-salam/pamong/infra/eventbus"
	"github.com/huda-salam/pamong/infra/idempotency"
	"github.com/huda-salam/pamong/infra/observability"
	"github.com/huda-salam/pamong/infra/ratelimit"
	"github.com/huda-salam/pamong/infra/sequence"
	"github.com/huda-salam/pamong/infra/storage"
	infrauser "github.com/huda-salam/pamong/infra/user"
	"github.com/huda-salam/pamong/modules"
	"github.com/huda-salam/pamong/port"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "pamong: gagal:", err)
		os.Exit(1)
	}
}

// run merakit dependency, mem-bootstrap modul, dan melayani HTTP sampai sinyal shutdown.
// Dipisah dari main agar error di-return (bukan os.Exit tersebar) dan alur mudah diikuti.
func run() error {
	ctx := context.Background()

	// Config berlapis (env > local > {env} > default). Tak valid → gagal cepat saat boot
	// (philosophy #4), bukan error misterius saat melayani request.
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("konfigurasi tidak valid: %w", err)
	}

	logger := observability.NewLogger(observability.LogOptions{
		Level:  cfg.Observ.LogLevel,
		Format: cfg.Observ.LogFormat,
	})
	logger.Info(ctx, "memulai pamong", port.F("env", cfg.Env), port.F("addr", cfg.HTTPAddr()))

	// --- Driven adapters (composition root) ---

	// Identity DB = koneksi bootstrap: registry tenant hidup di sini, jadi connect ke sini
	// DULU (ADR-004). Gagal di sini = gagal boot (benar: tanpa registry tak ada cara menemukan
	// DB tenant).
	identityPool, err := db.NewIdentity(ctx, cfg.IdentityDB)
	if err != nil {
		return fmt.Errorf("koneksi identity DB (bootstrap, ADR-004): %w", err)
	}
	defer identityPool.Close()

	// DB tenant: TenantConnManager me-resolve lokasi DB per tenant dari registry (lazy — pool
	// tenant dibuka on-demand saat request pertama), dibungkus TenantRoutingConn agar modul
	// menerima satu app.DB() yang otomatis memilih DB tenant dari context tiap query.
	tenantResolver := identitydb.NewTenantResolver(identitydb.NewTenantRepo(identityPool))
	connMgr := db.NewTenantConnManager(tenantResolver, cfg.DB, cfg.CentralDBResolved())
	defer connMgr.Close()
	tenantDB := db.NewTenantRoutingConn(connMgr)
	// DB sentral (entity ResidencyCentral, ADR-005) — jalur eksplisit ke pool sentral, terpisah
	// dari tenantDB yang route per-tenant (menutup DEFERRED Phase-5.1.2 #1).
	centralDB := db.NewCentralRoutingConn(connMgr)

	// Event bus (driver dari config: memory untuk dev; nats/redis untuk produksi).
	bus, err := eventbus.NewFromConfig(cfg.EventBus, eventbus.NewSchemaRegistry())
	if err != nil {
		return fmt.Errorf("event bus: %w", err)
	}

	// Storage (minio/s3/local) & metrics (Prometheus) — adapter siap pakai.
	store, err := storage.NewFromConfig(cfg.Storage)
	if err != nil {
		return fmt.Errorf("storage: %w", err)
	}
	metrics := observability.NewPrometheusMetrics()

	// --- Auth stack (PR-5.1.2) ---
	// Token verifier internal (HS256, ADR-007): verifikasi tanda tangan + jti-revocation
	// (revoked store di identity DB). Ini seam TokenVerifier yang dikonsumsi middleware auth.
	verifier := identitytoken.NewJWTCodec(
		[]byte(cfg.Auth.TokenSecret),
		cfg.Auth.TokenTTL(),
		identitydb.NewRevokedTokenStore(identityPool),
	)
	// Katalog role sentral: snapshot proses dari identity DB (dibaca sekali saat boot; gagal =
	// gagal boot, fail-fast philosophy #4 — tanpa katalog RBAC tak bisa ditegakkan).
	centralCatalog, err := identitydb.NewCentralRoleCatalog(ctx, identitydb.NewCentralRoleRepo(identityPool))
	if err != nil {
		return fmt.Errorf("katalog role sentral (RBAC): %w", err)
	}
	// Rate limiter per-principal (in-memory; swap Redis untuk multi-instance — titik ekstensi #1).
	rateLimiter := ratelimit.NewMemory(nil)
	// Generator nomor ber-urut: tabel gov.sequences per tenant DB (skema dipastikan lazy per
	// tenant). Atomik + reset per tahun — dipakai use case seperti CreateSuratMasuk untuk
	// nomor agenda. Menutup kabel nil PR-5.1.1 (gap Phase-1).
	sequenceGen := sequence.NewDBGenerator(connMgr)
	// Kripto field (ADR-009/010/017): kunci hidup di identity DB (id.data_keys) apa pun realm-nya,
	// jadi ia dirakit di atas identityPool. Dipakai lapis repository (enkripsi transparan), jalur
	// audit, DAN jalur clone tenant — pembaca clone tak bisa membuka pengenal tanpanya.
	cryptoSvc, err := crypto.NewFromConfig(cfg, identityPool)
	if err != nil {
		return fmt.Errorf("kripto field (ADR-009): %w", err)
	}
	// Resolver user: baca READ-ONLY clone gov.user_profiles pada tenant DB (tenant dari context).
	// Modul bisnis mengakses data user lewat port ini, bukan query clone langsung. Menutup kabel
	// nil PR-5.1.1 (gap Phase-2). HasCentralRole DEFERRED (butuh lookup identity DB).
	userResolver, err := infrauser.NewDBResolver(connMgr, cryptoSvc)
	if err != nil {
		return fmt.Errorf("resolver user (clone terenkripsi): %w", err)
	}
	// Store idempotency: tabel gov.idempotency_keys per tenant DB (skema dipastikan lazy per
	// tenant). Menegakkan "request mutasi duplikat → response sama tanpa efek ganda". Dirakit
	// SESUDAH cryptoSvc: badan respons tersimpan disegel (ADR-009 §6 butir 3).
	idempotencyStore, err := idempotency.NewDBStore(connMgr, cryptoSvc)
	if err != nil {
		return fmt.Errorf("store idempotency (cache respons terenkripsi): %w", err)
	}

	// Router aggregator: rute semua modul terkumpul di sini saat Bootstrap.
	router := gateway.NewRouter()

	// Health check TRIPWIRE (liveness). /healthz yang BENAR-BENAR dilayani ada di top mux
	// (auth-free, lihat perakitan server di bawah) yang membayangi (shadow) pendaftaran ini.
	// Registrasi di sini SEBELUM Bootstrap modul sengaja dipertahankan sebagai tripwire: bila
	// modul keliru mendaftar "/healthz" ke router bisnis, gateway.Router panic saat registrasi
	// — konflik ter-atribusi ke modul (fail-fast, philosophy #4). Ia tak pernah men-serve
	// request karena top mux menang; hanya menjaga namespace /healthz tetap milik framework.
	router.Get("/healthz", healthz)

	// App container. Sequence & UserResolver kini ter-wire (PR-5.1.x live-path completion) —
	// jalur request bisnis lengkap end-to-end. WorkflowRegistry masih minimal; dispatch penuh
	// via engine menyusul di slice berikutnya.
	app := domain.NewApp(
		tenantDB,             // DBConn (routing per-tenant)
		centralDB,            // CentralDB (routing ke DB sentral, ADR-005)
		bus,                  // EventPublisher
		bus,                  // EventSubscriber
		sequenceGen,          // SequenceGenerator (gov.sequences per-tenant, atomik)
		metrics,              // MetricsPort
		store,                // StoragePort
		userResolver,         // UserResolver (baca clone gov.user_profiles tenant DB)
		newWorkflowActions(), // WorkflowRegistry (minimal; dispatch penuh via engine menyusul)
		router,               // Router
	)

	// Registry modul: daftar → validasi (DAG, entity, tabel unik) → bootstrap (wiring DI &
	// registrasi rute). Semua gagal = gagal boot (philosophy #4).
	registry := domain.NewRegistry()
	registry.Register(modules.All()...)
	if err := registry.Validate(); err != nil {
		return fmt.Errorf("registry modul tidak valid: %w", err)
	}

	// Factory evaluator: bangun port.PermissionEvaluator per-request (composite central+tenant),
	// disuntik ke middleware auth agar gateway.Context.RequirePermission menegakkan RBAC live.
	// Permission strict (SoD) dikumpulkan dari manifest modul terdaftar (ADR-014); dibangun
	// SETELAH Validate agar hanya modul valid yang berkontribusi. TTL cache catalog tenant dari
	// config (refresh-on-change TTL-based; event-driven menyusul — lihat evaluator_factory.go).
	evalFactory := newEvaluatorFactory(centralCatalog, connMgr, registry.StrictPermissions(), cfg.Permission.CatalogTTL())

	for _, m := range registry.Modules() {
		if err := m.Bootstrap(ctx, app); err != nil {
			return fmt.Errorf("bootstrap modul %q: %w", m.Manifest().Name, err)
		}
		logger.Info(ctx, "modul ter-bootstrap", port.F("module", m.Manifest().Name))
	}

	// --- HTTP server + middleware stack + graceful shutdown ---
	handler := buildServerHandler(serverDeps{
		router:         router,
		verifier:       verifier,
		evalFactory:    evalFactory,
		tenantResolver: tenantResolver,
		rateLimiter:    rateLimiter,
		rateLimit:      cfg.RateLimit,
		idempotency:    idempotencyStore,
		corsOrigins:    cfg.CORS.AllowedOrigins, // allowlist dari config; kosong = same-origin only (aman)
		logger:         logger,
	})

	// Timeout menyeluruh: ReadHeaderTimeout saja tak menutup slow-body/idle Slowloris.
	// ReadTimeout membatasi pembacaan seluruh request, WriteTimeout respons, IdleTimeout
	// koneksi keep-alive menganggur.
	srv := &http.Server{
		Addr:              cfg.HTTPAddr(),
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		logger.Info(ctx, "HTTP server listening", port.F("addr", cfg.HTTPAddr()))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
	}()

	// Tunggu sinyal shutdown atau kegagalan serve.
	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case err := <-serveErr:
		return fmt.Errorf("http server: %w", err)
	case <-sigCtx.Done():
		logger.Info(ctx, "sinyal shutdown diterima; menutup server")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	logger.Info(ctx, "pamong berhenti dengan bersih")
	return nil
}

// workflowActions adalah WorkflowRegistry minimal: menyimpan pemetaan nama→use case yang
// didaftarkan modul saat Bootstrap. Dispatch/eksekusi penuh oleh workflow engine (yang butuh
// DefinitionStore DB + guard) di-wire pada slice berikutnya; di sini cukup memenuhi kontrak
// domain.WorkflowRegistry agar Bootstrap modul (yang memanggil RegisterAction) berjalan.
type workflowActions struct {
	actions map[string]any
}

func newWorkflowActions() *workflowActions {
	return &workflowActions{actions: make(map[string]any)}
}

func (w *workflowActions) RegisterAction(name string, useCase any) {
	w.actions[name] = useCase
}
