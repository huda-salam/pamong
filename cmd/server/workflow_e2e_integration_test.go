//go:build integration

// workflow_e2e_integration_test.go membuktikan DoD PR-W4a: satu surat didisposisi LEWAT
// WORKFLOW — dari template yang dipilih tenant, lewat stack komposisi produksi
// (buildServerHandler) — dan disposisinya benar-benar tersimpan di tenant DB.
//
// Yang dibuktikan di sini tak bisa dibuktikan test per-komponen mana pun:
//
//  1. Nama action di disposisi.yaml ("DisposisiSurat") benar-benar sampai ke use case. Sebelum
//     ADR-022, registry action menampung `any` yang TIDAK PERNAH dibaca siapa pun — engine dan
//     modul saling terdaftar tanpa satu pun jalur pemanggilan di antaranya.
//  2. Definisi baseline modul (YAML ter-embed) ter-seed ke DB TENANT, bukan ke satu store
//     proses-lebar yang kebetulan lolos test unit.
//  3. Instance bertahan antar REQUEST: transisi terjadi di request yang berbeda dari start.
//
// Membutuhkan Postgres nyata (PAMONG_TEST_DB_DSN) — lihat catatan di e2e_integration_test.go.
package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/huda-salam/pamong/core/config"
	"github.com/huda-salam/pamong/core/domain"
	coreWf "github.com/huda-salam/pamong/core/workflow"
	"github.com/huda-salam/pamong/gateway"
	gatewaywf "github.com/huda-salam/pamong/gateway/workflow"
	identitydb "github.com/huda-salam/pamong/identity/adapter/db"
	identitytoken "github.com/huda-salam/pamong/identity/adapter/token"
	identitysync "github.com/huda-salam/pamong/identity/sync"
	"github.com/huda-salam/pamong/infra/crypto"
	"github.com/huda-salam/pamong/infra/db"
	"github.com/huda-salam/pamong/infra/eventbus"
	"github.com/huda-salam/pamong/infra/observability"
	"github.com/huda-salam/pamong/infra/ratelimit"
	"github.com/huda-salam/pamong/infra/sequence"
	"github.com/huda-salam/pamong/infra/storage"
	infrauser "github.com/huda-salam/pamong/infra/user"
	infrawf "github.com/huda-salam/pamong/infra/workflow"
	"github.com/huda-salam/pamong/modules"
	smdomain "github.com/huda-salam/pamong/modules/surat_masuk/domain"
	"github.com/huda-salam/pamong/port"
	tenantroledb "github.com/huda-salam/pamong/tenantrole/adapter/db"
	tenantroledomain "github.com/huda-salam/pamong/tenantrole/domain"
)

