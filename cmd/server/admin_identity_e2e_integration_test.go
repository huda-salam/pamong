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
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/huda-salam/pamong/core/config"
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

	codec := identitytoken.NewJWTCodec(
		[]byte("w2-e2e-secret-0123456789-abcdef"), time.Hour,
		identitydb.NewRevokedTokenStore(identityPool),
	)
	// Catalog dibangun SESUDAH role sentral di-seed: ia snapshot proses, jadi role yang lahir
	// setelahnya tak terlihat sampai restart (perilaku yang memang didokumentasikan).
	centralCatalog, err := identitydb.NewCentralRoleCatalog(ctx, identitydb.NewCentralRoleRepo(identityPool))
	if err != nil {
		t.Fatalf("central catalog: %v", err)
	}
	limiter := ratelimit.NewMemory(nil)
	authHandler, err := wireAuth(identityPool, connMgr, cryptoSvc, codec, limiter, logger,
		config.MessagingConfig{Driver: "log"}, verifyGate)
	if err != nil {
		t.Fatalf("wireAuth: %v", err)
	}

	handler := buildServerHandler(serverDeps{
		router:         router,
		verifier:       codec,
		evalFactory:    newEvaluatorFactory(centralCatalog, connMgr, nil, 0),
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

	role := &identitydomain.CentralRole{
		ID: uuid.New(), Name: adminRole, Label: "Admin Platform",
		ScopeType: identitydomain.ScopeGlobal,
		Permissions: []string{
			identitydomain.PermPersonBuat,
			identitydomain.PermEmploymentLampir,
			identitydomain.PermCredentialBuat,
			identitydomain.PermAssignmentTugaskan,
			identitydomain.PermCentralRoleAssign,
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
