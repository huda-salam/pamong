//go:build integration

package db_test

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core/audit"
	"github.com/huda-salam/pamong/core/permission"
	"github.com/huda-salam/pamong/identity/adapter/db"
	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/identity/usecase"
	infradb "github.com/huda-salam/pamong/infra/db"
	"github.com/huda-salam/pamong/testkit"
)

// applyCentralRoleMigration menerapkan migrasi 004 (id.central_roles + grant + assignment)
// di atas schema id yang sudah disiapkan setupIdentityDB (001).
func applyCentralRoleMigration(t *testing.T, pool *infradb.Pool, ctx context.Context) {
	t.Helper()
	upSQL, err := os.ReadFile("../../migrations/004_create_central_roles.up.sql")
	if err != nil {
		t.Fatalf("baca migrasi 004: %v", err)
	}
	if _, err := pool.Exec(ctx, string(upSQL)); err != nil {
		t.Fatalf("apply migrasi 004: %v", err)
	}
}

const (
	permTenantBaca     = "identity:tenant:baca"
	permTenantNonaktif = "identity:tenant:nonaktif"
	tenantA            = "pemkot-surabaya"
	tenantB            = "pemkot-malang"
)

// TestCentralRole_GlobalVsScoped_EndToEnd membuktikan DoD PR-2.3.2 menembus seluruh lapis:
// persist (use case+repo) -> catalog DB (snapshot) -> resolver (scope) -> Engine.
//
//   - Role GLOBAL berlaku di SEMUA tenant.
//   - Role SCOPED hanya berlaku di tenant dalam tenant_scope-nya.
func TestCentralRole_GlobalVsScoped_EndToEnd(t *testing.T) {
	pool, ctx := setupIdentityDB(t)
	applyCentralRoleMigration(t, pool, ctx)

	roleRepo := db.NewCentralRoleRepo(pool)
	assignRepo := db.NewCentralRoleAssignmentRepo(pool)
	resolver := db.NewCentralRoleResolver(pool)

	// Actor (admin platform) + subjek penerima role — keduanya person nyata (FK).
	actor := seedPerson(t, pool, ctx, "3500000000000001", "Admin Platform")
	subject := seedPerson(t, pool, ctx, "3578010101900001", "Budi")

	actx := testkit.Ctx(t,
		testkit.WithPersonID(actor),
		testkit.WithPermission(domain.PermCentralRoleBuat),
		testkit.WithPermission(domain.PermCentralRoleAssign),
	)

	// Role global (mis. auditor platform) -> izin baca tenant; berlaku semua tenant.
	globalRole, err := usecase.NewCreateCentralRole(roleRepo).Execute(actx, usecase.CreateCentralRoleInput{
		Name: "platform_auditor", Label: "Auditor Platform", ScopeType: domain.ScopeGlobal,
		Permissions: []string{permTenantBaca},
	})
	if err != nil {
		t.Fatalf("create global role: %v", err)
	}
	// Role scoped (mis. helpdesk regional) -> izin nonaktif; hanya berlaku di tenantA.
	scopedRole, err := usecase.NewCreateCentralRole(roleRepo).Execute(actx, usecase.CreateCentralRoleInput{
		Name: "regional_helpdesk", Label: "Helpdesk Regional", ScopeType: domain.ScopeScoped,
		Permissions: []string{permTenantNonaktif},
	})
	if err != nil {
		t.Fatalf("create scoped role: %v", err)
	}

	if _, err := usecase.NewAssignCentralRole(roleRepo, assignRepo).Execute(actx,
		usecase.AssignCentralRoleInput{PersonID: subject, RoleID: globalRole.ID}); err != nil {
		t.Fatalf("assign global: %v", err)
	}
	if _, err := usecase.NewAssignCentralRole(roleRepo, assignRepo).Execute(actx,
		usecase.AssignCentralRoleInput{PersonID: subject, RoleID: scopedRole.ID, TenantScope: []string{tenantA}}); err != nil {
		t.Fatalf("assign scoped: %v", err)
	}

	// Catalog snapshot dari DB + Engine (lapis evaluasi nyata).
	catalog, err := db.NewCentralRoleCatalog(ctx, roleRepo)
	if err != nil {
		t.Fatalf("build catalog: %v", err)
	}
	engine := permission.NewEngine(catalog)

	// allows merangkai alur runtime: resolve role efektif utk tenant lalu evaluasi.
	allows := func(tenantID, perm string) bool {
		roles, err := resolver.EffectiveRoles(ctx, subject, tenantID)
		if err != nil {
			t.Fatalf("resolve roles (%s): %v", tenantID, err)
		}
		return engine.Allows(roles, perm)
	}

	// Global: izin baca berlaku di kedua tenant.
	if !allows(tenantA, permTenantBaca) {
		t.Errorf("role global harus berlaku di %s", tenantA)
	}
	if !allows(tenantB, permTenantBaca) {
		t.Errorf("role global harus berlaku di %s", tenantB)
	}

	// Scoped: izin nonaktif hanya di tenantA, TIDAK di tenantB.
	if !allows(tenantA, permTenantNonaktif) {
		t.Errorf("role scoped harus berlaku di scope-nya (%s)", tenantA)
	}
	if allows(tenantB, permTenantNonaktif) {
		t.Errorf("role scoped TIDAK boleh berlaku di luar scope (%s)", tenantB)
	}
}