func TestE2E_DisposisiLewatWorkflow(t *testing.T) {
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

	applyUpMigrations(t, ctx, pool, repoPath("identity/migrations"))
	applyUpMigrations(t, ctx, pool, repoPath("modules/surat_masuk/migrations"))

	const (
		tenantID = "pemkot-wf"
		slot     = "surat_masuk.disposisi"
	)
	if _, err := pool.Exec(ctx, `
		INSERT INTO id.tenant_registry (tenant_id, nama, tier, db_host, db_name, is_active)
		VALUES ($1, $2, 1, '', $3, true)`, tenantID, "Pemkot WF", cp.Database); err != nil {
		t.Fatalf("seed tenant_registry: %v", err)
	}

	// --- Dependency seperti run(), dari DSN test ---
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

	tenantPool, err := connMgr.Tenant(ctx, tenantID)
	if err != nil {
		t.Fatalf("tenant pool: %v", err)
	}

	// Role tenant dengan SELURUH permission yang dibutuhkan alur ini: membuat surat, memakai
	// runtime workflow, dan mendisposisi (yang dicek DUA KALI — guard di definisi dan use case).
	roleRepo := tenantroledb.NewTenantRoleRepo(tenantPool)
	if err := roleRepo.Save(ctx, &tenantroledomain.TenantRole{
		ID: uuid.New(), Name: "operator_surat", Label: "Operator Surat",
		Permissions: []string{
			smdomain.PermSuratBuat,
			smdomain.PermSuratDisposisi,
			coreWf.PermInstanceMulai,
			coreWf.PermInstanceTransisi,
			coreWf.PermInstanceBaca,
		},
	}); err != nil {
		t.Fatalf("seed tenant role: %v", err)
	}

	bus, err := eventbus.NewFromConfig(
		config.EventBusConfig{Driver: "memory"}, eventbus.NewSchemaRegistry())
	if err != nil {
		t.Fatalf("event bus: %v", err)
	}
	store, err := storage.NewFromConfig(config.StorageConfig{
		Driver: "local", Endpoint: t.TempDir(), Bucket: "test"})
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	metrics := observability.NewPrometheusMetrics()
	logger := observability.NewLogger(observability.LogOptions{Level: "error", Format: "text"})
	sequenceGen := sequence.NewDBGenerator(connMgr)

	cryptoSvc, err := crypto.NewFromConfig(&config.AppConfig{
		Env: "production",
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

	// Clone user aktor: DisposisiSurat meresolusi jabatan pendisposisi lewat UserResolver, jadi
	// tanpa baris ini transisi gagal NOT_FOUND. Ditulis lewat penulis clone PRODUKSI agar realm
	// kunci & blind index-nya sama persis dengan yang dibaca resolver.
	personID := uuid.New()
	cloneWriter, err := identitysync.NewTenantDBWriter(connMgr, cryptoSvc)
	if err != nil {
		t.Fatalf("clone writer: %v", err)
	}
	if err := cloneWriter.Upsert(ctx, tenantID, identitysync.UserProfileClone{
		PersonID: personID, AssignmentID: uuid.New(),
		NIK: "3578010101900001", NIP: "199001012015031001",
		NamaLengkap: "Budi Santoso", EmploymentStatus: "asn",
	}); err != nil {
		t.Fatalf("seed clone user: %v", err)
	}

	// --- Perakitan yang diuji: sama urutannya dengan run() ---
	router := gateway.NewRouter()
	workflowActions := coreWf.NewActionRegistry()
	app := domain.NewApp(tenantDB, centralDB, bus, bus, sequenceGen, metrics, store,
		userResolver, workflowActions, router)

	registry := domain.NewRegistry()
	registry.Register(modules.All()...)
	if err := registry.Validate(); err != nil {
		t.Fatalf("validate modul: %v", err)
	}
	if err := wireModuleEventSchemas(ctx, registry, bus, logger); err != nil {
		t.Fatalf("wireModuleEventSchemas: %v", err)
	}
	for _, m := range registry.Modules() {
		if err := m.Bootstrap(ctx, app); err != nil {
			t.Fatalf("bootstrap modul %q: %v", m.Manifest().Name, err)
		}
	}

	workflowSeeds, err := collectWorkflowSeeds(registry)
	if err != nil {
		t.Fatalf("collectWorkflowSeeds: %v", err)
	}
	if len(workflowSeeds) == 0 {
		t.Fatal("tak ada seed workflow dari manifest modul — DoD tak mungkin terpenuhi")
	}
	// deadlines & notifier sengaja nil di sini: test ini menguji jalur DISPATCH ACTION (PR-W4a).
	// Jalur SLA + notifikasi dirakit penuh dan dibuktikan di sla_notification_e2e_integration_test.go
	// (PR-W4b), yang butuh pool DB SENTRAL untuk tabel scheduler (ADR-023).
	runtimes := newWorkflowFactory(connMgr, workflowActions, workflowSeeds, logger, nil, nil)
	gatewaywf.MountRoutes(router, gatewaywf.NewHandler(runtimes))

	// Bangun tumpukan tenant sekali di muka supaya skema workflow ada & definisi baseline
	// ter-seed; sesudah itu barulah pilihan template tenant bisa ditetapkan.
	if _, err := runtimes.RuntimeFor(port.WithTenant(ctx, tenantID), tenantID); err != nil {
		t.Fatalf("bangun tumpukan workflow tenant: %v", err)
	}

	// Bukti seed mendarat di DB TENANT (bukan sekadar di memori proses).
	var jumlahDef int
	if err := tenantPool.QueryRow(ctx,
		`SELECT count(*) FROM gov.workflow_definitions WHERE workflow_id = $1`,
		"surat_masuk.disposisi.standar").Scan(&jumlahDef); err != nil {
		t.Fatalf("hitung definisi: %v", err)
	}
	if jumlahDef == 0 {
		t.Fatal("definisi baseline modul tak ter-seed ke tenant DB")
	}

	// Pilihan template tenant untuk slot ini (jalur admin ber-tata-kelola = TemplateChoiceManager;
	// di sini cukup jalur seed/framework).
	templates := infrawf.NewDBTemplateStore(tenantPool, infrawf.NewDBStore(tenantPool))
	if err := templates.SetTenantTemplate(coreWf.TenantWorkflowConfig{
		TenantID: tenantID, Slot: slot, TemplateID: "surat_masuk.disposisi.standar",
		RoleBindings: map[string]string{"sekretaris_daerah": "operator_surat"},
	}); err != nil {
		t.Fatalf("set pilihan template tenant: %v", err)
	}

	// --- Stack HTTP produksi ---
	secret := []byte("e2e-wf-secret-0123456789-abcdefgh")
	revoked := identitydb.NewRevokedTokenStore(identityPool)
	codec := identitytoken.NewJWTCodec(identitytoken.Options{
		Secret: secret, TTL: time.Hour, Revoked: revoked})
	centralCatalog, err := identitydb.NewCentralRoleCatalog(ctx, identitydb.NewCentralRoleRepo(identityPool))
	if err != nil {
		t.Fatalf("central catalog: %v", err)
	}
	handler := buildServerHandler(serverDeps{
		router:         router,
		verifier:       codec,
		evalFactory:    newEvaluatorFactory(centralCatalog, connMgr, registry.StrictPermissions(), 0),
		tenantResolver: resolver,
		rateLimiter:    ratelimit.NewMemory(nil),
		rateLimit:      config.RateLimitConfig{Enabled: false},
		logger:         logger,
	})
	token, err := codec.Issue(ctx, port.Claims{
		PersonID: personID, Persona: "employee", EmploymentStatus: "asn",
		TenantID: tenantID, TenantRoles: []string{"operator_surat"},
	})
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	call := func(method, target, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	// 1. Buat surat (entitas yang akan dikelola alur).
	rec := call(http.MethodPost, "/surat-masuk", `{
		"NomorSurat":"010/IN/2025",
		"TanggalSurat":"2025-03-04T00:00:00Z",
		"TanggalAgenda":"2025-03-04T00:00:00Z",
		"Pengirim":"Dinas Y",
		"Perihal":"Permohonan data",
		"Sifat":"biasa"
	}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /surat-masuk: %d — %s", rec.Code, rec.Body.String())
	}
	var surat struct {
		ID uuid.UUID `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &surat); err != nil || surat.ID == uuid.Nil {
		t.Fatalf("id surat tak terbaca dari respons: %v — %s", err, rec.Body.String())
	}

	// 2. Mulai instance dari template TENANT.
	rec = call(http.MethodPost, "/workflow/instances",
		`{"slot":"`+slot+`","entity_id":"`+surat.ID.String()+`"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /workflow/instances: %d — %s", rec.Code, rec.Body.String())
	}
	var started struct {
		ID           uuid.UUID `json:"id"`
		CurrentState string    `json:"current_state"`
		DefinitionID string    `json:"definition_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &started); err != nil {
		t.Fatalf("respons start tak terbaca: %v — %s", err, rec.Body.String())
	}
	if started.CurrentState != "diterima" || started.DefinitionID != "surat_masuk.disposisi.standar" {
		t.Fatalf("instance awal = %+v", started)
	}

	// 3. Transisi "disposisi" di REQUEST TERPISAH — inilah yang menuntut instance bertahan di DB.
	rec = call(http.MethodPost, "/workflow/instances/"+started.ID.String()+"/transitions",
		`{"action":"disposisi","params":{"kepada_jabatan":"kabag_umum","instruksi":"tindak lanjuti"},"comment":"prioritas"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST transitions: %d — %s", rec.Code, rec.Body.String())
	}
	var after struct {
		CurrentState string `json:"current_state"`
		History      []struct {
			From, To, Action, Comment string
		} `json:"history"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &after); err != nil {
		t.Fatalf("respons transisi tak terbaca: %v — %s", err, rec.Body.String())
	}
	if after.CurrentState != "didisposisi" {
		t.Fatalf("current_state = %q, ingin didisposisi", after.CurrentState)
	}
	if len(after.History) != 1 || after.History[0].Action != "DisposisiSurat" ||
		after.History[0].Comment != "prioritas" {
		t.Fatalf("riwayat transisi tak sesuai: %+v", after.History)
	}

	// 4. BUKTI INTI: action benar-benar memanggil use case — barisnya ada di tenant DB, dengan
	// params yang dikirim aktor dan surat dari INSTANCE (bukan dari body transisi).
	var jumlahDisposisi int
	if err := tenantPool.QueryRow(ctx,
		`SELECT count(*) FROM surat_masuk.disposisis`).Scan(&jumlahDisposisi); err != nil {
		t.Fatalf("hitung disposisi: %v", err)
	}
	if jumlahDisposisi != 1 {
		t.Fatalf("jumlah disposisi = %d, ingin 1 — action workflow tak memanggil use case", jumlahDisposisi)
	}
	var (
		kepada   string
		suratRef uuid.UUID
	)
	if err := tenantPool.QueryRow(ctx,
		`SELECT kepada_jabatan, surat_id FROM surat_masuk.disposisis`).
		Scan(&kepada, &suratRef); err != nil {
		t.Fatalf("baca disposisi: %v", err)
	}
	if kepada != "kabag_umum" {
		t.Errorf("kepada_jabatan = %q, ingin kabag_umum (params aktor tak sampai ke use case)", kepada)
	}
	if suratRef != surat.ID {
		t.Errorf("surat_id disposisi = %v, ingin %v (entity dari instance)", suratRef, surat.ID)
	}

	// 5. Instance tersimpan dengan state & versi optimistic lock yang benar: satu tulis saat
	// start, satu saat transisi. Saling-meniadakan transisi bersamaan dijaga kunci per-instance
	// (TryLockInstance), bukan dengan menulis dua kali.
	var (
		stateDB   string
		versionDB int
	)
	if err := tenantPool.QueryRow(ctx,
		`SELECT current_state, version FROM gov.workflow_instances WHERE id = $1`, started.ID).
		Scan(&stateDB, &versionDB); err != nil {
		t.Fatalf("baca instance dari DB: %v", err)
	}
	if stateDB != "didisposisi" || versionDB != 2 {
		t.Fatalf("instance di DB: state=%q version=%d, ingin didisposisi/2", stateDB, versionDB)
	}
}
