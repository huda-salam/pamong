//go:build integration

package db_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core/audit"
	"github.com/huda-salam/pamong/core/permission"
	infradb "github.com/huda-salam/pamong/infra/db"
	"github.com/huda-salam/pamong/tenantrole/adapter/db"
	"github.com/huda-salam/pamong/tenantrole/domain"
	"github.com/huda-salam/pamong/tenantrole/usecase"
	"github.com/huda-salam/pamong/testkit"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	permBaca   = "surat_masuk:surat:baca"
	permBuat   = "surat_masuk:surat:buat"
	permStrict = "keuangan:spm:terbitkan" // ditandai strict pada Engine
)

// setupTenantRoleDB membuka pool ke DB uji (PAMONG_TEST_DB_DSN) lalu membersihkan tabel role
// tenant PER-TABEL. JANGAN DROP SCHEMA gov CASCADE: schema gov dipakai bersama lintas-paket
// (mis. gov.audit_logs infra/db). Tabel dibuat lewat ensure-on-write oleh repo, jadi setup
// tidak perlu menerapkan migrasi.
func setupTenantRoleDB(t *testing.T) (*infradb.Pool, context.Context) {
	t.Helper()
	dsn := os.Getenv("PAMONG_TEST_DB_DSN")
	if dsn == "" {
		t.Skip("PAMONG_TEST_DB_DSN tidak diset — lewati integration test")
	}
	ctx := context.Background()
	pgpool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("buka pool: %v", err)
	}
	pool := infradb.NewPool(pgpool)
	drop := func() {
		_, _ = pool.Exec(context.Background(), `DROP TABLE IF EXISTS gov.user_role_assignments`)
		_, _ = pool.Exec(context.Background(), `DROP TABLE IF EXISTS gov.tenant_role_permissions`)
		_, _ = pool.Exec(context.Background(), `DROP TABLE IF EXISTS gov.tenant_roles`)
	}
	drop()
	t.Cleanup(func() {
		drop()
		pgpool.Close()
	})
	return pool, ctx
}

// TestTenantRole_Resolution_EndToEnd membuktikan DoD PR-2.3.3 menembus seluruh lapis:
// persist (use case+repo tenant DB) → catalog DB tenant (snapshot) → resolver → Engine
// resolusi penuh (composite central+tenant).
//
//   - Role tenant berlaku di tenant-nya: izin baca/buat dari role tenant dievaluasi (DoD).
//   - Strict ditegakkan (intersection): perm strict ditolak saat role tenant tak sepakat.
//   - Global menang atas tenant: role global (central) mengizinkan perm strict yang sama.
func TestTenantRole_Resolution_EndToEnd(t *testing.T) {
	pool, ctx := setupTenantRoleDB(t)
	roleRepo := db.NewTenantRoleRepo(pool)
	assignRepo := db.NewTenantRoleAssignmentRepo(pool)
	resolver := db.NewTenantRoleResolver(pool)

	actor := uuid.New()
	actx := testkit.Ctx(t,
		testkit.WithPersonID(actor),
		testkit.WithPermission(domain.PermTenantRoleBuat),
		testkit.WithPermission(domain.PermTenantRoleAssign),
	)

	// bendahara memberi perm strict; operator_surat TIDAK (keduanya memberi baca).
	bendahara, err := usecase.NewCreateTenantRole(roleRepo).Execute(actx, usecase.CreateTenantRoleInput{
		Name: "bendahara", Label: "Bendahara", Permissions: []string{permBaca, permStrict},
	})
	if err != nil {
		t.Fatalf("create bendahara: %v", err)
	}
	operator, err := usecase.NewCreateTenantRole(roleRepo).Execute(actx, usecase.CreateTenantRoleInput{
		Name: "operator_surat", Label: "Operator Surat", Permissions: []string{permBaca, permBuat},
	})
	if err != nil {
		t.Fatalf("create operator: %v", err)
	}

	user := uuid.New()
	for _, roleID := range []uuid.UUID{bendahara.ID, operator.ID} {
		if _, err := usecase.NewAssignTenantRole(assignRepo).Execute(actx,
			usecase.AssignTenantRoleInput{UserID: user, RoleID: roleID}); err != nil {
			t.Fatalf("assign %s: %v", roleID, err)
		}
	}

	// Lapis evaluasi nyata: catalog tenant (DB) + catalog central (di sini MemoryCatalog
	// dengan satu role global) digabung composite, lalu Engine dengan permStrict ditandai strict.
	tenantCat, err := db.NewTenantRoleCatalog(ctx, roleRepo)
	if err != nil {
		t.Fatalf("build catalog tenant: %v", err)
	}
	centralCat := permission.NewMemoryCatalog().
		Define("super_admin", permission.LayerGlobal, permStrict)
	engine := permission.NewEngine(permission.NewCompositeCatalog(centralCat, tenantCat), permStrict)

	tenantRoles, err := resolver.EffectiveRoles(ctx, user)
	if err != nil {
		t.Fatalf("resolve roles: %v", err)
	}
	if len(tenantRoles) != 2 || !contains(tenantRoles, "bendahara") || !contains(tenantRoles, "operator_surat") {
		t.Fatalf("role tenant efektif salah: %v", tenantRoles)
	}

	// DoD: role tenant berlaku — izin baca (union dari kedua role) & buat (dari operator).
	if !engine.Allows(tenantRoles, permBaca) {
		t.Error("role tenant harus memberi izin baca di tenant-nya")
	}
	if !engine.Allows(tenantRoles, permBuat) {
		t.Error("non-strict harus union: izin buat dari operator_surat")
	}
	// DoD: strict ditegakkan — operator_surat tak memberi permStrict → intersection gagal.
	if engine.Allows(tenantRoles, permStrict) {
		t.Error("strict: role tenant yang tak sepakat harus memblokir (intersection)")
	}
	// DoD: global menang — role global (central) mengizinkan permStrict yang sama.
	withGlobal := append(append([]string{}, tenantRoles...), "super_admin")
	if !engine.Allows(withGlobal, permStrict) {
		t.Error("role global (central) harus menang atas strict-deny role tenant")
	}
}

