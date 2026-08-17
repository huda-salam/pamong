//go:build integration

// DoD PR-W2: `POST /admin/identity/assignments` benar-benar melahirkan baris
// `gov.user_profiles` di DB tenant tujuan, dengan pengenal TERENKRIPSI.
//
// Test ini menempuh jalur penuh lewat perakitan produksi:
//
//	seed admin bootstrap (assigned_by = sentinel SYSTEM)
//	  → POST /auth/login                          (token nyata, wireAuth)
//	  → POST /admin/identity/persons              (wireAdminIdentity, repo ber-audit)
//	  → POST /admin/identity/employments
//	  → POST /admin/identity/credentials
//	  → POST /admin/identity/assignments          → identity.employment.ditugaskan
//	  → NATS NYATA → clone engine (wireIdentitySync) → gov.user_profiles tenant
//	  → UserResolver.ResolveByNIK menemukannya lewat blind index
//
// Bus-nya NATS embedded, BUKAN driver memory. Memory mengirim SINKRON di goroutine pemanggil,
// jadi ia tak bisa membedakan "subscriber ter-register" dari "ter-register TEPAT WAKTU" —
// persis kelas cacat yang pernah muncul di repo ini sebagai test flaky (Subscribe tanpa Flush).
//
// Inilah yang tak pernah terbukti sebelum PR ini: clone engine ter-wire sejak PR-5.1.4, tapi
// tak ada satu pun kode produksi yang menerbitkan event pemicunya (GAP (b)). Jalur clone karena
// itu tak pernah berjalan di luar test yang menerbitkan event-nya sendiri.
package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/huda-salam/pamong/core/config"
	"github.com/huda-salam/pamong/core/permission"
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
	infrauser "github.com/huda-salam/pamong/infra/user"
	"github.com/huda-salam/pamong/port"
	tenantroledb "github.com/huda-salam/pamong/tenantrole/adapter/db"
	tenantroledomain "github.com/huda-salam/pamong/tenantrole/domain"
)

// Pengenal fixture pegawai yang DIBUAT lewat HTTP. Nilainya dipakai dua arah — dikirim ke API
// dan dicari di dump clone — jadi satu konstanta, bukan dua literal yang bisa menyimpang.
const (
	w2Tenant   = "pemkot-w2"
	w2NIK      = "3578010101900055"
	w2NIP      = "199001012015011055"
	w2Email    = "wahyu@example.test"
	w2NoHP     = "0812340055"
	w2Nama     = "Wahyu Pratama"
	w2Password = "kata-sandi-pegawai"

	adminNIP      = "199001012015011000"
	adminPassword = "kata-sandi-admin-w2"
	adminRole     = "platform_admin"
)

