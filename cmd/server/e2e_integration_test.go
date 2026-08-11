//go:build integration

// e2e_integration_test.go membuktikan tujuan Phase 5: satu request bisnis dilayani END-TO-END
// lewat stack komposisi produksi (buildServerHandler) — auth → tenant resolver → RBAC live →
// routing DB per-tenant → use case → generator nomor (gov.sequences) → repo INSERT. Inilah DoD
// PR-5.1.x (live-path completion): POST /surat-masuk dengan token valid mengembalikan 201,
// bukan 500 (kabel Sequence/UserResolver yang dulu nil kini tersambung).
//
// PR-5.1.5 menambah ujung yang selama ini putus tanpa gejala: event yang dideklarasikan
// manifest modul benar-benar SAMPAI ke subscriber. 201 saja tak pernah membuktikannya — use
// case membuang error publish, jadi event yang ditolak bus (schema tak terdaftar) tetap
// menghasilkan 201 dan baris tersimpan.
//
// Membutuhkan Postgres nyata (PAMONG_TEST_DB_DSN), satu database untuk identity (schema id.*)
// dan tenant (schema gov.*/surat_masuk.*) — tak bertabrakan karena beda schema; tenant registry
// diarahkan ke database yang sama (db_host kosong → shared host, db_name = DB test).
package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/huda-salam/pamong/core/config"
	"github.com/huda-salam/pamong/core/domain"
	"github.com/huda-salam/pamong/gateway"
	identitydb "github.com/huda-salam/pamong/identity/adapter/db"
	identitytoken "github.com/huda-salam/pamong/identity/adapter/token"
	"github.com/huda-salam/pamong/infra/crypto"
	"github.com/huda-salam/pamong/infra/db"
	"github.com/huda-salam/pamong/infra/eventbus"
	"github.com/huda-salam/pamong/infra/observability"
	"github.com/huda-salam/pamong/infra/ratelimit"
	"github.com/huda-salam/pamong/infra/sequence"
	"github.com/huda-salam/pamong/infra/storage"
	infrauser "github.com/huda-salam/pamong/infra/user"
	"github.com/huda-salam/pamong/modules"
	smdomain "github.com/huda-salam/pamong/modules/surat_masuk/domain"
	"github.com/huda-salam/pamong/port"
	tenantroledb "github.com/huda-salam/pamong/tenantrole/adapter/db"
	tenantroledomain "github.com/huda-salam/pamong/tenantrole/domain"
)

