//go:build integration

package db_test

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core/audit"
	"github.com/huda-salam/pamong/identity/adapter/db"
	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/identity/usecase"
	infradb "github.com/huda-salam/pamong/infra/db"
	"github.com/huda-salam/pamong/testkit"
)

// applyTenantMigration menerapkan migrasi 002 (tenant_registry) di atas schema id.
func applyTenantMigration(t *testing.T, pool *infradb.Pool) {
	t.Helper()
	sql, err := os.ReadFile("../../migrations/002_create_tenant_registry.up.sql")
	if err != nil {
		t.Fatalf("baca migrasi 002: %v", err)
	}
	if _, err := pool.Exec(context.Background(), string(sql)); err != nil {
		t.Fatalf("apply migrasi 002: %v", err)
	}
}

func TestTenantRegistry_CRUD_Aktif(t *testing.T) {
	pool, ctx := setupIdentityDB(t)
	applyTenantMigration(t, pool)

	registry := db.NewTenantRepo(pool)
	actx := testkit.Ctx(t,
		testkit.WithPersonID(uuid.New()),
		testkit.WithPermission(domain.PermTenantDaftar),
		testkit.WithPermission(domain.PermTenantBaca),
		testkit.WithPermission(domain.PermTenantNonaktif),
	)

	// Buat.
	_, err := usecase.NewRegisterTenant(registry).Execute(actx, usecase.RegisterTenantInput{
		TenantID: "pemkot-surabaya", Nama: "Pemkot Surabaya", DBHost: "db1", DBName: "gov_pemkot_surabaya",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	// Duplikat -> konflik.
	if _, err := usecase.NewRegisterTenant(registry).Execute(actx, usecase.RegisterTenantInput{
		TenantID: "pemkot-surabaya", Nama: "Dup", DBHost: "db1", DBName: "x",
	}); err == nil {
		t.Fatal("tenant duplikat harus ditolak")
	}

	// List.
	list, err := usecase.NewListTenants(registry).Execute(actx)
	if err != nil || len(list) != 1 || list[0].IsActive != true {
		t.Fatalf("list salah: %v / %+v", err, list)
	}

	// Nonaktifkan.
	if err := usecase.NewDeactivateTenant(registry).Execute(actx, "pemkot-surabaya"); err != nil {
		t.Fatalf("deactivate: %v", err)
	}
	got, err := registry.FindByID(ctx, "pemkot-surabaya")
	if err != nil || got.IsActive {
		t.Fatalf("tenant harus nonaktif: %v / %+v", err, got)
	}
}

func TestTenantRegistry_Mutasi_TerAudit(t *testing.T) {
	pool, ctx := setupIdentityDB(t)
	applyTenantMigration(t, pool)

	auditStore := db.NewAuditStore(pool)
	if err := auditStore.EnsureSchema(ctx); err != nil {
		t.Fatalf("ensure audit: %v", err)
	}
	engine := audit.NewEngine(auditStore)
	registry := db.NewAuditedTenantRepo(db.NewTenantRepo(pool), engine)

	actor := uuid.New()
	actx := testkit.Ctx(t,
		testkit.WithPersonID(actor),
		testkit.WithPermission(domain.PermTenantDaftar),
		testkit.WithPermission(domain.PermTenantNonaktif),
	)

	if _, err := usecase.NewRegisterTenant(registry).Execute(actx, usecase.RegisterTenantInput{
		TenantID: "pemkot-malang", Nama: "Pemkot Malang", DBHost: "db1", DBName: "gov_pemkot_malang",
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := usecase.NewDeactivateTenant(registry).Execute(actx, "pemkot-malang"); err != nil {
		t.Fatalf("deactivate: %v", err)
	}

	// Audit: create + update(is_active) tercatat di chain identity & utuh.
	chain, err := auditStore.Chain(ctx)
	if err != nil {
		t.Fatalf("chain: %v", err)
	}
	if len(chain) != 2 {
		t.Fatalf("harus 2 entry (create+update tenant), dapat %d", len(chain))
	}
	if chain[0].Action != audit.ActionCreate || chain[1].Action != audit.ActionUpdate {
		t.Fatalf("aksi audit salah: %s/%s", chain[0].Action, chain[1].Action)
	}
	if chain[0].ActorID != actor {
		t.Fatalf("actor tidak terekam: %+v", chain[0])
	}
	if res := audit.VerifyChain(chain); !res.OK {
		t.Fatalf("chain harus utuh: %+v", res)
	}
}
