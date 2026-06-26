//go:build integration

package db_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core/permission"
	"github.com/huda-salam/pamong/tenantrole/adapter/db"
	"github.com/huda-salam/pamong/tenantrole/domain"
	"github.com/huda-salam/pamong/tenantrole/usecase"
	"github.com/huda-salam/pamong/testkit"
)

// TestABAC_UnitScope_EndToEnd membuktikan DoD PR-2.3.5a: permission dibatasi ke unit kerja &
// ditegakkan saat evaluasi data-level — menembus persist (assignment ber-unit) → resolver
// scoped-grant → ScopedEngine + hierarki OPD (gov.org_units).
//
//   - Assignment unit-only: hanya menjangkau unit itu, unit lain ditolak.
//   - Assignment subtree: menjangkau unit sendiri + keturunannya pada tree OPD.
func TestABAC_UnitScope_EndToEnd(t *testing.T) {
	pool, ctx := setupTenantRoleDB(t)
	roleRepo := db.NewTenantRoleRepo(pool)
	assignRepo := db.NewTenantRoleAssignmentRepo(pool)
	grants := db.NewTenantScopedGrantResolver(pool)
	roleResolver := db.NewTenantRoleResolver(pool)
	hierarchy := db.NewOrgUnitHierarchy(pool)

	actx := testkit.Ctx(t,
		testkit.WithPermission(domain.PermTenantRoleBuat),
		testkit.WithPermission(domain.PermTenantRoleAssign),
	)
	// Buat role lebih dulu: jalur ensure-on-write membuat schema gov (sekali) sebelum kita
	// menyentuh gov.org_units — menghindari CREATE SCHEMA kedua yang balapan dgn paket lain.
	role, err := usecase.NewCreateTenantRole(roleRepo).Execute(actx, usecase.CreateTenantRoleInput{
		Name: "verifikator_keuangan", Label: "Verifikator", Permissions: []string{permBaca},
	})
	if err != nil {
		t.Fatalf("create role: %v", err)
	}

	// Hierarki OPD: dinas (induk) → bidang (anak). Tabel di-drop bersih; schema gov sudah ada.
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DROP TABLE IF EXISTS gov.org_units`) })
	if _, err := pool.Exec(ctx, `DROP TABLE IF EXISTS gov.org_units`); err != nil {
		t.Fatalf("drop org_units: %v", err)
	}
	if _, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS gov.org_units (id UUID PRIMARY KEY, parent_id UUID REFERENCES gov.org_units(id), name VARCHAR(255) NOT NULL)`); err != nil {
		t.Fatalf("create org_units: %v", err)
	}
	dinas, bidang, lain := uuid.New(), uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO gov.org_units (id, parent_id, name) VALUES ($1,NULL,'Dinas'),($2,$1,'Bidang'),($3,NULL,'Lain')`,
		dinas, bidang, lain); err != nil {
		t.Fatalf("seed org_units: %v", err)
	}

	userUnit := uuid.New()    // assignment unit-only ke bidang
	userSubtree := uuid.New() // assignment subtree ke dinas
	if _, err := usecase.NewAssignTenantRole(assignRepo).Execute(actx, usecase.AssignTenantRoleInput{
		UserID: userUnit, RoleID: role.ID, UnitKerjaID: &bidang,
	}); err != nil {
		t.Fatalf("assign unit-only: %v", err)
	}
	if _, err := usecase.NewAssignTenantRole(assignRepo).Execute(actx, usecase.AssignTenantRoleInput{
		UserID: userSubtree, RoleID: role.ID, UnitKerjaID: &dinas, IncludeSubtree: true,
	}); err != nil {
		t.Fatalf("assign subtree: %v", err)
	}

	tenantCat, err := db.NewTenantRoleCatalog(ctx, roleRepo)
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	eng := permission.NewScopedEngine(permission.NewEngine(tenantCat), hierarchy)

	authorityOf := func(user uuid.UUID) permission.Authority {
		names, err := roleResolver.EffectiveRoles(ctx, user)
		if err != nil {
			t.Fatalf("resolve names: %v", err)
		}
		gr, err := grants.Grants(ctx, user)
		if err != nil {
			t.Fatalf("resolve grants: %v", err)
		}
		return permission.Authority{RoleNames: names, RoleGrants: gr}
	}
	check := func(user, unit uuid.UUID, want bool, msg string) {
		ok, err := eng.AllowsInUnit(ctx, authorityOf(user), permBaca, permission.ResourceScope{UnitKerjaID: unit})
		if err != nil {
			t.Fatalf("%s: error: %v", msg, err)
		}
		if ok != want {
			t.Errorf("%s: AllowsInUnit = %v, mau %v", msg, ok, want)
		}
	}

	// unit-only @ bidang
	check(userUnit, bidang, true, "unit-only boleh akses unit-nya")
	check(userUnit, dinas, false, "unit-only TAK boleh akses induk")
	check(userUnit, lain, false, "unit-only TAK boleh akses unit lain")

	// subtree @ dinas
	check(userSubtree, dinas, true, "subtree boleh akses unit sendiri")
	check(userSubtree, bidang, true, "subtree boleh akses keturunan")
	check(userSubtree, lain, false, "subtree TAK boleh akses unit di luar subtree")
}