func TestE2E_CreateSuratMasuk_201(t *testing.T) {
	dsn := os.Getenv("PAMONG_TEST_DB_DSN")
	if dsn == "" {
		t.Skip("PAMONG_TEST_DB_DSN tidak diset — lewati e2e integration test")
	}
	ctx := context.Background()

	pgcfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse DSN: %v", err)
	}
	cp := pgcfg.ConnConfig

	// Pool langsung untuk reset skema, terapkan migrasi, dan seed.
	rawPool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("buka pool: %v", err)
	}
	pool := db.NewPool(rawPool)
	dropSchemas := func() {
		_, _ = pool.Exec(context.Background(), `DROP SCHEMA IF EXISTS id, gov, surat_masuk CASCADE`)
	}
	dropSchemas()
	t.Cleanup(func() { dropSchemas(); rawPool.Close() })

	// Skema faithful ke produksi: migrasi identity (id.*) + migrasi modul surat_masuk.
	applyUpMigrations(t, ctx, pool, repoPath("identity/migrations"))
	applyUpMigrations(t, ctx, pool, repoPath("modules/surat_masuk/migrations"))

	// Daftarkan tenant di registry → resolver menemukan DB tenant (di sini = DB test yang sama).
	const tenantID = "pemkot-a"
	if _, err := pool.Exec(ctx, `
		INSERT INTO id.tenant_registry (tenant_id, nama, tier, db_host, db_name, is_active)
		VALUES ($1, $2, 1, '', $3, true)`, tenantID, "Pemkot A", cp.Database); err != nil {
		t.Fatalf("seed tenant_registry: %v", err)
	}

	// --- Rakit dependency seperti run(), tapi dari DSN test ---
	idCfg := config.IdentityDBConfig{
		Host: cp.Host, Port: int(cp.Port), Name: cp.Database, User: cp.User, Password: cp.Password,
		PoolMax: 5, PoolIdle: 1,
	}
	identityPool, err := db.NewIdentity(ctx, idCfg)
	if err != nil {
		t.Fatalf("identity pool: %v", err)
	}
	t.Cleanup(identityPool.Close)

	sharedCfg := config.DBConfig{
		Host: cp.Host, Port: int(cp.Port), Name: cp.Database, User: cp.User, Password: cp.Password,
		PoolMax: 5, PoolIdle: 1,
	}
	resolver := identitydb.NewTenantResolver(identitydb.NewTenantRepo(identityPool))
	connMgr := db.NewTenantConnManager(resolver, sharedCfg, config.CentralDBConfig{})
	t.Cleanup(connMgr.Close)
	tenantDB := db.NewTenantRoutingConn(connMgr)
	centralDB := db.NewCentralRoutingConn(connMgr)

	// Seed role tenant yang MEMBERI permission surat_masuk:surat:buat (lewat repo publik yang
	// juga meng-ensure skema gov.tenant_roles pada tenant DB).
	tenantPool, err := connMgr.Tenant(ctx, tenantID)
	if err != nil {
		t.Fatalf("tenant pool: %v", err)
	}
	roleRepo := tenantroledb.NewTenantRoleRepo(tenantPool)
	if err := roleRepo.Save(ctx, &tenantroledomain.TenantRole{
		ID: uuid.New(), Name: "operator_surat", Label: "Operator Surat",
		Permissions: []string{"surat_masuk:surat:buat"},
	}); err != nil {
		t.Fatalf("seed tenant role: %v", err)
	}

	// Bus NATS nyata (server embedded), BUKAN driver memory. Alasannya bukan realisme umum:
	// pada jalur NATS, sisi TERIMA merekonstruksi payload lewat schema registry
	// (unmarshalEvent → SchemaRegistry.Unmarshal) dan MEMBUANG pesan yang schema-nya tak
	// terdaftar tanpa satu pun error. Driver memory meneruskan payload apa adanya, sehingga ia
	// tak bisa membedakan "schema modul terdaftar" dari "tidak" — persis yang diuji di sini.
	// Lagipula memory mengantar sinkron di goroutine pemanggil, jadi ia juga tak menguji
	// ketepatan waktu registrasi subscriber.
	bus, err := eventbus.NewFromConfig(
		config.EventBusConfig{Driver: "nats", URL: startEmbeddedNATS(t)},
		eventbus.NewSchemaRegistry(),
	)
	if err != nil {
		t.Fatalf("event bus: %v", err)
	}
	store, err := storage.NewFromConfig(config.StorageConfig{Driver: "local", Endpoint: t.TempDir(), Bucket: "test"})
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	metrics := observability.NewPrometheusMetrics()
	logger := observability.NewLogger(observability.LogOptions{Level: "error", Format: "text"})
	sequenceGen := sequence.NewDBGenerator(connMgr)

	// Resolver user dari clone tenant DB — dirakit sama seperti composition root produksi
	// (PR-5.1.x). Modul referensi belum meng-expose rute HTTP disposisi (use case-nya hanya
	// action workflow), jadi resolver tak terpanggil oleh POST /surat-masuk; ia di-wire di sini
	// untuk membuktikan resolver non-nil tak merusak boot/serve. Perilaku baca clone diverifikasi
	// terpisah di infra/user (db_resolver_integration_test.go).
	// Kripto field dirakit dari identityPool persis seperti run() — id.data_keys hidup di sana
	// apa pun realm-nya. Migrasi identity (termasuk 007/008) sudah diterapkan di atas, jadi
	// cukup NewFromConfig; tak perlu helper cryptokit yang justru akan menerapkannya dua kali.
	cryptoSvc, err := crypto.NewFromConfig(&config.AppConfig{
		Env: "production", // driver static = jalur produksi Tier 1/2
		Crypto: config.CryptoConfig{
			KMSDriver:   crypto.DriverStatic,
			MasterKey:   base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x5A}, 32)),
			DEKCacheTTL: time.Minute,
		},
	}, identityPool)
	if err != nil {
		t.Fatalf("crypto: %v", err)
	}
	userResolver, err := infrauser.NewDBResolver(connMgr, cryptoSvc)
	if err != nil {
		t.Fatalf("NewDBResolver: %v", err)
	}

	router := gateway.NewRouter()
	router.Get("/healthz", healthz)
	app := domain.NewApp(
		tenantDB, centralDB, bus, bus, sequenceGen, metrics, store, userResolver,
		newWorkflowActions(), router,
	)
	registry := domain.NewRegistry()
	registry.Register(modules.All()...)
	if err := registry.Validate(); err != nil {
		t.Fatalf("validate modul: %v", err)
	}
	// === Perakitan produksi yang diuji (PR-5.1.5) ===
	// Urutan sama dengan run(): SESUDAH Validate, SEBELUM Bootstrap. Yang dipanggil adalah
	// fungsi wiring produksi, bukan pendaftaran manual per-event di test — kalau daftar
	// Produces sebuah manifest hilang, test ini yang gagal.
	if err := wireModuleEventSchemas(ctx, registry, bus, logger); err != nil {
		t.Fatalf("wireModuleEventSchemas: %v", err)
	}

	// Pengamat event: berdiri di seam yang sama dengan modul konsumen (app.Subscribe →
	// Bus.Subscribe). Didaftarkan SEBELUM ada yang menerbitkan; NATSDriver.Subscribe blokir
	// sampai server mencatat SUB, jadi tak ada jendela pesan hilang.
	diterima := make(chan port.Event, 4)
	if err := bus.Subscribe(smdomain.EventSuratDiterima, func(_ context.Context, ev port.Event) error {
		diterima <- ev
		return nil
	}); err != nil {
		t.Fatalf("subscribe %s: %v", smdomain.EventSuratDiterima, err)
	}

	for _, m := range registry.Modules() {
		if err := m.Bootstrap(ctx, app); err != nil {
			t.Fatalf("bootstrap modul %q: %v", m.Manifest().Name, err)
		}
	}

	// Auth stack — secret sama dipakai untuk verifier (server) & issuer (mint token).
	secret := []byte("e2e-test-secret-0123456789-abcdef")
	revoked := identitydb.NewRevokedTokenStore(identityPool)
	verifier := identitytoken.NewJWTCodec(identitytoken.Options{Secret: secret, TTL: time.Hour, Revoked: revoked})
	centralCatalog, err := identitydb.NewCentralRoleCatalog(ctx, identitydb.NewCentralRoleRepo(identityPool))
	if err != nil {
		t.Fatalf("central catalog: %v", err)
	}
	evalFactory := newEvaluatorFactory(centralCatalog, connMgr, registry.StrictPermissions(), 0)

	handler := buildServerHandler(serverDeps{
		router:         router,
		verifier:       verifier,
		evalFactory:    evalFactory,
		tenantResolver: resolver,
		rateLimiter:    ratelimit.NewMemory(nil),
		rateLimit:      config.RateLimitConfig{Enabled: false},
		idempotency:    nil, // tanpa Idempotency-Key → middleware idempotency tak relevan
		corsOrigins:    nil,
		logger:         logger,
	})

	// Mint token employee ber-role operator_surat pada tenant pemkot-a.
	issuer := identitytoken.NewJWTCodec(identitytoken.Options{Secret: secret, TTL: time.Hour, Revoked: revoked})
	token, err := issuer.Issue(ctx, port.Claims{
		PersonID: uuid.New(), Persona: "employee", EmploymentStatus: "asn",
		TenantID: tenantID, TenantRoles: []string{"operator_surat"},
	})
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	// Field JSON = nama field CreateSuratMasukInput (decoder case-insensitive, tanpa tag).
	body := `{
		"NomorSurat":"001/IN/2025",
		"TanggalSurat":"2025-01-02T00:00:00Z",
		"TanggalAgenda":"2025-01-02T00:00:00Z",
		"Pengirim":"Dinas X",
		"Perihal":"Undangan rapat",
		"Sifat":"biasa"
	}`
	req := httptest.NewRequest(http.MethodPost, "/surat-masuk", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /surat-masuk: ingin 201, dapat %d — body: %s", rec.Code, rec.Body.String())
	}
	// Bukti generator nomor tersambung: response memuat nomor agenda ter-render pola.
	if !strings.Contains(rec.Body.String(), "2025/AG/00001") {
		t.Fatalf("response tak memuat nomor agenda ter-generate; body: %s", rec.Body.String())
	}

	// Bukti persist: baris benar-benar masuk ke tenant DB.
	var count int
	if err := tenantPool.QueryRow(ctx, `SELECT count(*) FROM surat_masuk.surat_masuks`).Scan(&count); err != nil {
		t.Fatalf("hitung surat: %v", err)
	}
	if count != 1 {
		t.Fatalf("ingin 1 surat tersimpan, dapat %d", count)
	}

	// --- Bukti event modul benar-benar terkirim (PR-5.1.5) ---
	// Sebelum registrasi schema modul, publish ini ditolak Bus ("event tak terdaftar") dan
	// use case MEMBUANG error-nya — request tetap 201, baris tetap tersimpan, dan tak ada satu
	// pun gejala. Karena itu 201 di atas bukan bukti apa pun soal event; yang membuktikan hanya
	// pesan yang benar-benar sampai ke subscriber.
	select {
	case ev := <-diterima:
		payload, ok := ev.Payload.(smdomain.SuratDiterimaPayload)
		if !ok {
			// Tipe konkret = bukti payload direkonstruksi lewat schema TERDAFTAR, bukan
			// map[string]any hasil tebakan.
			t.Fatalf("payload bertipe %T, mau smdomain.SuratDiterimaPayload", ev.Payload)
		}
		if payload.NomorAgenda != "2025/AG/00001" {
			t.Errorf("nomor agenda di payload = %q, mau 2025/AG/00001", payload.NomorAgenda)
		}
		if payload.SuratID == uuid.Nil {
			t.Error("surat_id di payload kosong — payload tak terisi dari use case")
		}
		if ev.TenantID != tenantID {
			t.Errorf("tenant_id event = %q, mau %q", ev.TenantID, tenantID)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("event surat_masuk.surat.diterima tak diterima dalam 10 detik — " +
			"schema modul tak terdaftar (publish ditolak diam-diam) atau subscriber tak aktif")
	}
}

// repoPath mengembalikan path absolut ke sub-direktori repo dari lokasi file test ini
// (cmd/server → naik dua tingkat ke root repo).
func repoPath(rel string) string {
	_, thisFile, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(thisFile), "..", "..")
	return filepath.Join(root, rel)
}

// applyUpMigrations menerapkan semua file *.up.sql pada dir (urut nama) ke pool. Faithful ke
// skema produksi tanpa meng-embed migrasi identity (SENGAJA di luar infra/schema, CLAUDE.md).
func applyUpMigrations(t *testing.T, ctx context.Context, pool *db.Pool, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("baca dir migrasi %s: %v", dir, err)
	}
	var ups []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".up.sql") {
			ups = append(ups, e.Name())
		}
	}
	sort.Strings(ups)
	for _, name := range ups {
		sqlBytes, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("baca migrasi %s: %v", name, err)
		}
		if _, err := pool.Exec(ctx, string(sqlBytes)); err != nil {
			t.Fatalf("terapkan migrasi %s: %v", name, err)
		}
	}
}
