//go:build integration

package sequence_test

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/huda-salam/pamong/core/config"
	"github.com/huda-salam/pamong/infra/db"
	infraSeq "github.com/huda-salam/pamong/infra/sequence"
	"github.com/huda-salam/pamong/port"
)

// fixedResolver mengembalikan satu tenant yang menunjuk ke DB test (host kosong → fallback ke
// shared.Host manager). Cukup untuk menguji generator DB via TenantConnManager tanpa registry.
type fixedResolver struct{ dbName string }

func (r fixedResolver) Resolve(_ context.Context, tenantID string) (*port.TenantInfo, error) {
	return &port.TenantInfo{TenantID: tenantID, DBName: r.dbName, IsActive: true}, nil
}

// newSeqEnv merakit generator DB di atas DSN test. connMgr diarahkan ke DB yang sama dengan
// DSN agar Tenant() membuka pool ke DB test; skema di-reset penuh.
func newSeqEnv(t *testing.T) (*infraSeq.DBGenerator, context.Context) {
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
		PoolMax: 8, PoolIdle: 1,
	}
	connMgr := db.NewTenantConnManager(fixedResolver{dbName: cp.Database}, shared, config.CentralDBConfig{})
	t.Cleanup(connMgr.Close)

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

	return infraSeq.NewDBGenerator(connMgr), ctx
}

// TestDBGenerator_MonotonicDanFormat: nomor menaik berurutan & ter-render sesuai pola.
func TestDBGenerator_MonotonicDanFormat(t *testing.T) {
	gen, ctx := newSeqEnv(t)
	want := []string{"2025/AG/00001", "2025/AG/00002", "2025/AG/00003"}
	for i, w := range want {
		got, err := gen.Next(ctx, "pemkot-a", "{tahun}/AG/{nomor:5}", 2025)
		if err != nil {
			t.Fatalf("Next#%d: %v", i+1, err)
		}
		if got != w {
			t.Fatalf("Next#%d = %q, ingin %q", i+1, got, w)
		}
	}
}

// TestDBGenerator_ResetPerTahun: tahun berbeda punya penghitung independen (mulai dari 1).
func TestDBGenerator_ResetPerTahun(t *testing.T) {
	gen, ctx := newSeqEnv(t)
	if _, err := gen.Next(ctx, "pemkot-a", "{nomor:3}", 2025); err != nil {
		t.Fatalf("Next 2025: %v", err)
	}
	got, err := gen.Next(ctx, "pemkot-a", "{nomor:3}", 2026)
	if err != nil {
		t.Fatalf("Next 2026: %v", err)
	}
	if got != "001" {
		t.Fatalf("tahun baru harus mulai dari 1; got %q", got)
	}
}

// TestDBGenerator_PolaBerbedaTerpisah: pola berbeda = penghitung berbeda pada tahun sama.
func TestDBGenerator_PolaBerbedaTerpisah(t *testing.T) {
	gen, ctx := newSeqEnv(t)
	if _, err := gen.Next(ctx, "pemkot-a", "AG-{nomor}", 2025); err != nil {
		t.Fatalf("Next AG: %v", err)
	}
	got, err := gen.Next(ctx, "pemkot-a", "SPM-{nomor}", 2025)
	if err != nil {
		t.Fatalf("Next SPM: %v", err)
	}
	if got != "SPM-1" {
		t.Fatalf("pola berbeda harus punya penghitung sendiri; got %q", got)
	}
}

// TestDBGenerator_Konkuren: 50 pemanggilan paralel menghasilkan 50 nomor UNIK (increment
// atomik — tak ada duplikat karena baris dikunci selama UPDATE ... RETURNING).
func TestDBGenerator_Konkuren(t *testing.T) {
	gen, ctx := newSeqEnv(t)
	const n = 50
	var wg sync.WaitGroup
	var mu sync.Mutex
	seen := make(map[string]bool, n)
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := gen.Next(ctx, "pemkot-a", "{nomor:5}", 2025)
			if err != nil {
				errs <- err
				return
			}
			mu.Lock()
			if seen[got] {
				mu.Unlock()
				errs <- errDuplikat(got)
				return
			}
			seen[got] = true
			mu.Unlock()
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("konkuren: %v", err)
	}
	if len(seen) != n {
		t.Fatalf("harus %d nomor unik, dapat %d", n, len(seen))
	}
}

type errDuplikatType string

func (e errDuplikatType) Error() string { return "nomor duplikat: " + string(e) }
func errDuplikat(s string) error        { return errDuplikatType(s) }
