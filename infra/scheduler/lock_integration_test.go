//go:build integration

package scheduler_test

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	coreSched "github.com/huda-salam/pamong/core/scheduler"
	"github.com/huda-salam/pamong/infra/db"
	infraSched "github.com/huda-salam/pamong/infra/scheduler"
)

func newLockEnv(t *testing.T) (*infraSched.DBJobStore, *infraSched.DBLocker, context.Context) {
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

	drop := `DROP TABLE IF EXISTS gov.job_runs;
	         DROP TABLE IF EXISTS gov.scheduled_jobs;
	         DROP TABLE IF EXISTS gov.job_locks;`
	_, _ = pool.Exec(ctx, drop)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), drop)
		pgpool.Close()
	})

	store := infraSched.NewDBJobStore(pool)
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("ensure store schema: %v", err)
	}
	locker := infraSched.NewDBLocker(pool)
	if err := locker.EnsureSchema(ctx); err != nil {
		t.Fatalf("ensure lock schema: %v", err)
	}
	return store, locker, ctx
}

// TestDBLocker_AcquireIsExclusive — lock atomik: pemenang tunggal, kedaluwarsa bisa diambil alih.
func TestDBLocker_AcquireIsExclusive(t *testing.T) {
	_, locker, ctx := newLockEnv(t)

	lock, ok, err := locker.Acquire(ctx, "k", time.Minute)
	if err != nil || !ok {
		t.Fatalf("acquire pertama: ok=%v err=%v", ok, err)
	}
	if _, ok2, _ := locker.Acquire(ctx, "k", time.Minute); ok2 {
		t.Fatal("acquire kedua atas key aktif harus gagal")
	}
	if err := locker.Release(ctx, lock); err != nil {
		t.Fatalf("release: %v", err)
	}
	if _, ok3, _ := locker.Acquire(ctx, "k", time.Minute); !ok3 {
		t.Fatal("setelah release harus bisa acquire lagi")
	}

	// Sewa yang sudah kedaluwarsa boleh diambil alih.
	if _, ok4, _ := locker.Acquire(ctx, "expired", time.Nanosecond); !ok4 {
		t.Fatal("acquire ttl pendek gagal")
	}
	time.Sleep(5 * time.Millisecond)
	if _, ok5, _ := locker.Acquire(ctx, "expired", time.Minute); !ok5 {
		t.Fatal("lock kedaluwarsa harus bisa diambil alih")
	}
}

// TestDBLocker_TwoInstancesRunJobOnce — DoD PR-3.5.2: dua instance berbagi DB (store+lock),
// jalan konkuren; job hanya dieksekusi SEKALI.
func TestDBLocker_TwoInstancesRunJobOnce(t *testing.T) {
	store, locker, ctx := newLockEnv(t)

	var runs int32
	newReg := func() *coreSched.Registry {
		r := coreSched.NewRegistry()
		_ = r.Register("once", func(context.Context, []byte) error {
			atomic.AddInt32(&runs, 1)
			return nil
		})
		return r
	}
	now := time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }

	inst1 := coreSched.NewRunner(newReg(), store, time.Minute).WithClock(clock).WithLocker(locker, time.Minute)
	inst2 := coreSched.NewRunner(newReg(), store, time.Minute).WithClock(clock).WithLocker(locker, time.Minute)

	if _, err := inst1.Schedule(ctx, coreSched.ScheduledJob{
		TenantID: "t1", Name: "sekali", JobKey: "once",
		CronExpr: "", NextRunAt: now, Enabled: true,
	}); err != nil {
		t.Fatalf("Schedule: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _, _ = inst1.RunDue(ctx) }()
	go func() { defer wg.Done(); _, _ = inst2.RunDue(ctx) }()
	wg.Wait()

	if got := atomic.LoadInt32(&runs); got != 1 {
		t.Fatalf("job harus jalan tepat sekali di dua instance, jalan %d kali", got)
	}
}
