//go:build integration

// DoD PR-W1: alur login benar-benar bisa dipakai lewat server rakitan produksi.
//
// Sebelum PR ini, `LoginEmployee` & kawan-kawannya lengkap dan lulus unit test, tapi tak punya
// satu pun pemanggil di `cmd/server` sementara `RequireAuth` memagari seluruh rute bisnis — server
// yang di-boot `run()` tak bisa dilayani klien mana pun karena token hanya bisa dicetak di luar
// sistem. Unit test tak akan pernah menangkap itu: yang salah bukan komponennya, melainkan
// ketiadaan rakitan.
//
// Karena itu test ini sengaja BUKAN test handler. Ia menempuh jalur penuh:
//
//	POST /auth/login (tanpa token)  →  token JWT bertanda tangan
//	   → POST /surat-masuk dengan token itu  →  201
//
// Dua ujung yang dibuktikan sekaligus: token yang DITERBITKAN wireAuth lolos verifikasi stack
// middleware yang sama (satu secret, satu codec), dan role tenant yang di-resolve saat login
// benar-benar menghasilkan izin di RBAC live.
package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/huda-salam/pamong/core/config"
	"github.com/huda-salam/pamong/core/domain"
	coreWf "github.com/huda-salam/pamong/core/workflow"
	"github.com/huda-salam/pamong/gateway"
	identityauth "github.com/huda-salam/pamong/identity/adapter/auth"
	identitydb "github.com/huda-salam/pamong/identity/adapter/db"
	identitytoken "github.com/huda-salam/pamong/identity/adapter/token"
	identitydomain "github.com/huda-salam/pamong/identity/domain"
	identityusecase "github.com/huda-salam/pamong/identity/usecase"
	"github.com/huda-salam/pamong/infra/crypto"
	"github.com/huda-salam/pamong/infra/db"
	"github.com/huda-salam/pamong/infra/eventbus"
	"github.com/huda-salam/pamong/infra/observability"
	"github.com/huda-salam/pamong/infra/ratelimit"
	"github.com/huda-salam/pamong/infra/sequence"
	"github.com/huda-salam/pamong/infra/storage"
	infrauser "github.com/huda-salam/pamong/infra/user"
	"github.com/huda-salam/pamong/modules"
	"github.com/huda-salam/pamong/port"
	tenantroledb "github.com/huda-salam/pamong/tenantrole/adapter/db"
	tenantroledomain "github.com/huda-salam/pamong/tenantrole/domain"
)

