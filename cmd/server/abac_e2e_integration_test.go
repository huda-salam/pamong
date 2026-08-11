//go:build integration

// abac_e2e_integration_test.go adalah DoD PR-W3b: dua request dengan TOKEN YANG SAMA, berbeda
// HANYA pada `unit_kerja_id` sasaran — satu lolos, satu 403 — lewat stack HTTP produksi.
//
// Kenapa harus e2e dan tak cukup unit test: lapis ABAC (ScopedEngine, hierarki OPD, resolver
// grant, delegasi) sudah lengkap & lulus unit test sejak PR-2.3.5, dan justru itu masalahnya —
// ia tak punya satu pun pemanggil produksi, sehingga `RequirePermissionInUnit` bersifat PERMISIF
// di server hidup tanpa satu test pun berubah warna. Yang dibuktikan di sini adalah RANTAINYA:
// klaim token → Authority dari tenant DB → ScopedEngine → gateway.Context → use case → 403.
//
// Setiap mata rantai punya cara gagal yang tak terlihat dari komponennya: evaluator tak dipasang
// di middleware, Authority dibangun dari user yang salah, resolver menunjuk DB tenant yang salah,
// atau use case memakai RequirePermission (tanpa unit) sehingga scope tak pernah dievaluasi.
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
	"github.com/huda-salam/pamong/gateway"
	identitydb "github.com/huda-salam/pamong/identity/adapter/db"
	identitytoken "github.com/huda-salam/pamong/identity/adapter/token"
	"github.com/huda-salam/pamong/infra/crypto"
	"github.com/huda-salam/pamong/infra/db"
	"github.com/huda-salam/pamong/infra/observability"
	"github.com/huda-salam/pamong/infra/ratelimit"
	"github.com/huda-salam/pamong/port"
	tenantroledb "github.com/huda-salam/pamong/tenantrole/adapter/db"
	tenantroledomain "github.com/huda-salam/pamong/tenantrole/domain"
)

