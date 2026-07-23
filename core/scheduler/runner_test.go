package scheduler_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/huda-salam/pamong/core/scheduler"
)

// fixedClock mengembalikan waktu yang bisa dimajukan manual — penjadwalan deterministik.
type fixedClock struct{ t time.Time }

func (c *fixedClock) now() time.Time      { return c.t }
func (c *fixedClock) add(d time.Duration) { c.t = c.t.Add(d) }

func newRunner(t *testing.T, reg *scheduler.Registry, store scheduler.JobStore, clk *fixedClock) *scheduler.Runner {
	t.Helper()
	return scheduler.NewRunner(reg, store, time.Minute).WithClock(clk.now)
}

func TestRunDue_RecurringRunsAndReschedules(t *testing.T) {
	clk := &fixedClock{t: time.Date(2026, 7, 23, 9, 59, 0, 0, time.UTC)}
	var calls int32
	reg := scheduler.NewRegistry()
	_ = reg.Register("tick", func(context.Context, []byte) error {
		atomic.AddInt32(&calls, 1)
		return nil
	})
	store := scheduler.NewMemoryJobStore()
	r := newRunner(t, reg, store, clk)

	ctx := context.Background()
	job, err := r.Schedule(ctx, scheduler.ScheduledJob{
		TenantID: "t1", Name: "tiap-menit", JobKey: "tick",
		CronExpr: "* * * * *", Enabled: true,
	})
	if err != nil {
		t.Fatalf("Schedule: %v", err)
	}

	// Belum jatuh tempo (next = 10:00, sekarang 09:59) → tidak ada eksekusi.
	if n, _ := r.RunDue(ctx); n != 0 {
		t.Fatalf("belum waktunya, tapi jalan %d job", n)
	}

	// Maju ke 10:00 → job jalan, terjadwal ulang ke 10:01.
	clk.add(time.Minute)
	if n, _ := r.RunDue(ctx); n != 1 {
		t.Fatalf("want 1 eksekusi, got %d", n)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("handler dipanggil %d kali", got)
	}

	got, _ := store.GetSchedule(ctx, job.ID)
	if !got.Enabled {
		t.Error("job berulang harus tetap aktif")
	}
	want := time.Date(2026, 7, 23, 10, 1, 0, 0, time.UTC)
	if !got.NextRunAt.Equal(want) {
		t.Errorf("NextRunAt: want %v, got %v", want, got.NextRunAt)
	}
	if got.LastRunAt == nil {
		t.Error("LastRunAt harus terisi")
	}
}

func TestRunDue_OneShotDisablesAfterRun(t *testing.T) {
	clk := &fixedClock{t: time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)}
	reg := scheduler.NewRegistry()
	_ = reg.Register("deadline", noop)
	store := scheduler.NewMemoryJobStore()
	r := newRunner(t, reg, store, clk)
	ctx := context.Background()

	deadline := time.Date(2026, 7, 23, 10, 5, 0, 0, time.UTC)
	job, err := r.Schedule(ctx, scheduler.ScheduledJob{
		TenantID: "t1", Name: "sla-eskalasi", JobKey: "deadline",
		CronExpr: "", NextRunAt: deadline, Enabled: true, // one-shot
	})
	if err != nil {
		t.Fatalf("Schedule: %v", err)
	}

	clk.t = deadline
	if n, _ := r.RunDue(ctx); n != 1 {
		t.Fatalf("want 1, got %d", n)
	}
	got, _ := store.GetSchedule(ctx, job.ID)
	if got.Enabled {
		t.Error("one-shot harus nonaktif setelah jalan")
	}
	// Tidak jalan lagi meski dipanggil ulang.
	if n, _ := r.RunDue(ctx); n != 0 {
		t.Errorf("one-shot jalan dua kali: %d", n)
	}
}

func TestSchedule_RejectsUnknownJobKey(t *testing.T) {
	reg := scheduler.NewRegistry()
	store := scheduler.NewMemoryJobStore()
	clk := &fixedClock{t: time.Now()}
	r := newRunner(t, reg, store, clk)
	_, err := r.Schedule(context.Background(), scheduler.ScheduledJob{
		JobKey: "tak.terdaftar", CronExpr: "* * * * *", Enabled: true,
	})
	if err == nil {
		t.Error("JobKey tak terdaftar harus ditolak saat Schedule")
	}
}

func TestSchedule_RejectsInvalidCron(t *testing.T) {
	reg := scheduler.NewRegistry()
	_ = reg.Register("j", noop)
	r := newRunner(t, reg, scheduler.NewMemoryJobStore(), &fixedClock{t: time.Now()})
	if _, err := r.Schedule(context.Background(), scheduler.ScheduledJob{
		JobKey: "j", CronExpr: "bukan cron", Enabled: true,
	}); err == nil {
		t.Error("cron invalid harus ditolak")
	}
}

