//go:build integration

package config_test

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	coreCfg "github.com/huda-salam/pamong/core/config"
	infraCfg "github.com/huda-salam/pamong/infra/config"
	"github.com/huda-salam/pamong/infra/db"
)

func newTenantConfigEnv(t *testing.T) (*coreCfg.Resolver, *infraCfg.DBTenantConfigStore, context.Context) {
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

	_, _ = pool.Exec(ctx,
		`DROP TABLE IF EXISTS gov.tenant_configs;
		 DROP INDEX IF EXISTS gov.idx_tenant_config_lookup`)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DROP TABLE IF EXISTS gov.tenant_configs;
			 DROP INDEX IF EXISTS gov.idx_tenant_config_lookup`)
		pgpool.Close()
	})

	store := infraCfg.NewDBTenantConfigStore(pool)
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	return coreCfg.NewResolver(store), store, ctx
}

func ptr(u uuid.UUID) *uuid.UUID { return &u }

// TestDBTenantConfig_UnitOverridesTenant — DoD integrasi PR-3.3.2: scope unit kerja
// meng-override level tenant, di-resolve lewat store Postgres nyata.
func TestDBTenantConfig_UnitOverridesTenant(t *testing.T) {
	r, _, ctx := newTenantConfigEnv(t)
	const tenant = "pemkot-surabaya"
	unit := uuid.New()
	aktor := uuid.New()

	if err := r.Set(ctx, coreCfg.ConfigEntry{
		Scope: coreCfg.ConfigScope{TenantID: tenant},
		Key:   "keuangan.persediaan", Value: "fifo", SetBy: &aktor,
	}); err != nil {
		t.Fatalf("set tenant-level: %v", err)
	}
	if err := r.Set(ctx, coreCfg.ConfigEntry{
		Scope: coreCfg.ConfigScope{TenantID: tenant, UnitKerjaID: ptr(unit)},
		Key:   "keuangan.persediaan", Value: "average", SetBy: &aktor,
	}); err != nil {
		t.Fatalf("set unit-level: %v", err)
	}

	// Query pada unit → unit-level menang.
	if v, ok, err := r.Resolve(ctx,
		coreCfg.ConfigScope{TenantID: tenant, UnitKerjaID: ptr(unit)},
		"keuangan.persediaan"); err != nil || !ok || v != "average" {
		t.Fatalf("unit scope: want average, got %q ok=%v err=%v", v, ok, err)
	}
	// Query pada unit lain → fallback ke tenant-level.
	if v, ok, err := r.Resolve(ctx,
		coreCfg.ConfigScope{TenantID: tenant, UnitKerjaID: ptr(uuid.New())},
		"keuangan.persediaan"); err != nil || !ok || v != "fifo" {
		t.Fatalf("unit lain: want fifo, got %q ok=%v err=%v", v, ok, err)
	}
	// Query level tenant → tenant-level.
	if v, ok, err := r.Resolve(ctx,
		coreCfg.ConfigScope{TenantID: tenant}, "keuangan.persediaan"); err != nil || !ok || v != "fifo" {
		t.Fatalf("tenant scope: want fifo, got %q ok=%v err=%v", v, ok, err)
	}
}

// TestDBTenantConfig_Upsert memverifikasi UNIQUE NULLS NOT DISTINCT: set kedua pada scope
// tenant-level yang sama menimpa nilai, tidak menggandakan baris.
func TestDBTenantConfig_Upsert(t *testing.T) {
	r, store, ctx := newTenantConfigEnv(t)
	const tenant = "pemkot-malang"
	scope := coreCfg.ConfigScope{TenantID: tenant}

	if err := r.Set(ctx, coreCfg.ConfigEntry{Scope: scope, Key: "k", Value: "fifo"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Set(ctx, coreCfg.ConfigEntry{Scope: scope, Key: "k", Value: "average"}); err != nil {
		t.Fatal(err)
	}

	cands, err := store.Candidates(ctx, tenant, "k")
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 1 {
		t.Fatalf("want 1 baris setelah upsert, got %d", len(cands))
	}
	if cands[0].Value != "average" {
		t.Fatalf("want average, got %q", cands[0].Value)
	}
}

// TestDBTenantConfig_ScopeCheckConstraint memverifikasi CHECK menolak resource tanpa unit.
// Validasi Go menolak lebih dulu; ini memastikan DB juga fail-closed.
func TestDBTenantConfig_RejectsResourceWithoutUnit(t *testing.T) {
	r, _, ctx := newTenantConfigEnv(t)
	err := r.Set(ctx, coreCfg.ConfigEntry{
		Scope: coreCfg.ConfigScope{TenantID: "t", ResourceID: ptr(uuid.New())},
		Key:   "k", Value: "v",
	})
	if err == nil {
		t.Fatal("want error untuk resource tanpa unit")
	}
}
