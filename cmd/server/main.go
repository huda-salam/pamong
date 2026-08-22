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
	coreWf "github.com/huda-salam/pamong/core/workflow"
	"github.com/huda-salam/pamong/gateway"
	gatewaywf "github.com/huda-salam/pamong/gateway/workflow"
	identitydb "github.com/huda-salam/pamong/identity/adapter/db"
	identitytoken "github.com/huda-salam/pamong/identity/adapter/token"
	identitydomain "github.com/huda-salam/pamong/identity/domain"
	identityusecase "github.com/huda-salam/pamong/identity/usecase"
	"github.com/huda-salam/pamong/infra/crypto"
	"github.com/huda-salam/pamong/infra/db"
	"github.com/huda-salam/pamong/infra/eventbus"
	"github.com/huda-salam/pamong/infra/idempotency"
	"github.com/huda-salam/pamong/infra/messaging"
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
	// Schema event identity WAJIB terdaftar sebelum ada yang publish: Bus.Publish menolak nama
	// event tak terdaftar (gerbang "event tanpa schema"). Registry yang kosong membuat SELURUH
	// jalur event identity — termasuk clone ke tenant — mati sejak baris pertama. Daftarnya
	// hidup bersama konstanta event-nya (identity/domain/events.go), bukan di sini.
	if err := identitydomain.RegisterEventSchemas(bus.Schema()); err != nil {
		return fmt.Errorf("schema event identity: %w", err)
	}

	// Storage (minio/s3/local) & metrics (Prometheus) — adapter siap pakai.
	store, err := storage.NewFromConfig(cfg.Storage)
	if err != nil {
		return fmt.Errorf("storage: %w", err)
	}
	metrics := observability.NewPrometheusMetrics()

	// Transport pesan keluar: SATU driver untuk seluruh proses, dipakai OTP login (wireAuth)
	// DAN channel email notifikasi (notificationFactory). Driver dari config — `log` untuk dev
	// (DITOLAK di staging/production oleh config.Validate karena body OTP mendarat di log),
	// `smtp` untuk email nyata. Dirakit di sini, bukan di dalam masing-masing wiring: dua
	// driver SMTP paralel berarti dua kolam koneksi dan dua tempat untuk salah konfigurasi.
	messageSender, err := messaging.NewFromConfig(cfg.Messaging)
	if err != nil {
		return fmt.Errorf("driver messaging: %w", err)
	}

	// --- Auth stack (PR-5.1.2) ---
	// Token verifier internal (HS256, ADR-007): verifikasi tanda tangan + jti-revocation
	// (revoked store di identity DB). Ini seam TokenVerifier yang dikonsumsi middleware auth.
	// Ambang pagar token dihitung SEKALI di sini lalu dipakai codec DAN MaxHeaderBytes, supaya
	// keduanya mustahil menyimpang (ADR-020 Keputusan 4).
	tokenMaxBytes := effectiveTokenMaxBytes(cfg.Auth.TokenMaxBytes)
	verifier := identitytoken.NewJWTCodec(identitytoken.Options{
		Secret:  []byte(cfg.Auth.TokenSecret),
		TTL:     cfg.Auth.TokenTTL(),
		Revoked: identitydb.NewRevokedTokenStore(identityPool),
		// Pagar ukuran token (ADR-020) + observasinya. metrics & logger WAJIB di sini —
		// penolakan tanpa log/metrik hanya memindahkan kegagalan senyap dari proxy ke aplikasi.
		MaxBytes: tokenMaxBytes,
		Metrics:  metrics,
		Logger:   logger,
	})
	// Nilai EFEKTIF pagar dicatat saat boot, bukan hanya nilai yang ditulis ops. Loader sengaja
	// mengabaikan env var yang tak bisa di-parse (core/config/loader.go: satu env rusak tak boleh
	// menjatuhkan boot), jadi `GOV_AUTH_TOKEN_MAX_BYTES=16k` diam-diam menyisakan default —
	// dan justru dalam insiden "admin terkunci"-lah orang menyetel knob ini lalu perlu memastikan
	// setelannya benar-benar berlaku. Satu baris log menutup lingkar umpan-baliknya.
	logger.Info(ctx, "pagar ukuran token aktif",
		port.F("token_max_bytes", tokenMaxBytes),
		port.F("dari_config", cfg.Auth.TokenMaxBytes),
		port.F("max_header_bytes", maxHeaderBytes(tokenMaxBytes)),
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
	// Clone engine identity: subscribe identity.employment.ditugaskan → tulis gov.user_profiles
	// pada DB tenant tujuan. Ini SISI TULIS dari clone yang dibaca userResolver di atas; tanpanya
	// resolver membaca tabel yang tak pernah terisi. Lihat identity_sync.go untuk alasan tiap
	// pilihan perakitan (realm kunci, repo tanpa dekorator audit, semantik kegagalan handler).
	if err := wireIdentitySync(identityPool, connMgr, cryptoSvc, bus); err != nil {
		return fmt.Errorf("clone engine identity (identity/sync): %w", err)
	}
	// Sejak baris di atas ada subscriber terdaftar, jadi setiap jalan keluar dari run() wajib
	// menguras bus DULU. Sengaja defer, bukan panggilan di ujung alur sukses: handler clone
	// berjalan di goroutine driver dan memakai pool tenant, sementara run() bisa kembali lewat
	// banyak jalur (bootstrap modul gagal, serve gagal, Shutdown timeout) — dan justru pada
	// Shutdown yang TIMEOUT-lah handler paling mungkin masih berjalan.
	//
	// Urutannya dijamin LIFO: defer ini terdaftar SESUDAH defer penutup pool (identityPool,
	// connMgr) sehingga berjalan SEBELUM keduanya. Terbalik = pool tertutup di bawah kaki
	// handler, dan pada NATS Core pesannya hilang permanen (tak ada re-delivery).
	defer func() {
		if err := bus.Drain(); err != nil {
			logger.Error(ctx, "drain event bus gagal; ada clone yang mungkin tak tertulis",
				port.F("err", err.Error()))
			return
		}
		logger.Info(ctx, "event bus terkuras; subscriber selesai")
	}()

	// Router aggregator: rute semua modul terkumpul di sini saat Bootstrap.
	router := gateway.NewRouter()

	// Health check TRIPWIRE (liveness). /healthz yang BENAR-BENAR dilayani ada di top mux
	// (auth-free, lihat perakitan server di bawah) yang membayangi (shadow) pendaftaran ini.
	// Registrasi di sini SEBELUM Bootstrap modul sengaja dipertahankan sebagai tripwire: bila
	// modul keliru mendaftar "/healthz" ke router bisnis, gateway.Router panic saat registrasi
	// — konflik ter-atribusi ke modul (fail-fast, philosophy #4). Ia tak pernah men-serve
	// request karena top mux menang; hanya menjaga namespace /healthz tetap milik framework.
	router.Get("/healthz", healthz)

	// Registry action workflow (PR-W4a, ADR-022): SATU objek yang memenuhi dua kontrak — sisi
	// DAFTAR (domain.WorkflowRegistry yang dipakai modul saat Bootstrap) dan sisi PANGGIL
	// (workflow.ActionDispatcher yang dipakai setiap engine per-tenant). Sebelum ADR-022 kedua
	// sisi itu tak pernah bertemu: registry lama menampung `any` yang tak pernah dibaca siapa pun.
	workflowActions := coreWf.NewActionRegistry()

	// App container. Sequence & UserResolver ter-wire (PR-5.1.x live-path completion); registry
	// action workflow kini nyata & dipanggil engine (PR-W4a) — jalur request bisnis lengkap
	// end-to-end.
	app := domain.NewApp(
		tenantDB,        // DBConn (routing per-tenant)
		centralDB,       // CentralDB (routing ke DB sentral, ADR-005)
		bus,             // EventPublisher
		bus,             // EventSubscriber
		sequenceGen,     // SequenceGenerator (gov.sequences per-tenant, atomik)
		metrics,         // MetricsPort
		store,           // StoragePort
		userResolver,    // UserResolver (baca clone gov.user_profiles tenant DB)
		workflowActions, // WorkflowRegistry + ActionDispatcher (ADR-022)
		router,          // Router
	)

	// Registry modul: daftar → validasi (DAG, entity, tabel unik) → bootstrap (wiring DI &
	// registrasi rute). Semua gagal = gagal boot (philosophy #4).
	registry := domain.NewRegistry()
	registry.Register(modules.All()...)
	if err := registry.Validate(); err != nil {
		return fmt.Errorf("registry modul tidak valid: %w", err)
	}

	// Schema event MODUL (Manifest().Events.Produces) ke registry yang sama dengan event identity
	// di atas. Tanpa ini Bus.Publish menolak setiap event modul — tanpa gejala, karena use case
	// membuang error publish. Dipanggil SESUDAH Validate & SEBELUM Bootstrap; alasan urutannya di
	// module_events.go.
	if err := wireModuleEventSchemas(ctx, registry, bus, logger); err != nil {
		return fmt.Errorf("schema event modul: %w", err)
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

	// Alur auth (PR-W1): sisi PENERBIT token. Dirakit SESUDAH registry & evaluator agar seluruh
	// dependensinya (pool identity, connMgr tenant, kripto, limiter) sudah berdiri. Tanpa ini
	// server tak punya pintu masuk sama sekali — RequireAuth memagari semua rute bisnis sementara
	// tak ada cara memperoleh token. Codec yang sama dipakai sebagai issuer DAN verifier: satu
	// secret, jadi token yang diterbitkan pasti lolos verifikasi stack ini.
	// SATU gerbang concurrency bcrypt untuk seluruh proses, dibagi jalur login (wireAuth) dan
	// pembuatan kredensial (wireAdminIdentity). Gerbang per fungsi wiring akan melipatgandakan
	// batas yang justru ingin ditegakkan — lihat usecase.NewVerifyGate.
	verifyGate := identityusecase.NewVerifyGate(0, 0) // 0,0 = GOMAXPROCS slot, tunggu 2s

	authHandler, err := wireAuth(identityPool, connMgr, cryptoSvc, verifier, rateLimiter, logger,
		messageSender, verifyGate)
	if err != nil {
		return fmt.Errorf("alur auth (identity): %w", err)
	}

	// Administrasi identity (PR-W2): sisi TULIS identitas — person, employment, kredensial,
	// penugasan tenant, role sentral. Dipasang di router BISNIS (bukan top mux) agar berada di
	// balik stack lengkap termasuk RequireAuth; lihat admin_identity.go.
	//
	// `AssignEmploymentToTenant` di dalamnya adalah satu-satunya produsen
	// identity.employment.ditugaskan di server hidup. Tanpa grup ini clone engine yang ter-wire
	// di atas tak pernah menerima apa pun, dan gov.user_profiles tiap tenant tetap kosong
	// selamanya — GAP (b) PR-5.1.4. Bus yang diteruskan WAJIB bus yang sama dengan yang
	// di-subscribe wireIdentitySync.
	adminIdentity, err := wireAdminIdentity(ctx, identityPool, cryptoSvc, bus, verifyGate)
	if err != nil {
		return fmt.Errorf("admin identity: %w", err)
	}
	mountAdminIdentityRoutes(router, adminIdentity)

	// Administrasi wewenang TENANT (PR-W3b): role tenant, penugasannya ber-scope unit kerja, dan
	// delegasi/PLT. Inilah PEMANGGIL PRODUKSI pertama `RequirePermissionInUnit` — lapis ABAC yang
	// lengkap sejak PR-2.3.5 tapi tak pernah dipakai siapa pun. Evaluator-nya dipasang di
	// middleware auth (scoped_evaluator.go); tanpa grup ini evaluator itu jadi seam dorman (DoD 11).
	//
	// Audit & repo memakai tenantDB (routing per-request dari klaim token), jadi satu perakitan
	// melayani semua tenant tanpa tenant pernah datang dari body.
	adminIAM, err := wireAdminIAM(tenantDB, db.NewAuditRepo(tenantDB))
	if err != nil {
		return fmt.Errorf("administrasi wewenang tenant (tenantrole/delegation): %w", err)
	}
	mountAdminIAMRoutes(router, adminIAM)

	// Runtime workflow (PR-W4a): permukaan HTTP framework untuk memulai instance dari template
	// tenant, menjalankan transisi, dan membaca riwayatnya. Dirakit SESUDAH Bootstrap modul —
	// action yang dipanggil engine baru terdaftar di sana, dan seed definisi diambil dari manifest
	// modul yang sudah tervalidasi.
	//
	// Inilah yang membuat `app.Workflow().RegisterAction` di modul berhenti menjadi seam dorman:
	// nama action di disposisi.yaml kini benar-benar sampai ke use case (DoD 11).
	workflowSeeds, err := collectWorkflowSeeds(registry)
	if err != nil {
		return fmt.Errorf("seed workflow modul: %w", err)
	}
	// Tumpukan notifikasi per-tenant (PR-W4b): dipakai DUA jalur — notifikasi transisi (dari
	// engine, di dalam request) dan eskalasi SLA (dari scheduler, di luar request). Satu factory
	// untuk keduanya agar keduanya mendarat di inbox yang sama.
	//
	// Template baseline modul dikumpulkan & di-parse di sini juga: definisi alur modul boleh
	// merujuk `notify.template`, dan sebelum PR ini tak seorang pun menanam defaultnya —
	// setiap `notify:` milik modul gagal render di instalasi baru mana pun.
	notifSeeds, err := collectNotificationSeeds(registry, EscalationTemplateKey)
	if err != nil {
		return fmt.Errorf("seed template notifikasi modul: %w", err)
	}
	// Setiap `notify.template` di definisi alur modul WAJIB punya default. Diperiksa di sini,
	// di BOOT: tanpa ini modul berikutnya akan lupa dengan cara yang sama seperti dulu — dan
	// akibatnya baru terlihat sebagai notifikasi yang diam-diam tak sampai di instalasi baru.
	if err := validateNotifyTemplatesSeeded(workflowSeeds, notifSeeds, EscalationTemplateKey); err != nil {
		return fmt.Errorf("template notifikasi yang dirujuk alur: %w", err)
	}
	notifRuntimes := newNotificationFactory(connMgr, cryptoSvc, messageSender, notifSeeds, metrics, logger)

	// Scheduler (PR-W4b, ADR-023): tabel jadwal hidup di DB SENTRAL, jadi satu loop proses-lebar
	// melayani seluruh tenant. Pool sentral diminta EKSPLISIT di sini — bukan lewat connMgr di
	// dalam wireScheduler — supaya "scheduler membaca DB sentral" terbaca di composition root,
	// tempat keputusan residensi memang seharusnya terlihat.
	centralPool, err := connMgr.Central(ctx)
	if err != nil {
		return fmt.Errorf("pool DB sentral untuk scheduler (ADR-023): %w", err)
	}
	sched, err := wireScheduler(ctx, centralPool, connMgr, notifRuntimes,
		cfg.Scheduler.Interval(), cfg.Scheduler.LockTTL(), metrics, logger)
	if err != nil {
		return fmt.Errorf("scheduler: %w", err)
	}

	workflowRuntimes := newWorkflowFactory(connMgr, workflowActions, workflowSeeds, logger,
		sched.deadlines, notifRuntimes)
	gatewaywf.MountRoutes(router, gatewaywf.NewHandler(workflowRuntimes))
	logger.Info(ctx, "runtime workflow terpasang",
		port.F("action", workflowActions.Names()), port.F("seed_definisi", len(workflowSeeds)))

	// Loop scheduler dijalankan SESUDAH seluruh perakitan selesai: sejak baris ini ada goroutine
	// yang mengeksekusi job nyata terhadap DB tenant, dan ia tak boleh mendahului dependensinya.
	//
	// Penghentiannya sengaja defer, bukan panggilan di ujung alur sukses — run() bisa kembali
	// lewat banyak jalur (serve gagal, Shutdown timeout), dan justru pada jalur gagal itulah job
	// paling mungkin masih berjalan. Terdaftar SESUDAH defer pengurasan bus, jadi LIFO
	// menjalankannya SEBELUM bus dikuras dan (jauh) sebelum pool ditutup. Terbalik = pool
	// tertutup di bawah kaki job yang masih menulis riwayat.
	schedCtx, stopScheduler := context.WithCancel(context.Background())
	schedulerDone := sched.runner.Start(schedCtx)
	defer func() {
		stopScheduler()
		select {
		case <-schedulerDone:
			logger.Info(ctx, "scheduler berhenti; tak ada job yang tertinggal berjalan")
		case <-time.After(15 * time.Second):
			// Melewati batas berarti sebuah job menggantung. Yang benar adalah MENGATAKANNYA,
			// bukan menunggu selamanya (proses tak pernah mati) dan bukan diam (riwayat job
			// yang hilang akan terbaca sebagai job yang tak pernah jalan).
			logger.Error(ctx, "scheduler tak berhenti dalam 15s; ada job yang masih berjalan saat pool ditutup")
		}
	}()

	// --- HTTP server + middleware stack + graceful shutdown ---
	handler := buildServerHandler(serverDeps{
		router:         router,
		verifier:       verifier,
		evalFactory:    evalFactory,
		tenantResolver: tenantResolver,
		rateLimiter:    rateLimiter,
		rateLimit:      cfg.RateLimit,
		idempotency:    idempotencyStore,
		auth:           authHandler,
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
		// Batas header dinyatakan EKSPLISIT, diturunkan dari pagar token (ADR-020). Default Go
		// 1 MiB adalah nilai yang tak pernah dipilih siapa pun: ia jauh di atas batas proxy mana
		// pun (nginx 8 KiB, ALB 16 KiB) sehingga aplikasi tak pernah jadi yang menolak — dan
		// sekaligus mengizinkan satu klien menahan 1 MiB buffer per koneksi.
		MaxHeaderBytes: maxHeaderBytes(tokenMaxBytes),
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
	// Pengurasan bus terjadi di defer (lihat sesudah wireIdentitySync), jadi ia tetap berjalan
	// pada jalur gagal — termasuk `return` di atas saat Shutdown timeout.
	logger.Info(ctx, "server HTTP berhenti; menguras event bus")
	return nil
}

// maxHeaderBytes menghitung batas header HTTP dari pagar ukuran token (ADR-020). Keduanya harus
// KOHEREN: token yang lolos pagar wajib bisa dikirim kembali sebagai "Authorization: Bearer …",
// jadi batas header tak boleh lebih rendah dari ambang token. Bila ops menaikkan ambang token ke
// 16 KiB untuk deployment di belakang ALB tanpa batas header ikut naik, hasilnya adalah pagar
// KEDUA yang menolak token yang baru saja dinyatakan sah (431) — kegagalan yang sama
// membingungkannya dengan yang hendak dicegah, hanya berpindah tempat.
//
// PENTING soal satuan yang dibandingkan: `MaxHeaderBytes` membatasi request line + SELURUH header
// digabung (Go menambah ±4 KiB slop internal), sementara batas proxy yang kita jadikan acuan
// (nginx `large_client_header_buffers` 8 KiB, ALB 16 KiB) berlaku PER header. Jadi slack di sini
// tidak boleh sekadar sebesar prefiks "Authorization: Bearer ": ia harus memuat semua header LAIN
// di request yang sama — Cookie (bisa beberapa KiB), Referer, User-Agent, header trace. 16 KiB
// dipilih untuk itu, dan lantai 32 KiB menjaga konfigurasi bawaan tetap lebih longgar dari total
// header request nyata mana pun yang wajar. Keduanya tetap belasan kali lebih ketat dari default
// Go 1 MiB — setengah alasan nilai ini dinyatakan eksplisit adalah agar satu klien tak bisa
// menahan buffer sebesar itu per koneksi.
func maxHeaderBytes(tokenMaxBytes int) int {
	tokenMaxBytes = effectiveTokenMaxBytes(tokenMaxBytes)
	const (
		floor = 32 << 10 // total header request nyata + token pada ambang default
		slack = 16 << 10 // ruang untuk SEMUA header selain Authorization (Cookie dsb)
	)
	if n := tokenMaxBytes + slack; n > floor {
		return n
	}
	return floor
}

// effectiveTokenMaxBytes menerjemahkan nilai config menjadi ambang yang BENAR-BENAR berlaku:
// 0/negatif → default adapter token, dan dikurung pada plafon yang sama dengan validasi config.
//
// Kurungan itu bukan duplikasi validasi. config.Validate sudah menolak nilai di atas plafon, tapi
// fungsi ini tak boleh bergantung padanya: pemanggil lain (test, tooling) bisa memberi angka apa
// pun, dan tanpa kurungan `tokenMaxBytes + slack` di maxHeaderBytes MELUAP jadi negatif — batas
// header lalu jatuh ke floor sementara pagar token praktis mati, yaitu aplikasi menolak token yang
// ia sendiri terbitkan.
func effectiveTokenMaxBytes(tokenMaxBytes int) int {
	if tokenMaxBytes <= 0 {
		return identitytoken.DefaultMaxBytes
	}
	if tokenMaxBytes > config.MaxTokenMaxBytes {
		return config.MaxTokenMaxBytes
	}
	return tokenMaxBytes
}
