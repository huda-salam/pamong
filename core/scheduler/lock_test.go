package scheduler_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/huda-salam/pamong/core/scheduler"
)

func TestMemoryLocker_AcquireBlocksSecond(t *testing.T) {
	l := scheduler.NewMemoryLocker()
	ctx := context.Background()

	lock, ok, err := l.Acquire(ctx, "k", time.Minute)
	if err != nil || !ok {
		t.Fatalf("acquire pertama harus sukses: ok=%v err=%v", ok, err)
	}
	if _, ok2, _ := l.Acquire(ctx, "k", time.Minute); ok2 {
		t.Fatal("acquire kedua atas key sama harus gagal")
	}
	// Setelah release, key bisa diambil lagi.
	if err := l.Release(ctx, lock); err != nil {
		t.Fatalf("release: %v", err)
	}
	if _, ok3, _ := l.Acquire(ctx, "k", time.Minute); !ok3 {
		t.Fatal("setelah release, acquire harus sukses")
	}
}

func TestMemoryLocker_ReleaseWrongTokenIgnored(t *testing.T) {
	l := scheduler.NewMemoryLocker()
	ctx := context.Background()
	_, _, _ = l.Acquire(ctx, "k", time.Minute)
	// Token asing tidak boleh melepas lock milik orang lain.
	_ = l.Release(ctx, scheduler.Lock{Key: "k", Token: "asing"})
	if _, ok, _ := l.Acquire(ctx, "k", time.Minute); ok {
		t.Fatal("release token salah tak boleh membebaskan lock")
	}
}

// TestRunDue_LockPreventsDoubleRun — DoD PR-3.5.2: dua Runner (dua "instance") berbagi
// store & locker yang sama, jalan konkuren; job hanya dieksekusi SEKALI.
func TestRunDue_LockPreventsDoubleRun(t *testing.T) {
	var runs int32
	reg := scheduler.NewRegistry()
	_ = reg.Register("once", func(context.Context, []byte) error {
		atomic.AddInt32(&runs, 1)
		return nil
	})

	store := scheduler.NewMemoryJobStore() // dibagi kedua instance (analog satu DB)
	locker := scheduler.NewMemoryLocker()  // dibagi kedua instance (analog satu DBLocker)
	now := time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }

	inst1 := scheduler.NewRunner(reg, store, time.Minute).WithClock(clock).WithLocker(locker, time.Minute)
	inst2 := scheduler.NewRunner(reg, store, time.Minute).WithClock(clock).WithLocker(locker, time.Minute)

	ctx := context.Background()
	if _, err := inst1.Schedule(ctx, scheduler.ScheduledJob{
		JobKey: "once", CronExpr: "", NextRunAt: now, Enabled: true, // one-shot jatuh tempo
	}); err != nil {
		t.Fatalf("Schedule: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _, _ = inst1.RunDue(ctx) }()
	go func() { defer wg.Done(); _, _ = inst2.RunDue(ctx) }()
	wg.Wait()

	if got := atomic.LoadInt32(&runs); got != 1 {
		t.Fatalf("job harus jalan tepat sekali, jalan %d kali", got)
	}
}