// TestCentralRole_Audited membuktikan pembuatan & penugasan role sentral ter-audit otomatis
// (ADR-003), tanpa kode audit di use case.
func TestCentralRole_Audited(t *testing.T) {
	pool, ctx := setupIdentityDB(t)
	applyCentralRoleMigration(t, pool, ctx)

	auditStore := db.NewAuditStore(pool)
	if err := auditStore.EnsureSchema(ctx); err != nil {
		t.Fatalf("ensure audit schema: %v", err)
	}
	engine := audit.NewEngine(auditStore)

	roleRepo := db.NewAuditedCentralRoleRepo(db.NewCentralRoleRepo(pool), engine)
	assignRepo := db.NewAuditedCentralRoleAssignmentRepo(db.NewCentralRoleAssignmentRepo(pool), engine)

	actor := seedPerson(t, pool, ctx, "3500000000000001", "Admin Platform")
	subject := seedPerson(t, pool, ctx, "3578010101900001", "Budi")
	actx := testkit.Ctx(t,
		testkit.WithPersonID(actor),
		testkit.WithPermission(domain.PermCentralRoleBuat),
		testkit.WithPermission(domain.PermCentralRoleAssign),
	)

	role, err := usecase.NewCreateCentralRole(roleRepo).Execute(actx, usecase.CreateCentralRoleInput{
		Name: "platform_auditor", Label: "Auditor Platform", ScopeType: domain.ScopeGlobal,
		Permissions: []string{permTenantBaca},
	})
	if err != nil {
		t.Fatalf("create role: %v", err)
	}
	assignment, err := usecase.NewAssignCentralRole(roleRepo, assignRepo).Execute(actx,
		usecase.AssignCentralRoleInput{PersonID: subject, RoleID: role.ID})
	if err != nil {
		t.Fatalf("assign: %v", err)
	}

	roleEntries, err := auditStore.ByEntity(ctx, "identity.CentralRole", role.ID)
	if err != nil {
		t.Fatalf("byEntity role: %v", err)
	}
	if len(roleEntries) != 1 || roleEntries[0].Action != audit.ActionCreate || roleEntries[0].ActorID != actor {
		t.Fatalf("audit pembuatan role salah: %+v", roleEntries)
	}

	assignEntries, err := auditStore.ByEntity(ctx, "identity.CentralRoleAssignment", assignment.ID)
	if err != nil {
		t.Fatalf("byEntity assignment: %v", err)
	}
	if len(assignEntries) != 1 || assignEntries[0].ActorID != actor {
		t.Fatalf("audit penugasan role salah: %+v", assignEntries)
	}
}

// TestCentralRoleResolver_ScopedTanpaScope_FailClosed membuktikan perbaikan review PR-2.3.2:
// assignment SCOPED dengan tenant_scope kosong (data rusak yang melewati use case — di sini
// disuntik via SQL langsung, mensimulasikan migrasi/bulk-import) TIDAK berlaku di tenant mana
// pun. Otoritas global vs scoped = scope_type role, bukan kekosongan tenant_scope (fail-closed).
func TestCentralRoleResolver_ScopedTanpaScope_FailClosed(t *testing.T) {
	pool, ctx := setupIdentityDB(t)
	applyCentralRoleMigration(t, pool, ctx)

	subject := seedPerson(t, pool, ctx, "3578010101900099", "Subjek Cacat")
	roleID := uuid.New()
	// Sengaja lewati use case: role 'scoped' + assignment TANPA tenant_scope (NULL).
	if _, err := pool.Exec(ctx,
		`INSERT INTO id.central_roles (id, name, label, scope_type) VALUES ($1,'regional_broken','Broken','scoped')`,
		roleID); err != nil {
		t.Fatalf("insert role rusak: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO id.central_role_assignments (id, person_id, role_id, assigned_by) VALUES ($1,$2,$3,$2)`,
		uuid.New(), subject, roleID); err != nil {
		t.Fatalf("insert assignment rusak: %v", err)
	}

	roles, err := db.NewCentralRoleResolver(pool).EffectiveRoles(ctx, subject, tenantA)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(roles) != 0 {
		t.Fatalf("scoped tanpa scope harus tak berlaku di mana pun (fail-closed), dapat: %v", roles)
	}
}

// TestCentralRoleRepo_DuplicatePermissionIdempoten membuktikan perbaikan review PR-2.3.2:
// permission duplikat pada Save tidak menggagalkan transaksi (ON CONFLICT DO NOTHING) dan
// hanya tersimpan satu baris — batas pertahanan repo untuk caller non-use-case.
func TestCentralRoleRepo_DuplicatePermissionIdempoten(t *testing.T) {
	pool, ctx := setupIdentityDB(t)
	applyCentralRoleMigration(t, pool, ctx)

	repo := db.NewCentralRoleRepo(pool)
	role := &domain.CentralRole{
		ID: uuid.New(), Name: "platform_auditor", Label: "Auditor", ScopeType: domain.ScopeGlobal,
		Permissions: []string{permTenantBaca, permTenantBaca}, // duplikat sengaja
	}
	if err := repo.Save(ctx, role); err != nil {
		t.Fatalf("Save dengan permission duplikat harus sukses (idempoten): %v", err)
	}
	got, err := repo.FindByID(ctx, role.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if len(got.Permissions) != 1 || got.Permissions[0] != permTenantBaca {
		t.Fatalf("grant duplikat harus tersimpan satu baris, dapat: %v", got.Permissions)
	}
}

// seedPerson menyimpan satu person minimal sebagai target FK dan mengembalikan id-nya.
func seedPerson(t *testing.T, pool *infradb.Pool, ctx context.Context, nik, nama string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if err := db.NewPersonRepo(pool).Save(ctx, &domain.Person{
		ID: id, NIK: nik, NamaLengkap: nama, IsActive: true,
	}); err != nil {
		t.Fatalf("seed person %s: %v", nama, err)
	}
	return id
}