// TestTenantRole_Audited membuktikan pembuatan & penugasan role tenant ter-audit otomatis ke
// gov.audit_logs tenant DB (ADR-003), tanpa kode audit di use case. tenant_id unik mengisolasi
// hash-chain audit dari paket lain yang berbagi DB uji.
func TestTenantRole_Audited(t *testing.T) {
	pool, ctx := setupTenantRoleDB(t)

	auditRepo := infradb.NewAuditRepo(pool) // schema gov
	if err := auditRepo.EnsureSchema(ctx); err != nil {
		t.Fatalf("ensure audit schema: %v", err)
	}
	engine := audit.NewEngine(auditRepo)

	roleRepo := db.NewAuditedTenantRoleRepo(db.NewTenantRoleRepo(pool), engine)
	assignRepo := db.NewAuditedTenantRoleAssignmentRepo(db.NewTenantRoleAssignmentRepo(pool), engine)

	actor := uuid.New()
	tenantID := "tenant-audit-" + uuid.NewString()
	actx := testkit.Ctx(t,
		testkit.WithPersonID(actor),
		testkit.WithTenant(tenantID),
		testkit.WithPermission(domain.PermTenantRoleBuat),
		testkit.WithPermission(domain.PermTenantRoleAssign),
	)

	role, err := usecase.NewCreateTenantRole(roleRepo).Execute(actx, usecase.CreateTenantRoleInput{
		Name: "verifikator_keuangan", Label: "Verifikator", Permissions: []string{permBaca},
	})
	if err != nil {
		t.Fatalf("create role: %v", err)
	}
	assignment, err := usecase.NewAssignTenantRole(assignRepo).Execute(actx,
		usecase.AssignTenantRoleInput{UserID: uuid.New(), RoleID: role.ID})
	if err != nil {
		t.Fatalf("assign: %v", err)
	}

	roleEntries, err := auditRepo.ByEntity(ctx, "tenantrole.TenantRole", role.ID)
	if err != nil {
		t.Fatalf("byEntity role: %v", err)
	}
	if len(roleEntries) != 1 || roleEntries[0].Action != audit.ActionCreate || roleEntries[0].ActorID != actor {
		t.Fatalf("audit pembuatan role salah: %+v", roleEntries)
	}

	assignEntries, err := auditRepo.ByEntity(ctx, "tenantrole.TenantRoleAssignment", assignment.ID)
	if err != nil {
		t.Fatalf("byEntity assignment: %v", err)
	}
	if len(assignEntries) != 1 || assignEntries[0].ActorID != actor {
		t.Fatalf("audit penugasan role salah: %+v", assignEntries)
	}
}

// TestTenantRoleResolver_Expired membuktikan assignment kedaluwarsa tidak ikut ter-resolve.
func TestTenantRoleResolver_Expired(t *testing.T) {
	pool, ctx := setupTenantRoleDB(t)
	roleRepo := db.NewTenantRoleRepo(pool)
	assignRepo := db.NewTenantRoleAssignmentRepo(pool)
	resolver := db.NewTenantRoleResolver(pool)

	actx := testkit.Ctx(t,
		testkit.WithPermission(domain.PermTenantRoleBuat),
		testkit.WithPermission(domain.PermTenantRoleAssign),
	)
	role, err := usecase.NewCreateTenantRole(roleRepo).Execute(actx, usecase.CreateTenantRoleInput{
		Name: "ppk_opd", Label: "PPK OPD", Permissions: []string{permBaca},
	})
	if err != nil {
		t.Fatalf("create role: %v", err)
	}
	user := uuid.New()
	past := time.Now().Add(-time.Hour)
	from := past.Add(-time.Hour)
	if _, err := usecase.NewAssignTenantRole(assignRepo).Execute(actx, usecase.AssignTenantRoleInput{
		UserID: user, RoleID: role.ID, ValidFrom: from, ValidUntil: &past,
	}); err != nil {
		t.Fatalf("assign expired: %v", err)
	}

	roles, err := resolver.EffectiveRoles(ctx, user)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(roles) != 0 {
		t.Fatalf("assignment kedaluwarsa tidak boleh ter-resolve, dapat: %v", roles)
	}
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
