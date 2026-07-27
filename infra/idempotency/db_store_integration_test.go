//go:build integration

package idempotency_test

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/huda-salam/pamong/core/config"
	"github.com/huda-salam/pamong/infra/db"
	infraIdem "github.com/huda-salam/pamong/infra/idempotency"
	"github.com/huda-salam/pamong/port"
)

// fixedResolver mengembalikan satu tenant yang menunjuk ke DB test (host kosong → fallback ke
// shared.Host manager). Cukup untuk menguji store DB via TenantConnManager tanpa registry.
type fixedResolver struct{ dbName string }

func (r fixedResolver) Resolve(_ context.Context, tenantID string) (*port.TenantInfo, error) {
	return &port.TenantInfo{TenantID: tenantID, DBName: r.dbName, IsActive: true}, nil
}

// newIdemEnv merakit store DB di atas DSN test. connMgr diarahkan ke DB yang sama dengan DSN
// agar Tenant() membuka pool ke DB test; skema di-reset penuh.
func newIdemEnv(t *testing.T) (*infraIdem.DBStore, *db.Pool, context.Context) {
	t.Helper()
	dsn := os.Getenv("PAMONG_TEST_DB_DSN")
	if dsn == "" {
		t.Skip("PAMONG_TEST_DB_DSN tidak diset — lewati integration test")
	}
	ctx := context.Background()
	pgcfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse DSN: %v", err)
	}
	cp := pgcfg.ConnConfig
	shared := config.DBConfig{
		Host: cp.Host, Port: int(cp.Port), User: cp.User, Password: cp.Password,
		PoolMax: 5, PoolIdle: 1,
	}
	connMgr := db.NewTenantConnManager(fixedResolver{dbName: cp.Database}, shared, config.CentralDBConfig{})
	t.Cleanup(connMgr.Close)

	// Pool langsung untuk reset + inspeksi.
	pgpool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("buka pool: %v", err)
	}
	pool := db.NewPool(pgpool)
	_, _ = pool.Exec(ctx, `DROP SCHEMA IF EXISTS gov CASCADE`)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DROP SCHEMA IF EXISTS gov CASCADE`)
		pgpool.Close()
	})

	return infraIdem.NewDBStore(connMgr), pool, ctx
}

func TestDBStore_ReserveCompleteReplay(t *testing.T) {
	store, _, ctx := newIdemEnv(t)
	person := uuid.New()

	// Reservasi pertama → reserved.
	rec, reserved, err := store.Reserve(ctx, "pemkot-a", person, "k1", "fp1")
	if err != nil {
		t.Fatalf("reserve#1: %v", err)
	}
	if !reserved || rec != nil {
		t.Fatalf("reserve#1 harus reserved=true rec=nil, got reserved=%v rec=%v", reserved, rec)
	}

	// Sebelum Complete: reservasi kedua (kembar in-flight) → tidak reserved, belum completed.
	rec, reserved, err = store.Reserve(ctx, "pemkot-a", person, "k1", "fp1")
	if err != nil {
		t.Fatalf("reserve#2: %v", err)
	}
	if reserved || rec == nil || rec.Completed {
		t.Fatalf("reserve#2 harus reserved=false & belum completed, got reserved=%v rec=%+v", reserved, rec)
	}

	// Complete lalu Reserve lagi → replay dengan status+body tersimpan.
	if err := store.Complete(ctx, "pemkot-a", person, "k1", 201, []byte(`{"id":"x"}`)); err != nil {
		t.Fatalf("complete: %v", err)
	}
	rec, reserved, err = store.Reserve(ctx, "pemkot-a", person, "k1", "fp1")
	if err != nil {
		t.Fatalf("reserve#3: %v", err)
	}
	if reserved || rec == nil || !rec.Completed || rec.Status != 201 || string(rec.Body) != `{"id":"x"}` {
		t.Fatalf("reserve#3 harus replay completed 201; got reserved=%v rec=%+v", reserved, rec)
	}
}

func TestDBStore_ScopePerPrincipal(t *testing.T) {
	store, _, ctx := newIdemEnv(t)
	a, b := uuid.New(), uuid.New()

	// Person A reserve & complete key "shared".
	if _, _, err := store.Reserve(ctx, "pemkot-a", a, "shared", "fpA"); err != nil {
		t.Fatalf("reserve A: %v", err)
	}
	if err := store.Complete(ctx, "pemkot-a", a, "shared", 200, []byte("A")); err != nil {
		t.Fatalf("complete A: %v", err)
	}
	// Person B pakai NILAI key sama → HARUS reservasi baru (bukan replay respons A).
	rec, reserved, err := store.Reserve(ctx, "pemkot-a", b, "shared", "fpB")
	if err != nil {
		t.Fatalf("reserve B: %v", err)
	}
	if !reserved || rec != nil {
		t.Fatalf("key di-scope per-principal: B harus reservasi baru, tak melihat entri A; got reserved=%v rec=%+v", reserved, rec)
	}
}

func TestDBStore_ReleaseMembolehkanRetry(t *testing.T) {
	store, _, ctx := newIdemEnv(t)
	person := uuid.New()

	if _, _, err := store.Reserve(ctx, "pemkot-a", person, "k1", "fp1"); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if err := store.Release(ctx, "pemkot-a", person, "k1"); err != nil {
		t.Fatalf("release: %v", err)
	}
	// Setelah Release, key bebas → reservasi baru berhasil.
	_, reserved, err := store.Reserve(ctx, "pemkot-a", person, "k1", "fp1")
	if err != nil {
		t.Fatalf("reserve setelah release: %v", err)
	}
	if !reserved {
		t.Fatal("setelah Release, reservasi ulang harus reserved=true")
	}
}