func TestE2E_AdminIdentity_PenugasanMelahirkanCloneTerenkripsi(t *testing.T) {
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
	dropSchemas := func() { _, _ = pool.Exec(context.Background(), `DROP SCHEMA IF EXISTS id, gov CASCADE`) }
	dropSchemas()
	t.Cleanup(func() { dropSchemas(); rawPool.Close() })

	// Skema identity dari file migrasi NYATA — termasuk 010 yang men-seed sentinel SYSTEM.
	// gov.user_profiles TIDAK di-seed: yang diuji justru apakah writer meng-ensure-nya sendiri.
	applyUpMigrations(t, ctx, pool, repoPath("identity/migrations"))
	if _, err := pool.Exec(ctx, `
		INSERT INTO id.tenant_registry (tenant_id, nama, tier, db_host, db_name, is_active)
		VALUES ($1, $2, 1, '', $3, true)`, w2Tenant, "Pemkot W2", cp.Database); err != nil {
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

	adminID := seedAdminBootstrap(t, ctx, identityPool, cryptoSvc)

	// --- Bus NYATA + clone engine + grup admin, semua lewat fungsi wiring produksi ---
	bus, err := eventbus.NewFromConfig(
		config.EventBusConfig{Driver: "nats", URL: startEmbeddedNATS(t)},
		eventbus.NewSchemaRegistry(),
	)
	if err != nil {
		t.Fatalf("event bus NATS: %v", err)
	}
	if err := identitydomain.RegisterEventSchemas(bus.Schema()); err != nil {
		t.Fatalf("RegisterEventSchemas: %v", err)
	}
	if err := wireIdentitySync(identityPool, connMgr, cryptoSvc, bus); err != nil {
		t.Fatalf("wireIdentitySync: %v", err)
	}
	t.Cleanup(func() { _ = bus.Drain() })

	// Gerbang bcrypt yang SAMA untuk kedua permukaan, persis seperti run().
	verifyGate := identityusecase.NewVerifyGate(0, 0)

	adminHandler, err := wireAdminIdentity(ctx, identityPool, cryptoSvc, bus, verifyGate)
	if err != nil {
		t.Fatalf("wireAdminIdentity: %v", err)
	}

	logger := observability.NewLogger(observability.LogOptions{Level: "error", Format: "text"})
	router := gateway.NewRouter()
	router.Get("/healthz", healthz)
	mountAdminIdentityRoutes(router, adminHandler)

	codec := identitytoken.NewJWTCodec(identitytoken.Options{
		Secret:  []byte("w2-e2e-secret-0123456789-abcdef"),
		TTL:     time.Hour,
		Revoked: identitydb.NewRevokedTokenStore(identityPool),
	})
	// Catalog dibangun SESUDAH role sentral di-seed: ia snapshot proses, jadi role yang lahir
	// setelahnya tak terlihat sampai restart (perilaku yang memang didokumentasikan).
	centralCatalog, err := identitydb.NewCentralRoleCatalog(ctx, identitydb.NewCentralRoleRepo(identityPool))
	if err != nil {
		t.Fatalf("central catalog: %v", err)
	}
	limiter := ratelimit.NewMemory(nil)
	authHandler, err := wireAuth(identityPool, connMgr, cryptoSvc, codec, limiter, logger,
		testMessageSender(t), verifyGate)
	if err != nil {
		t.Fatalf("wireAuth: %v", err)
	}

	handler := buildServerHandler(serverDeps{
		router:   router,
		verifier: codec,
		// TTL sengaja sekecil mungkin, bukan 0. TTL 0 berarti "cache selama umur proses": katalog
		// tenant akan dibangun saat request admin PERTAMA dan tak pernah melihat role tenant yang
		// diseed sesudahnya. Assertion B8 di bawah lalu hijau karena role-nya TAK ADA di katalog —
		// bukan karena resolusi ber-lapis-asal menolaknya. Dengan TTL ini tiap request membangun
		// ulang katalog tenant, sehingga role penyamar BENAR-BENAR ada di lapis tenant saat diuji.
		evalFactory:    newEvaluatorFactory(centralCatalog, connMgr, nil, time.Nanosecond),
		tenantResolver: tenantResolver,
		rateLimiter:    limiter,
		rateLimit:      config.RateLimitConfig{Enabled: false},
		auth:           authHandler,
		logger:         logger,
	})
	// === Langkah 0: admin bootstrap login lewat jalur nyata ===
	token := loginAdmin(t, handler)

	// === Langkah 1-3: bangun pegawai lewat HTTP ===
	personID := postAdmin[struct {
		ID uuid.UUID `json:"id"`
	}](t, handler, token, "/admin/identity/persons", map[string]any{
		"nik": w2NIK, "nama_lengkap": w2Nama, "email": w2Email, "no_hp": w2NoHP,
	}).ID

	employmentID := postAdmin[struct {
		ID uuid.UUID `json:"id"`
	}](t, handler, token, "/admin/identity/employments", map[string]any{
		"person_id": personID, "status": "asn", "nip": w2NIP, "instansi_asal": "Pemkot W2",
	}).ID

	credentialID := postAdmin[struct {
		ID uuid.UUID `json:"id"`
	}](t, handler, token, "/admin/identity/credentials", map[string]any{
		"person_id": personID, "cred_type": "nip", "cred_value": w2NIP,
		"password": w2Password, "is_primary": true,
	}).ID
	if credentialID == uuid.Nil {
		t.Fatal("credential tak menghasilkan id")
	}

	// === Langkah 4: DoD — penugasan ke tenant ===
	assignment := postAdmin[struct {
		ID           uuid.UUID `json:"id"`
		TenantID     string    `json:"tenant_id"`
		IsHomeTenant bool      `json:"is_home_tenant"`
	}](t, handler, token, "/admin/identity/assignments", map[string]any{
		"employment_id": employmentID, "tenant_id": w2Tenant,
	})
	if assignment.TenantID != w2Tenant || !assignment.IsHomeTenant {
		t.Fatalf("respons penugasan salah: %+v", assignment)
	}

	// === Bukti 1: baris clone muncul di DB TENANT, pengenalnya tak terbaca dari dump ===
	tenantPool, err := connMgr.Tenant(ctx, w2Tenant)
	if err != nil {
		t.Fatalf("pool tenant: %v", err)
	}
	waitForClone(t, ctx, tenantPool, personID)

	var dump, gotNama string
	var gotAssignment uuid.UUID
	if err := tenantPool.QueryRow(ctx,
		`SELECT user_profiles::text, nama_lengkap, assignment_id
		   FROM gov.user_profiles WHERE id = $1`, personID,
	).Scan(&dump, &gotNama, &gotAssignment); err != nil {
		t.Fatalf("baca clone: %v", err)
	}
	if gotNama != w2Nama {
		t.Fatalf("nama clone = %q, mau %q", gotNama, w2Nama)
	}
	if gotAssignment != assignment.ID {
		t.Fatalf("assignment_id clone = %s, mau %s (id yang dikembalikan endpoint)",
			gotAssignment, assignment.ID)
	}
	assertPengenalTersegel(t, dump, w2Nama)

	// === Bukti 2: pembaca produksi menemukannya lewat blind index ===
	// Ini yang mengunci kecocokan realm penulis↔pembaca: realm yang salah tidak melempar error,
	// ia hanya membuat bidx tak pernah cocok sehingga lookup mati tanpa gejala.
	userResolver, err := infrauser.NewDBResolver(connMgr, cryptoSvc)
	if err != nil {
		t.Fatalf("NewDBResolver: %v", err)
	}
	tenantCtx := port.WithTenant(ctx, w2Tenant)
	profile, err := userResolver.ResolveByNIK(tenantCtx, w2NIK)
	if err != nil {
		t.Fatalf("ResolveByNIK lewat blind index: %v", err)
	}
	if profile.ID != personID || profile.NIP != w2NIP || profile.NamaLengkap != w2Nama {
		t.Fatalf("profil hasil resolusi salah: id=%s nip=%q nama=%q",
			profile.ID, profile.NIP, profile.NamaLengkap)
	}

	// === Bukti 3: pengenal di IDENTITY DB juga tersimpan terenkripsi (bukan hanya di clone) ===
	assertIdentityDBTersegel(t, ctx, pool, personID)

	// === Bukti 4: mutasi ter-audit dengan aktor yang benar, dan diff-nya tak plaintext ===
	assertAuditMutasi(t, ctx, pool, adminID, personID)

	// === Bukti 5: kredensial yang dibuat lewat HTTP benar-benar bisa dipakai login ===
	// Ini menutup lingkarnya: CreateCredential memakai port yang sama dengan sisi verifikasi,
	// jadi hash yang ditulisnya bukan artefak yang hanya "terlihat benar".
	loginBody := `{"cred_type":"nip","cred_value":"` + w2NIP + `","password":"` + w2Password + `"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(loginBody))
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("pegawai baru tak bisa login dengan kredensial yang baru dibuat: %d — body: %s",
			rec.Code, rec.Body.String())
	}

	// === DoD PR-W3a (b) & (c): B8 + B7 lewat stack HTTP NYATA ===
	//
	// Pegawai w2 di atas kini dipakai sebagai AKTOR. Ia diberi dua hal sekaligus, karena
	// keduanya jalur berbeda menuju tujuan yang sama (mengambil alih wewenang platform):
	//
	//   B8 — role TENANT bernama persis seperti role SENTRAL `platform_admin`, tanpa permission
	//        apa pun. Sebelum ADR-019, nama itu me-resolve ke definisi sentral ber-LayerGlobal
	//        dan membuka SELURUH permission identity:* tanpa pernah menyebut namespace itu.
	//   B7 — role SENTRAL scoped yang memberi identity:credential:buat di tenantnya sendiri.
	//        Wewenang yang SAH, tapi tak boleh berlaku atas target di luar wewenangnya.
	//
	seedRolePenyamarDanScoped(t, ctx, identityPool, tenantPool, personID)
	if err := centralCatalog.Refresh(ctx); err != nil {
		t.Fatalf("refresh central catalog: %v", err)
	}

	pegawaiToken := loginPegawaiW2(t, handler)
	claims, err := codec.Verify(ctx, pegawaiToken)
	if err != nil {
		t.Fatalf("verify token pegawai: %v", err)
	}
	// Prasyarat test, bukan yang diuji: klaim HARUS benar-benar membawa nama penyamar itu di
	// tenant_roles. Kalau tidak, dua assert di bawah hijau tanpa menguji apa pun.
	if !slices.Contains(claims.TenantRoles, adminRole) {
		t.Fatalf("tenant_roles klaim = %v, harus memuat %q — seed role penyamar tak sampai ke token",
			claims.TenantRoles, adminRole)
	}
	if slices.Contains(claims.CentralRoles, adminRole) {
		t.Fatalf("central_roles klaim = %v, tak boleh memuat %q — pegawai tak pernah diberi "+
			"role sentral itu", claims.CentralRoles, adminRole)
	}

	// Prasyarat kedua: role penyamar HARUS terlihat di katalog lapis tenant saat request dijalankan.
	// Kalau tidak, 403 di bawah datang dari "role tak dikenal", bukan dari pengurungan lapis asal —
	// test hijau karena alasan yang salah. Katalog dibangun ulang persis seperti yang dilakukan
	// evaluatorFactory tiap request (TTL di atas).
	assertRolePenyamarTerlihat(t, ctx, tenantPool)

	// --- DoD (b): role tenant penyamar tak mewarisi permission role sentral ---
	// `identity:person:buat` hanya dimiliki role SENTRAL platform_admin. Role tenant senama
	// tidak memberi apa pun, dan role scoped pegawai hanya memberi identity:credential:buat.
	if code := postAdminStatus(t, handler, pegawaiToken, "/admin/identity/persons", map[string]any{
		"nik": "3578010101900077", "nama_lengkap": "Korban B8",
	}); code != http.StatusForbidden {
		t.Fatalf("B8: role TENANT bernama %q tak boleh mewarisi permission role sentral — "+
			"POST /admin/identity/persons mau 403, dapat %d", adminRole, code)
	}

	// --- DoD (c): kredensial untuk target di luar wewenang aktor ditolak (varian TERKUAT B7) ---
	// Kontrol positif dulu: permission scoped pegawai memang berlaku untuk target biasa. Tanpa
	// pasangan ini, 403 di bawah tak bisa dibedakan dari "permission tak pernah aktif".
	korban := postAdmin[struct {
		ID uuid.UUID `json:"id"`
	}](t, handler, token, "/admin/identity/persons", map[string]any{
		"nik": "3578010101900078", "nama_lengkap": "Pegawai Biasa",
	}).ID
	if code := postAdminStatus(t, handler, pegawaiToken, "/admin/identity/credentials", map[string]any{
		"person_id": korban, "cred_type": "nip", "cred_value": "199001012015011078",
		"password": "kata-sandi-yang-panjang",
	}); code != http.StatusCreated {
		t.Fatalf("kontrol positif: kredensial untuk target BIASA mau 201, dapat %d — "+
			"gerbang containment tak boleh mematikan administrasi identitas biasa", code)
	}
	// Target = ADMIN PLATFORM (pemegang role sentral global). Menerbitkan kredensial untuknya
	// setara dengan bisa login sebagai dia: login me-resolve murni lewat (cred_type, cred_value).
	if code := postAdminStatus(t, handler, pegawaiToken, "/admin/identity/credentials", map[string]any{
		"person_id": adminID, "cred_type": "nip", "cred_value": "199001012015011079",
		"password": "kata-sandi-yang-panjang",
	}); code != http.StatusForbidden {
		t.Fatalf("B7: kredensial untuk admin platform mau 403, dapat %d — aktor ber-scope tenant "+
			"tak boleh menerbitkan kredensial bagi target di luar wewenangnya", code)
	}
}

// assertRolePenyamarTerlihat memastikan role tenant bernama adminRole benar-benar ADA di katalog
// lapis tenant, dan bahwa katalog itu melaporkannya sebagai LayerTenant tanpa permission —
// prasyarat agar assertion B8 menguji pengurungan lapis, bukan ketiadaan role.
func assertRolePenyamarTerlihat(t *testing.T, ctx context.Context, tenantPool *db.Pool) {
	t.Helper()
	cat, err := tenantroledb.NewTenantRoleCatalog(ctx, tenantroledb.NewTenantRoleRepo(tenantPool))
	if err != nil {
		t.Fatalf("bangun katalog tenant: %v", err)
	}
	role, ok := cat.Lookup(adminRole)
	if !ok {
		t.Fatalf("role tenant %q tak terlihat di katalog lapis tenant — assertion B8 di bawah "+
			"akan hijau karena role tak dikenal, bukan karena lapis asalnya dikurung", adminRole)
	}
	if role.Layer != permission.LayerTenant || len(role.Permissions) != 0 {
		t.Fatalf("role penyamar di lapis tenant salah bentuk: layer=%v permissions=%v",
			role.Layer, role.Permissions)
	}
}

// seedRolePenyamarDanScoped menyiapkan dua wewenang untuk pegawai w2 (lihat DoD di pemanggil):
// role TENANT bernama persis seperti role sentral platform_admin (B8), dan role SENTRAL scoped
// yang memberi identity:credential:buat di w2Tenant saja (B7).
func seedRolePenyamarDanScoped(
	t *testing.T, ctx context.Context, identityPool, tenantPool *db.Pool, personID uuid.UUID,
) {
	t.Helper()

	// B8 — role tenant PENYAMAR. Daftar permission-nya sengaja KOSONG: yang diuji adalah
	// pewarisan lewat NAMA, bukan lewat grant. Namespace identity: memang tertutup bagi role
	// tenant (B6), dan justru itu intinya — jalur ini tak pernah menyebutnya.
	role := &tenantroledomain.TenantRole{
		ID: uuid.New(), Name: adminRole, Label: "Penyamar",
	}
	if err := tenantroledb.NewTenantRoleRepo(tenantPool).Save(ctx, role); err != nil {
		t.Fatalf("simpan role tenant penyamar: %v", err)
	}
	if err := tenantroledb.NewTenantRoleAssignmentRepo(tenantPool).Save(ctx,
		&tenantroledomain.TenantRoleAssignment{
			ID: uuid.New(), UserID: personID, RoleID: role.ID, AssignedBy: personID,
			ValidFrom: time.Now().Add(-time.Hour),
		}); err != nil {
		t.Fatalf("assign role tenant penyamar: %v", err)
	}

	// B7 — role sentral SCOPED, wewenang yang sah di tenantnya sendiri.
	scoped := &identitydomain.CentralRole{
		ID: uuid.New(), Name: "operator_identitas", Label: "Operator Identitas Daerah",
		ScopeType:   identitydomain.ScopeScoped,
		Permissions: []string{identitydomain.PermCredentialBuat},
	}
	if err := identitydb.NewCentralRoleRepo(identityPool).Save(ctx, scoped); err != nil {
		t.Fatalf("simpan role sentral scoped: %v", err)
	}
	if err := identitydb.NewCentralRoleAssignmentRepo(identityPool).Save(ctx,
		&identitydomain.CentralRoleAssignment{
			ID: uuid.New(), PersonID: personID, RoleID: scoped.ID,
			TenantScope: []string{w2Tenant}, AssignedBy: identitydomain.SystemActorID,
			ValidFrom: time.Now().Add(-time.Hour),
		}); err != nil {
		t.Fatalf("assign role sentral scoped: %v", err)
	}
}

// loginPegawaiW2 menempuh POST /auth/login sebagai pegawai w2 dan mengembalikan token final.
func loginPegawaiW2(t *testing.T, h http.Handler) string {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(
		`{"cred_type":"nip","cred_value":"`+w2NIP+`","password":"`+w2Password+`"}`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login pegawai w2: mau 200, dapat %d — body: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.Token == "" {
		t.Fatalf("token login pegawai w2 tak terbaca: %v — body: %s", err, rec.Body.String())
	}
	return out.Token
}

// postAdminStatus mengirim POST ber-token dan mengembalikan HANYA status code — pasangan
// postAdmin untuk kasus yang memang diharapkan GAGAL (postAdmin mem-fatal-kan non-2xx).
func postAdminStatus(
	t *testing.T, h http.Handler, token, path string, body map[string]any,
) int {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encode body: %v", err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(rec, req)
	return rec.Code
}

// Rute admin menolak permintaan anonim — diuji terhadap server rakitan NYATA, bukan hanya
// terhadap stub di admin_identity_routes_test.go. Yang dijaga: `assigned_by` tak pernah bisa
// terisi uuid.Nil karena request tak berpemilik tak pernah sampai ke use case.
func TestE2E_AdminIdentity_TanpaToken_401(t *testing.T) {
	if os.Getenv("PAMONG_TEST_DB_DSN") == "" {
		t.Skip("PAMONG_TEST_DB_DSN tidak diset — lewati e2e integration test")
	}
	router := gateway.NewRouter()
	mountAdminIdentityRoutes(router, &stubAdminRoutes{})
	h := buildServerHandler(serverDeps{
		router:         router,
		verifier:       &fakeVerifier{tokens: map[string]*port.Claims{}},
		evalFactory:    fakeFactory{},
		tenantResolver: fakeTenantResolver{},
		rateLimiter:    ratelimit.NewMemory(nil),
		rateLimit:      config.RateLimitConfig{Enabled: false},
		logger:         observability.NewLogger(observability.LogOptions{Level: "error", Format: "text"}),
	})
	w := postJSON(h, "/admin/identity/assignments", `{"employment_id":"`+uuid.New().String()+
		`","tenant_id":"`+w2Tenant+`"}`, "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("penugasan tanpa token harus 401, dapat %d", w.Code)
	}
}

// seedAdminBootstrap membuat admin platform PERTAMA — kasus yang justru membuat sentinel SYSTEM
// ada. Kedua assignment-nya ber-`assigned_by` = domain.SystemActorID, jadi test ini sekaligus
// membuktikan baris sentinel dari migrasi 010 memenuhi FK ke id.persons: tanpa migrasi itu,
// INSERT di bawah gagal dengan pelanggaran foreign key.
func seedAdminBootstrap(
	t *testing.T, ctx context.Context, identityPool *db.Pool, cryptoSvc port.CryptoPort,
) uuid.UUID {
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

	admin := &identitydomain.Person{
		ID: uuid.New(), NIK: "3578010101900000", NamaLengkap: "Admin Platform", IsActive: true,
	}
	if err := persons.Save(ctx, admin); err != nil {
		t.Fatalf("simpan admin: %v", err)
	}
	emp := &identitydomain.Employment{
		ID: uuid.New(), PersonID: admin.ID, Status: identitydomain.StatusASN, NIP: adminNIP,
		IsActive: true, ValidFrom: time.Now().Add(-24 * time.Hour),
	}
	if err := employments.Save(ctx, emp); err != nil {
		t.Fatalf("simpan employment admin: %v", err)
	}
	hash, err := identityauth.NewBcryptVerifier().Hash(adminPassword)
	if err != nil {
		t.Fatalf("hash password admin: %v", err)
	}
	if err := creds.Save(ctx, &identitydomain.Credential{
		ID: uuid.New(), PersonID: admin.ID, CredType: identitydomain.CredNIP,
		CredValue: adminNIP, SecretHash: hash, IsPrimary: true,
	}); err != nil {
		t.Fatalf("simpan credential admin: %v", err)
	}

	// assigned_by = SENTINEL: tak ada manusia yang bisa menugaskan admin pertama.
	if err := identitydb.NewTenantAssignmentRepo(identityPool).Save(ctx, &identitydomain.TenantAssignment{
		ID: uuid.New(), EmploymentID: emp.ID, TenantID: w2Tenant, IsHomeTenant: true,
		AssignedBy: identitydomain.SystemActorID, ValidFrom: time.Now().Add(-24 * time.Hour),
	}); err != nil {
		t.Fatalf("penugasan admin ber-sentinel gagal — migrasi 010 (seed SYSTEM) tak diterapkan? %v", err)
	}

	// PermAuthorityEscalate WAJIB ada di role bootstrap (ADR-019 Keputusan 5): memberikannya
	// lewat AssignCentralRole menuntut aktor sudah memegangnya, jadi pemegang PERTAMA hanya bisa
	// lahir dari seed seperti ini. Instalasi yang melewatkannya punya admin yang tak bisa
	// menugaskan role global maupun menugaskan lintas tenant, tanpa jalan keluar.
	role := &identitydomain.CentralRole{
		ID: uuid.New(), Name: adminRole, Label: "Admin Platform",
		ScopeType: identitydomain.ScopeGlobal,
		Permissions: []string{
			identitydomain.PermPersonBuat,
			identitydomain.PermEmploymentLampir,
			identitydomain.PermCredentialBuat,
			identitydomain.PermAssignmentTugaskan,
			identitydomain.PermCentralRoleAssign,
			identitydomain.PermAuthorityEscalate,
		},
	}
	if err := identitydb.NewCentralRoleRepo(identityPool).Save(ctx, role); err != nil {
		t.Fatalf("simpan role sentral: %v", err)
	}
	if err := identitydb.NewCentralRoleAssignmentRepo(identityPool).Save(ctx,
		&identitydomain.CentralRoleAssignment{
			ID: uuid.New(), PersonID: admin.ID, RoleID: role.ID,
			AssignedBy: identitydomain.SystemActorID, ValidFrom: time.Now().Add(-24 * time.Hour),
		}); err != nil {
		t.Fatalf("assignment role sentral ber-sentinel gagal: %v", err)
	}
	return admin.ID
}

// loginAdmin menempuh POST /auth/login dan mengembalikan token final.
func loginAdmin(t *testing.T, h http.Handler) string {
	t.Helper()
	body := `{"cred_type":"nip","cred_value":"` + adminNIP + `","password":"` + adminPassword + `"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("login admin: mau 200, dapat %d — body: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Token               string `json:"token"`
		NeedTenantSelection bool   `json:"need_tenant_selection"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.Token == "" {
		t.Fatalf("respons login tanpa token: %s (%v)", rec.Body.String(), err)
	}
	if out.NeedTenantSelection {
		t.Fatalf("admin dengan satu penugasan harus langsung dapat token final: %s", rec.Body.String())
	}
	return out.Token
}

// postAdmin mengirim satu request admin ber-token dan men-decode respons 201-nya ke T.
func postAdmin[T any](t *testing.T, h http.Handler, token, path string, body map[string]any) T {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("POST %s: mau 201, dapat %d — body: %s", path, rec.Code, rec.Body.String())
	}
	var out T
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode respons %s: %v — body: %s", path, err, rec.Body.String())
	}
	return out
}