func TestE2E_ABAC_PenugasanRole_ScopeUnitDitegakkan(t *testing.T) {
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

	const tenantID = "pemkot-a"
	if _, err := pool.Exec(ctx, `
		INSERT INTO id.tenant_registry (tenant_id, nama, tier, db_host, db_name, is_active)
		VALUES ($1, $2, 1, '', $3, true)`, tenantID, "Pemkot A", cp.Database); err != nil {
		t.Fatalf("seed tenant_registry: %v", err)
	}

	identityPool, err := db.NewIdentity(ctx, config.IdentityDBConfig{
		Host: cp.Host, Port: int(cp.Port), Name: cp.Database, User: cp.User, Password: cp.Password,
		PoolMax: 5, PoolIdle: 1,
	})
	if err != nil {
		t.Fatalf("identity pool: %v", err)
	}
	t.Cleanup(identityPool.Close)

	tenantResolver := identitydb.NewTenantResolver(identitydb.NewTenantRepo(identityPool))
	connMgr := db.NewTenantConnManager(tenantResolver, config.DBConfig{
		Host: cp.Host, Port: int(cp.Port), Name: cp.Database, User: cp.User, Password: cp.Password,
		PoolMax: 5, PoolIdle: 1,
	}, config.CentralDBConfig{})
	t.Cleanup(connMgr.Close)

	// --- Seed tenant DB: dua OPD sejajar + admin yang wewenangnya DIBATASI ke salah satunya ---
	tenantPool, err := connMgr.Tenant(ctx, tenantID)
	if err != nil {
		t.Fatalf("tenant pool: %v", err)
	}
	unitA, unitB := uuid.New(), uuid.New()
	if _, err := tenantPool.Exec(ctx, `
		CREATE SCHEMA IF NOT EXISTS gov;
		CREATE TABLE IF NOT EXISTS gov.org_units (
		    id UUID PRIMARY KEY, parent_id UUID REFERENCES gov.org_units(id), name VARCHAR(255) NOT NULL)`); err != nil {
		t.Fatalf("ensure org_units: %v", err)
	}
	for id, nama := range map[uuid.UUID]string{unitA: "Dinas A", unitB: "Dinas B"} {
		if _, err := tenantPool.Exec(ctx,
			`INSERT INTO gov.org_units (id, parent_id, name) VALUES ($1, NULL, $2)`, id, nama); err != nil {
			t.Fatalf("seed org_unit %s: %v", nama, err)
		}
	}

	// Role admin lokal: boleh MENUGASKAN role tenant. Perhatikan yang TIDAK ada di sini — batas
	// unitnya. Batas itu datang dari ASSIGNMENT-nya di bawah, bukan dari definisi role: itulah
	// pemisahan RBAC (apa) vs ABAC (di mana) yang sedang diuji.
	roles := tenantroledb.NewTenantRoleRepo(tenantPool)
	adminRole := &tenantroledomain.TenantRole{
		ID: uuid.New(), Name: "admin_iam_lokal", Label: "Admin IAM Dinas A",
		Permissions: []string{tenantroledomain.PermTenantRoleAssign, tenantroledomain.PermTenantRoleBuat},
	}
	if err := roles.Save(ctx, adminRole); err != nil {
		t.Fatalf("simpan role admin: %v", err)
	}
	// Role apa pun yang nanti DITUGASKAN oleh admin itu (sasaran, bukan wewenangnya).
	targetRole := &tenantroledomain.TenantRole{
		ID: uuid.New(), Name: "operator_lokal", Label: "Operator", Permissions: []string{"surat_masuk:surat:baca"},
	}
	if err := roles.Save(ctx, targetRole); err != nil {
		t.Fatalf("simpan role sasaran: %v", err)
	}

	adminID := uuid.New()
	if err := tenantroledb.NewTenantRoleAssignmentRepo(tenantPool).Save(ctx, &tenantroledomain.TenantRoleAssignment{
		ID: uuid.New(), UserID: adminID, RoleID: adminRole.ID,
		UnitKerjaID: &unitA, // ← jangkauan wewenangnya: HANYA Dinas A
		AssignedBy:  adminID, ValidFrom: time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatalf("simpan assignment admin: %v", err)
	}

	// --- Rakit server seperti run() ---
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
	_ = cryptoSvc // grup /admin/iam tak menyentuh pengenal; dirakit hanya agar setup setara run()

	logger := observability.NewLogger(observability.LogOptions{Level: "error", Format: "text"})
	tenantDB := db.NewTenantRoutingConn(connMgr)

	adminIAM, err := wireAdminIAM(tenantDB, db.NewAuditRepo(tenantDB))
	if err != nil {
		t.Fatalf("wireAdminIAM: %v", err)
	}
	router := gateway.NewRouter()
	router.Get("/healthz", healthz)
	mountAdminIAMRoutes(router, adminIAM)

	secret := []byte("w3b-e2e-secret-0123456789-abcdef")
	revoked := identitydb.NewRevokedTokenStore(identityPool)
	codec := identitytoken.NewJWTCodec(identitytoken.Options{Secret: secret, TTL: time.Hour, Revoked: revoked})
	centralCatalog, err := identitydb.NewCentralRoleCatalog(ctx, identitydb.NewCentralRoleRepo(identityPool))
	if err != nil {
		t.Fatalf("central catalog: %v", err)
	}
	// Factory PRODUKSI — termasuk scopedDeps builder yang merakit Authority dari tenant DB.
	handler := buildServerHandler(serverDeps{
		router:         router,
		verifier:       codec,
		evalFactory:    newEvaluatorFactory(centralCatalog, connMgr, nil, 0),
		tenantResolver: tenantResolver,
		rateLimiter:    ratelimit.NewMemory(nil),
		rateLimit:      config.RateLimitConfig{Enabled: false},
		logger:         logger,
	})

	token, err := codec.Issue(ctx, port.Claims{
		PersonID: adminID, Persona: "employee", EmploymentStatus: "asn",
		TenantID: tenantID, TenantRoles: []string{adminRole.Name},
	})
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	post := func(t *testing.T, body string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/admin/iam/tenant-role-assignments", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		handler.ServeHTTP(rec, req)
		return rec
	}
	assignBody := func(unit *uuid.UUID) string {
		b, err := json.Marshal(map[string]any{
			"user_id": uuid.New(), "role_id": targetRole.ID, "unit_kerja_id": unit,
		})
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		return string(b)
	}

	// === Dalam jangkauan: Dinas A → 201 ===
	if rec := post(t, assignBody(&unitA)); rec.Code != http.StatusCreated {
		t.Fatalf("menugaskan role di unit SENDIRI: mau 201, dapat %d — body: %s", rec.Code, rec.Body.String())
	}

	// === Di luar jangkauan: Dinas B, TOKEN YANG SAMA → 403 ===
	// Inilah DoD. Yang berbeda dari request sebelumnya HANYA satu UUID di body.
	recB := post(t, assignBody(&unitB))
	if recB.Code != http.StatusForbidden {
		t.Fatalf("menugaskan role di unit ORANG LAIN: mau 403, dapat %d — body: %s\n"+
			"200/201 di sini berarti scope unit tak pernah dievaluasi: evaluator tak terpasang di "+
			"middleware, atau use case memakai RequirePermission tanpa unit", recB.Code, recB.Body.String())
	}

	// === Se-tenant (unit_kerja_id null) → 403 ===
	// Eskalasi lewat field yang DIBIARKAN KOSONG: null = seluruh tenant, jangkauan terluas.
	// Admin ber-scope satu OPD tak boleh memperolehnya hanya dengan tidak mengisi field.
	recNull := post(t, assignBody(nil))
	if recNull.Code != http.StatusForbidden {
		t.Fatalf("menugaskan role SE-TENANT dari admin ber-scope satu unit: mau 403, dapat %d — body: %s",
			recNull.Code, recNull.Body.String())
	}

	// === Subtree pada unit SENDIRI → 403 ===
	// Eskalasi lewat BOOLEAN: admin berwenang atas Dinas A saja (assignment-nya tanpa
	// include_subtree), jadi ia tak boleh menerbitkan penugasan yang menjangkau seluruh keturunan
	// Dinas A — jangkauan yang ia sendiri tak punya.
	subtreeBody, err := json.Marshal(map[string]any{
		"user_id": uuid.New(), "role_id": targetRole.ID,
		"unit_kerja_id": unitA, "include_subtree": true,
	})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	if rec := post(t, string(subtreeBody)); rec.Code != http.StatusForbidden {
		t.Fatalf("penugasan ber-include_subtree dari admin tanpa wewenang subtree: mau 403, dapat %d "+
			"— body: %s", rec.Code, rec.Body.String())
	}

	// === Baris yang tersimpan hanya SATU (yang sah) ===
	// Membuktikan penolakan terjadi SEBELUM penulisan, bukan sesudahnya.
	var n int
	if err := tenantPool.QueryRow(ctx,
		`SELECT count(*) FROM gov.user_role_assignments WHERE role_id = $1`, targetRole.ID).Scan(&n); err != nil {
		t.Fatalf("hitung assignment: %v", err)
	}
	if n != 1 {
		t.Fatalf("assignment tersimpan = %d, mau 1 (hanya yang di unit sendiri)", n)
	}
}