func TestE2E_Login_LaluAksesRuteBisnis(t *testing.T) {
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

	const tenantID = "pemkot-a"
	if _, err := pool.Exec(ctx, `
		INSERT INTO id.tenant_registry (tenant_id, nama, tier, db_host, db_name, is_active)
		VALUES ($1, $2, 1, '', $3, true)`, tenantID, "Pemkot A", cp.Database); err != nil {
		t.Fatalf("seed tenant_registry: %v", err)
	}

	// --- Dependency, dirakit seperti run() ---
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
	tenantResolver := identitydb.NewTenantResolver(identitydb.NewTenantRepo(identityPool))
	connMgr := db.NewTenantConnManager(tenantResolver, sharedCfg, config.CentralDBConfig{})
	t.Cleanup(connMgr.Close)

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

	// --- Seed identitas lewat repo NYATA (pengenal tersimpan _enc/_bidx, ADR-017) ---
	const (
		nip      = "199001012015011001"
		password = "rahasia-yang-panjang"
	)
	personID, roleID := seedPegawaiBerpassword(t, ctx, identityPool, connMgr, cryptoSvc, tenantID, nip, password)
	_ = roleID

	// --- Rakit server ---
	bus, err := eventbus.NewFromConfig(
		config.EventBusConfig{Driver: "nats", URL: startEmbeddedNATS(t)},
		eventbus.NewSchemaRegistry(),
	)
	if err != nil {
		t.Fatalf("event bus: %v", err)
	}
	if err := identitydomain.RegisterEventSchemas(bus.Schema()); err != nil {
		t.Fatalf("schema event identity: %v", err)
	}
	store, err := storage.NewFromConfig(config.StorageConfig{Driver: "local", Endpoint: t.TempDir(), Bucket: "test"})
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	logger := observability.NewLogger(observability.LogOptions{Level: "error", Format: "text"})
	userResolver, err := infrauser.NewDBResolver(connMgr, cryptoSvc)
	if err != nil {
		t.Fatalf("NewDBResolver: %v", err)
	}

	router := gateway.NewRouter()
	router.Get("/healthz", healthz)
	app := domain.NewApp(
		db.NewTenantRoutingConn(connMgr), db.NewCentralRoutingConn(connMgr), bus, bus,
		sequence.NewDBGenerator(connMgr), observability.NewPrometheusMetrics(), store, userResolver,
		coreWf.NewActionRegistry(), router,
	)
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

	// Codec yang SAMA dipakai sebagai issuer (di dalam wireAuth) dan verifier (stack middleware),
	// persis seperti run(). Kalau keduanya dirakit dari secret berbeda, login berhasil tapi token
	// yang dihasilkannya ditolak 401 di request berikutnya — jenis kesalahan yang cuma terlihat
	// dengan menempuh kedua langkah dalam satu test.
	codec := identitytoken.NewJWTCodec(identitytoken.Options{
		Secret:  []byte("e2e-test-secret-0123456789-abcdef"),
		TTL:     time.Hour,
		Revoked: identitydb.NewRevokedTokenStore(identityPool),
	})
	centralCatalog, err := identitydb.NewCentralRoleCatalog(ctx, identitydb.NewCentralRoleRepo(identityPool))
	if err != nil {
		t.Fatalf("central catalog: %v", err)
	}
	limiter := ratelimit.NewMemory(nil)

	authHandler, err := wireAuth(identityPool, connMgr, cryptoSvc, codec, limiter, logger,
		testMessageSender(t), identityusecase.NewVerifyGate(0, 0))
	if err != nil {
		t.Fatalf("wireAuth: %v", err)
	}

	handler := buildServerHandler(serverDeps{
		router:         router,
		verifier:       codec,
		evalFactory:    newEvaluatorFactory(centralCatalog, connMgr, registry.StrictPermissions(), 0),
		tenantResolver: tenantResolver,
		rateLimiter:    limiter,
		rateLimit:      config.RateLimitConfig{Enabled: false},
		auth:           authHandler,
		logger:         logger,
	})

	// === Langkah 1: login TANPA token ===
	loginBody := `{"cred_type":"nip","cred_value":"` + nip + `","password":"` + password + `"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(loginBody))
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("POST /auth/login: mau 200, dapat %d — body: %s", rec.Code, rec.Body.String())
	}
	var login struct {
		Token               string `json:"token"`
		NeedTenantSelection bool   `json:"need_tenant_selection"`
		Tenants             []struct {
			TenantID string `json:"tenant_id"`
		} `json:"tenants"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &login); err != nil {
		t.Fatalf("decode respons login: %v — body: %s", err, rec.Body.String())
	}
	if login.Token == "" {
		t.Fatalf("respons login tanpa token: %s", rec.Body.String())
	}
	if login.NeedTenantSelection {
		t.Fatalf("pegawai dengan SATU penugasan harus langsung dapat token final, "+
			"bukan pemilihan tenant: %s", rec.Body.String())
	}
	if len(login.Tenants) != 1 || login.Tenants[0].TenantID != tenantID {
		t.Fatalf("daftar tenant pada respons = %+v, mau tepat [%s]", login.Tenants, tenantID)
	}

	// Klaim token harus membawa tenant & role hasil resolusi — inilah yang membuat langkah 2 lolos
	// RBAC. Diperiksa terpisah agar kegagalan langkah 2 bisa dibedakan: "token salah isi" vs
	// "izin tak ditegakkan".
	claims, err := codec.Verify(ctx, login.Token)
	if err != nil {
		t.Fatalf("token hasil login tak lolos verifikasi stack yang sama: %v", err)
	}
	if claims.TenantID != tenantID {
		t.Errorf("tenant_id di klaim = %q, mau %q", claims.TenantID, tenantID)
	}
	if claims.PersonID != personID {
		t.Errorf("person_id di klaim = %v, mau %v", claims.PersonID, personID)
	}
	if len(claims.TenantRoles) != 1 || claims.TenantRoles[0] != "operator_surat" {
		t.Errorf("tenant_roles di klaim = %v, mau [operator_surat] — TenantRoleResolver "+
			"tak memilih DB tenant yang benar", claims.TenantRoles)
	}

	// === Langkah 2: pakai token itu untuk rute bisnis ===
	body := `{
		"NomorSurat":"001/IN/2025",
		"TanggalSurat":"2025-01-02T00:00:00Z",
		"TanggalAgenda":"2025-01-02T00:00:00Z",
		"Pengirim":"Dinas X",
		"Perihal":"Undangan rapat",
		"Sifat":"biasa"
	}`
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/surat-masuk", strings.NewReader(body))
	req2.Header.Set("Authorization", "Bearer "+login.Token)
	req2.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusCreated {
		t.Fatalf("POST /surat-masuk dengan token hasil login: mau 201, dapat %d — body: %s",
			rec2.Code, rec2.Body.String())
	}

	// === Langkah 3: password salah ditolak, dan tak membocorkan tahap mana yang gagal ===
	recSalah := httptest.NewRecorder()
	reqSalah := httptest.NewRequest(http.MethodPost, "/auth/login",
		strings.NewReader(`{"cred_type":"nip","cred_value":"`+nip+`","password":"salah"}`))
	reqSalah.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(recSalah, reqSalah)
	if recSalah.Code != http.StatusUnauthorized {
		t.Fatalf("login password salah: mau 401, dapat %d — body: %s", recSalah.Code, recSalah.Body.String())
	}
	if strings.Contains(recSalah.Body.String(), nip) {
		t.Fatalf("respons login gagal mengutip NIP yang dicoba — jalur samping ADR-009 §6: %s",
			recSalah.Body.String())
	}

	// === Langkah 4 (DoD PR-W3c): akun yang SAMA menumpuk role sampai tokennya melewati pagar ===
	//
	// Ini bentuk kegagalan yang pagar ADR-020 ada untuk mencegah, dan ia hanya bisa dibuktikan
	// dari ujung ke ujung: role dibaca resolver tenant NYATA saat login, jadi tak ada satu pun
	// tempat di jalur ini yang tahu berapa besar token akan jadi sampai ia ditandatangani.
	// Tanpa pagar, langkah ini akan LULUS dengan 200 + token 7 KB — dan setiap request berikutnya
	// ditolak proxy 400 tanpa jejak di log aplikasi.
	seedBanyakRoleTenant(t, ctx, connMgr, tenantID, personID, 50, 100)

	recBesar := httptest.NewRecorder()
	reqBesar := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(loginBody))
	reqBesar.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(recBesar, reqBesar)

	if recBesar.Code != http.StatusConflict {
		t.Fatalf("login akun ber-50-role-panjang: mau 409, dapat %d — body: %s",
			recBesar.Code, recBesar.Body.String())
	}
	var gagal struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Token   string `json:"token"`
	}
	if err := json.Unmarshal(recBesar.Body.Bytes(), &gagal); err != nil {
		t.Fatalf("decode respons login oversize: %v — body: %s", err, recBesar.Body.String())
	}
	if gagal.Code != "TOKEN_TOO_LARGE" {
		t.Fatalf("code = %q, mau TOKEN_TOO_LARGE (kegagalan yang bisa dialertkan ops): %s",
			gagal.Code, recBesar.Body.String())
	}
	// Inti DoD: token itu TIDAK PERNAH sampai ke klien. Bila ia lolos, klien akan menyimpannya
	// dan gagal pada request berikutnya di tempat yang tak bisa didiagnosis.
	if gagal.Token != "" {
		t.Fatalf("token oversize bocor ke klien (%d byte)", len(gagal.Token))
	}
	if !strings.Contains(recBesar.Body.String(), "role tenant") {
		t.Fatalf("pesan tak menuntun ke sebabnya (jumlah role): %s", recBesar.Body.String())
	}
}

