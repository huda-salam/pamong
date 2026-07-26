//go:build integration

package workflow_test

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/huda-salam/pamong/infra/db"
	infraWf "github.com/huda-salam/pamong/infra/workflow"
	tenantroledb "github.com/huda-salam/pamong/tenantrole/adapter/db"
	"github.com/huda-salam/pamong/tenantrole/domain"
)

func newRoleCheckerEnv(t *testing.T) (*db.Pool, context.Context) {
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
	pool := db.NewPool(pgpool)

	drop := func() {
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

// TestTenantRoleChecker_RoleExists membuktikan RoleChecker (PR-N2 bagian C) membedakan role
// terdaftar dari yang tidak, tanpa melibatkan tenantID (isolasi tenant STRUKTURAL lewat pool).
func TestTenantRoleChecker_RoleExists(t *testing.T) {
	pool, ctx := newRoleCheckerEnv(t)
	if err := tenantroledb.NewTenantRoleRepo(pool).Save(ctx, &domain.TenantRole{
		ID: uuid.New(), Name: "ppk_opd", Label: "PPK OPD",
	}); err != nil {
		t.Fatalf("seed role: %v", err)
	}

	checker := infraWf.NewTenantRoleChecker(pool)

	ok, err := checker.RoleExists(ctx, "tenant-a", "ppk_opd")
	if err != nil {
		t.Fatalf("RoleExists (terdaftar): %v", err)
	}
	if !ok {
		t.Error("role terdaftar harus RoleExists=true")
	}

	ok, err = checker.RoleExists(ctx, "tenant-a", "role_siluman")
	if err != nil {
		t.Fatalf("RoleExists (tak terdaftar): %v", err)
	}
	if ok {
		t.Error("role tak terdaftar harus RoleExists=false, bukan error")
	}
}