// assertPengenalTersegel memeriksa dump satu baris: tak satu pun pengenal boleh terbaca, baik
// sebagai teks maupun sebagai bytea mentah (yang Postgres render sebagai hex — tanpa pemeriksaan
// ini, nilai yang mendarat MENTAH di kolom bytea akan lolos; pelajaran PR-3.8.5a).
func assertPengenalTersegel(t *testing.T, dump, namaTerlihat string) {
	t.Helper()
	lower := strings.ToLower(dump)
	for _, rahasia := range []string{w2NIK, w2NIP, w2Email, w2NoHP} {
		if strings.Contains(lower, strings.ToLower(rahasia)) {
			t.Fatalf("pengenal %q muncul plaintext di baris: %s", rahasia, dump)
		}
		if strings.Contains(lower, hex.EncodeToString([]byte(rahasia))) {
			t.Fatalf("pengenal %q tersimpan mentah di kolom bytea (terbaca sebagai hex)", rahasia)
		}
	}
	// Kontrol negatif: nama SENGAJA plaintext (kelas `personal`). Tanpa ini, pemeriksaan di atas
	// tetap hijau seandainya query membaca baris kosong dan tak membuktikan apa pun.
	if namaTerlihat != "" && !strings.Contains(lower, strings.ToLower(namaTerlihat)) {
		t.Fatal("nama_lengkap tak ada di dump — test membaca baris yang salah")
	}
}