func TestRunDue_FailedJobRecordedButRunnerContinues(t *testing.T) {
	clk := &fixedClock{t: time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)}
	reg := scheduler.NewRegistry()
	_ = reg.Register("boom", func(context.Context, []byte) error { return errors.New("gagal terhubung") })
	_ = reg.Register("ok", noop)
	store := scheduler.NewMemoryJobStore()
	r := newRunner(t, reg, store, clk)
	ctx := context.Background()

	bad, _ := r.Schedule(ctx, scheduler.ScheduledJob{JobKey: "boom", CronExpr: "", NextRunAt: clk.t, Enabled: true})
	_, _ = r.Schedule(ctx, scheduler.ScheduledJob{JobKey: "ok", CronExpr: "", NextRunAt: clk.t, Enabled: true})

	n, err := r.RunDue(ctx)
	if err != nil {
		t.Fatalf("RunDue: %v", err)
	}
	if n != 2 {
		t.Fatalf("kedua job harus dieksekusi meski satu gagal, got %d", n)
	}
	runs, _ := store.Runs(ctx, bad.ID, 10)
	if len(runs) != 1 || runs[0].Status != scheduler.StatusFailed {
		t.Fatalf("riwayat gagal tidak tercatat: %+v", runs)
	}
	if runs[0].Error == "" {
		t.Error("error message harus tercatat")
	}
}

func TestRunDue_PanicInHandlerBecomesFailure(t *testing.T) {
	clk := &fixedClock{t: time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)}
	reg := scheduler.NewRegistry()
	_ = reg.Register("panik", func(context.Context, []byte) error { panic("nil map") })
	store := scheduler.NewMemoryJobStore()
	r := newRunner(t, reg, store, clk)
	ctx := context.Background()
	job, _ := r.Schedule(ctx, scheduler.ScheduledJob{JobKey: "panik", CronExpr: "", NextRunAt: clk.t, Enabled: true})

	if _, err := r.RunDue(ctx); err != nil {
		t.Fatalf("panic handler tak boleh menjatuhkan RunDue: %v", err)
	}
	runs, _ := store.Runs(ctx, job.ID, 1)
	if len(runs) != 1 || runs[0].Status != scheduler.StatusFailed {
		t.Fatalf("panic harus jadi run gagal: %+v", runs)
	}
}

func TestReplay_ReRunsWithSameContext(t *testing.T) {
	clk := &fixedClock{t: time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)}
	var lastPayload atomic.Value
	fail := int32(1)
	reg := scheduler.NewRegistry()
	_ = reg.Register("import", func(_ context.Context, p []byte) error {
		lastPayload.Store(string(p))
		if atomic.LoadInt32(&fail) == 1 {
			return errors.New("sumber down")
		}
		return nil
	})
	store := scheduler.NewMemoryJobStore()
	r := newRunner(t, reg, store, clk)
	ctx := context.Background()

	job, _ := r.Schedule(ctx, scheduler.ScheduledJob{
		JobKey: "import", CronExpr: "", NextRunAt: clk.t, Enabled: true, Payload: []byte(`{"file":"x.csv"}`),
	})
	_, _ = r.RunDue(ctx)
	runs, _ := store.Runs(ctx, job.ID, 1)
	failedRun := runs[0]
	if failedRun.Status != scheduler.StatusFailed {
		t.Fatalf("run pertama harus gagal")
	}

	// Perbaiki sumber, replay run yang gagal — payload sama, attempt naik.
	atomic.StoreInt32(&fail, 0)
	replay, err := r.Replay(ctx, failedRun.ID)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if replay.Status != scheduler.StatusSuccess {
		t.Errorf("replay harus sukses, got %s (%s)", replay.Status, replay.Error)
	}
	if lastPayload.Load() != `{"file":"x.csv"}` {
		t.Errorf("replay harus pakai payload sama, got %v", lastPayload.Load())
	}
	if replay.Attempt != failedRun.Attempt+1 {
		t.Errorf("attempt: want %d, got %d", failedRun.Attempt+1, replay.Attempt)
	}
}

func TestTrigger_AdHocRunRecorded(t *testing.T) {
	clk := &fixedClock{t: time.Now()}
	reg := scheduler.NewRegistry()
	_ = reg.Register("adhoc", noop)
	store := scheduler.NewMemoryJobStore()
	r := newRunner(t, reg, store, clk)
	run, err := r.Trigger(context.Background(), "t1", "adhoc", []byte("p"))
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if run.Status != scheduler.StatusSuccess {
		t.Errorf("want success, got %s", run.Status)
	}
	if run.ScheduleID != nil {
		t.Error("run ad-hoc tidak boleh punya ScheduleID")
	}
}
