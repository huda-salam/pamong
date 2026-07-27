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
	"github.com/huda-salam/pamong/infra/db"
	"github.com/huda-salam/pamong/infra/eventbus"
	"github.com/huda-salam/pamong/infra/observability"
	"github.com/huda-salam/pamong/infra/storage"
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

	// Router aggregator: rute semua modul terkumpul di sini saat Bootstrap.
	router := gateway.NewRouter()

	// Health check (liveness) — tidak menyentuh DB, aman dipakai orchestrator. Didaftarkan
	// SEBELUM Bootstrap modul agar rute framework ini "menang": bila modul keliru mendaftar
	// "/healthz", ServeMux panic saat registrasi modul — konflik ter-atribusi ke modul, bukan
	// ke framework (fail-fast, philosophy #4).
	router.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// App container. Sebagian port belum punya implementasi produksi (gap fase lebih awal,
	// BUKAN lingkup PR-5.1.1) — di-wire nil dengan catatan; konstruksi modul tak men-deref-nya,
	// hanya jalur request yang memakainya yang belum lengkap:
	//   - Sequence: belum ada generator nomor — DEFERRED(Phase-1: sequence adapter).
	//   - UserResolver: belum ada adapter produksi baca gov.user_profiles — DEFERRED(Phase-2).
	app := domain.NewApp(
		tenantDB,             // DBConn (routing per-tenant)
		bus,                  // EventPublisher
		bus,                  // EventSubscriber
		nil,                  // Sequence — DEFERRED(Phase-1)
		metrics,              // MetricsPort
		store,                // StoragePort
		nil,                  // UserResolver — DEFERRED(Phase-2)
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
	for _, m := range registry.Modules() {
		if err := m.Bootstrap(ctx, app); err != nil {
			return fmt.Errorf("bootstrap modul %q: %w", m.Manifest().Name, err)
		}
		logger.Info(ctx, "modul ter-bootstrap", port.F("module", m.Manifest().Name))
	}

	// --- HTTP server + graceful shutdown ---
	// PERINGATAN KEAMANAN (PR-5.1.1): stack middleware KEAMANAN (auth/tenant/ratelimit/CORS/
	// audit) belum dipasang — itu PR-5.1.2. Sampai saat itu, rute bisnis dilayani TANPA auth
	// dan RequirePermission bersifat permisif-default (gateway.Context tanpa evaluator →
	// mengizinkan). Server ini BELUM layak deploy; hanya untuk membuktikan agregasi rute &
	// boot end-to-end. Yang dipasang di sini hanya recovery — pengaman crash dasar (bukan
	// bagian stack keamanan): panic handler → 500 anggun, bukan koneksi ter-reset.
	// Timeout menyeluruh: ReadHeaderTimeout saja tak menutup slow-body/idle Slowloris.
	// ReadTimeout membatasi pembacaan seluruh request, WriteTimeout respons, IdleTimeout
	// koneksi keep-alive menganggur.
	srv := &http.Server{
		Addr:              cfg.HTTPAddr(),
		Handler:           withRecovery(router, logger),
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

// withRecovery membungkus handler agar panic pada satu request menjadi respons 500 anggun +
// log — bukan koneksi ter-reset (HTTP 000) yang membingungkan klien. Ini pengaman crash dasar
// tingkat server, TERPISAH dari stack middleware keamanan (auth/tenant/ratelimit/audit, PR-5.1.2).
func withRecovery(next http.Handler, logger port.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rw := &recorder{ResponseWriter: w}
		defer func() {
			if rec := recover(); rec != nil {
				logger.Error(r.Context(), "panic saat melayani request",
					port.F("panic", fmt.Sprint(rec)),
					port.F("method", r.Method),
					port.F("path", r.URL.Path))
				// Hanya tulis 500 bila belum ada status/body terkirim; kalau handler sudah
				// menulis lalu panic di tengah, status sudah commit — memaksa WriteHeader(500)
				// hanya menghasilkan warning "superfluous" + body korup. Yang bisa dilakukan
				// hanyalah menghentikan penulisan (respons akan truncated, tapi ter-log).
				if !rw.wrote {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte(`{"error":"internal server error"}`))
				}
			}
		}()
		next.ServeHTTP(rw, r)
	})
}

// recorder membungkus http.ResponseWriter untuk menandai apakah status/body sudah terkirim,
// agar recovery tak menulis header ganda setelah handler menulis sebagian lalu panic.
type recorder struct {
	http.ResponseWriter
	wrote bool
}

func (r *recorder) WriteHeader(code int) {
	r.wrote = true
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(b []byte) (int, error) {
	r.wrote = true // Write implisit meng-commit 200 bila WriteHeader belum dipanggil
	return r.ResponseWriter.Write(b)
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