// seedBanyakRoleTenant menugaskan n role tenant bernama panjang (nameLen karakter) ke user —
// bentuk yang PERSIS diizinkan tenantRoleNameRe hari ini, sebab tak ada batas panjang di sana.
// Inilah cara akun nyata membengkak: admin tenant yang mengakumulasi role lintas tahun.
func seedBanyakRoleTenant(
	t *testing.T, ctx context.Context, connMgr *db.TenantConnManager,
	tenantID string, userID uuid.UUID, n, nameLen int,
) {
	t.Helper()
	tenantPool, err := connMgr.Tenant(ctx, tenantID)
	if err != nil {
		t.Fatalf("tenant pool: %v", err)
	}
	roles := tenantroledb.NewTenantRoleRepo(tenantPool)
	assigns := tenantroledb.NewTenantRoleAssignmentRepo(tenantPool)
	for i := 0; i < n; i++ {
		suffix := strconv.Itoa(i)
		name := strings.Repeat("r", nameLen-len(suffix)) + suffix
		role := &tenantroledomain.TenantRole{
			ID: uuid.New(), Name: name, Label: "Role " + suffix,
			Permissions: []string{"surat_masuk:surat:baca"},
		}
		if err := roles.Save(ctx, role); err != nil {
			t.Fatalf("simpan role %d: %v", i, err)
		}
		if err := assigns.Save(ctx, &tenantroledomain.TenantRoleAssignment{
			ID: uuid.New(), UserID: userID, RoleID: role.ID, AssignedBy: userID,
			ValidFrom: time.Now().Add(-24 * time.Hour),
		}); err != nil {
			t.Fatalf("simpan assignment role %d: %v", i, err)
		}
	}
}