// assertIdentityDBTersegel memeriksa baris id.persons/id.employments/id.credentials yang lahir
// dari endpoint admin. Clone yang bersih tak menjamin apa pun tentang sumbernya: keduanya
// memakai realm kunci yang BERBEDA (tenant vs sentral, ADR-017) dan penulis yang berbeda.
func assertIdentityDBTersegel(t *testing.T, ctx context.Context, pool *db.Pool, personID uuid.UUID) {
	t.Helper()
	var personRow, empRow, credRow string
	if err := pool.QueryRow(ctx, `SELECT persons::text FROM id.persons WHERE id = $1`, personID).
		Scan(&personRow); err != nil {
		t.Fatalf("baca id.persons: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT employments::text FROM id.employments WHERE person_id = $1`, personID).
		Scan(&empRow); err != nil {
		t.Fatalf("baca id.employments: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT credentials::text FROM id.credentials WHERE person_id = $1`, personID).
		Scan(&credRow); err != nil {
		t.Fatalf("baca id.credentials: %v", err)
	}

	assertPengenalTersegel(t, personRow, w2Nama)
	assertPengenalTersegel(t, empRow, "")
	assertPengenalTersegel(t, credRow, "")
	// Password plaintext tak boleh pernah menyentuh DB; yang tersimpan hanya hash bcrypt.
	if strings.Contains(credRow, w2Password) {
		t.Fatalf("password plaintext tersimpan di id.credentials: %s", credRow)
	}
	if !strings.Contains(credRow, "$2a$") {
		t.Fatalf("secret_hash bukan hash bcrypt: %s", credRow)
	}
}

// assertAuditMutasi menegakkan ADR-003 pada jalur baru ini: setiap mutasi identity lewat grup
// admin meninggalkan jejak ber-aktor, dan diff-nya tak memuat pengenal plaintext (REVIEW_BACKLOG
// E2 — mengenkripsi kolom sambil membiarkan diff terbuka hanya MEMINDAHKAN kebocoran).
func assertAuditMutasi(t *testing.T, ctx context.Context, pool *db.Pool, adminID, personID uuid.UUID) {
	t.Helper()
	var actorID uuid.UUID
	var diff string
	if err := pool.QueryRow(ctx,
		`SELECT actor_id, diff::text FROM id.audit_logs
		  WHERE entity = 'identity.Person' AND entity_id = $1`, personID,
	).Scan(&actorID, &diff); err != nil {
		t.Fatalf("entry audit pembuatan person tak ada — mutasi identity wajib ter-audit (ADR-003): %v", err)
	}
	if actorID != adminID {
		t.Fatalf("actor audit = %s, mau %s (admin yang memegang token)", actorID, adminID)
	}
	if strings.Contains(diff, w2NIK) || strings.Contains(diff, w2Email) || strings.Contains(diff, w2NoHP) {
		t.Fatalf("diff audit memuat pengenal plaintext: %s", diff)
	}
	if !strings.Contains(diff, w2Nama) {
		t.Fatalf("diff audit tak memuat nama — kontrol negatif gagal, entry yang dibaca salah: %s", diff)
	}

	// Kredensial ikut ter-audit, dan hash bcrypt-nya TIDAK ikut: audit menjawab "siapa membuat
	// kredensial ini", bukan menyimpan bahan yang bisa di-crack offline.
	var credDiff string
	if err := pool.QueryRow(ctx,
		`SELECT diff::text FROM id.audit_logs WHERE entity = 'identity.Credential'`).Scan(&credDiff); err != nil {
		t.Fatalf("entry audit pembuatan credential tak ada: %v", err)
	}
	if strings.Contains(credDiff, "$2a$") || strings.Contains(credDiff, "secret_hash") ||
		strings.Contains(credDiff, w2Password) {
		t.Fatalf("diff audit credential memuat secret: %s", credDiff)
	}
}