// seedPegawaiBerpassword membuat person + employment ASN + credential NIP berpassword di identity
// DB, penugasan ke tenant, serta role tenant `operator_surat` (ber-permission surat_masuk:surat:buat)
// beserta assignment-nya di tenant DB. Semua lewat repo NYATA, jadi pengenal tersimpan terenkripsi
// dan login harus menemukannya lewat blind index — bukan lewat kolom plaintext yang sudah tak ada.
func seedPegawaiBerpassword(
	t *testing.T, ctx context.Context, identityPool *db.Pool, connMgr *db.TenantConnManager,
	cryptoSvc port.CryptoPort, tenantID, nip, password string,
) (personID, roleID uuid.UUID) {
	t.Helper()

	persons, err := identitydb.NewPersonRepo(identityPool, cryptoSvc)
	if err != nil {
		t.Fatalf("PersonRepo: %v", err)
	}
	employments, err := identitydb.NewEmploymentRepo(identityPool, cryptoSvc)
	if err != nil {
		t.Fatalf("EmploymentRepo: %v", err)
	}
	creds, err := identitydb.NewCredentialRepo(identityPool, cryptoSvc)
	if err != nil {
		t.Fatalf("CredentialRepo: %v", err)
	}

	person := &identitydomain.Person{
		ID: uuid.New(), NIK: "3578010101900001", NamaLengkap: "Budi Santoso", IsActive: true,
	}
	if err := persons.Save(ctx, person); err != nil {
		t.Fatalf("simpan person: %v", err)
	}
	emp := &identitydomain.Employment{
		ID: uuid.New(), PersonID: person.ID, Status: identitydomain.StatusASN, NIP: nip,
		IsActive: true, ValidFrom: time.Now().Add(-24 * time.Hour),
	}
	if err := employments.Save(ctx, emp); err != nil {
		t.Fatalf("simpan employment: %v", err)
	}

	hash, err := identityauth.NewBcryptVerifier().Hash(password)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if err := creds.Save(ctx, &identitydomain.Credential{
		ID: uuid.New(), PersonID: person.ID, CredType: identitydomain.CredNIP,
		CredValue: nip, SecretHash: hash, IsPrimary: true,
	}); err != nil {
		t.Fatalf("simpan credential: %v", err)
	}

	// assigned_by menunjuk person yang sama (FK id.persons) — sentinel SYSTEM actor baru mendarat
	// di PR-W2, lihat backlog ROADMAP.
	if err := identitydb.NewTenantAssignmentRepo(identityPool).Save(ctx, &identitydomain.TenantAssignment{
		ID: uuid.New(), EmploymentID: emp.ID, TenantID: tenantID, IsHomeTenant: true,
		AssignedBy: person.ID, ValidFrom: time.Now().Add(-24 * time.Hour),
	}); err != nil {
		t.Fatalf("simpan tenant assignment: %v", err)
	}

	tenantPool, err := connMgr.Tenant(ctx, tenantID)
	if err != nil {
		t.Fatalf("tenant pool: %v", err)
	}
	role := &tenantroledomain.TenantRole{
		ID: uuid.New(), Name: "operator_surat", Label: "Operator Surat",
		Permissions: []string{"surat_masuk:surat:buat"},
	}
	if err := tenantroledb.NewTenantRoleRepo(tenantPool).Save(ctx, role); err != nil {
		t.Fatalf("simpan tenant role: %v", err)
	}
	if err := tenantroledb.NewTenantRoleAssignmentRepo(tenantPool).Save(ctx, &tenantroledomain.TenantRoleAssignment{
		ID: uuid.New(), UserID: person.ID, RoleID: role.ID, AssignedBy: person.ID,
		ValidFrom: time.Now().Add(-24 * time.Hour),
	}); err != nil {
		t.Fatalf("simpan assignment role tenant: %v", err)
	}

	return person.ID, role.ID
}
