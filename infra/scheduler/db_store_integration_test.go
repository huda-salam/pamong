//go:build integration

package scheduler_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	coreSched "github.com/huda-salam/pamong/core/scheduler"
	"github.com/huda-salam/pamong/infra/db"
	infraSched "github.com/huda-salam/pamong/infra/scheduler"
)

func newJobStoreEnv(t *testing.T) (*infraSched.DBJobStore, context.Context) {
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

	drop := `DROP TABLE IF EXISTS gov.job_runs; DROP TABLE IF EXISTS gov.scheduled_jobs;`
	_, _ = pool.Exec(ctx, drop)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), drop)
		pgpool.Close()
	})

	store := infraSched.NewDBJobStore(pool)
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	return store, ctx
}

// TestDBJobStore_RunnerExecutesDueJob — DoD PR-3.5.1: job terjadwal jalan tepat waktu,
// dijalankan Runner di atas store Postgres nyata, dengan riwayat tercatat.
func TestDBJobStore_RunnerExecutesDueJob(t *testing.T) {
	store, ctx := newJobStoreEnv(t)

	var ran bool
	reg := coreSched.NewRegistry()
	if err := reg.Register("rekonsiliasi", func(context.Context, []byte) error {
		ran = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	base := time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)
	clk := base
	runner := coreSched.NewRunner(reg, store, time.Minute).WithClock(func() time.Time { return clk })

	job, err := runner.Schedule(ctx, coreSched.ScheduledJob{
		TenantID: "pemkot-surabaya", Name: "rekon-harian", JobKey: "rekonsiliasi",
		CronExpr: "0 * * * *", Enabled: true, // tiap jam bulat
	})
	if err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	// next_run harus 11:00 (jam bulat berikutnya setelah 10:00).
	if got, _ := store.GetSchedule(ctx, job.ID); !got.NextRunAt.Equal(base.Add(time.Hour)) {
		t.Fatalf("NextRunAt awal: want %v, got %v", base.Add(time.Hour), got.NextRunAt)
	}

	// Belum jatuh tempo.
	if n, _ := runner.RunDue(ctx); n != 0 {
		t.Fatalf("belum waktunya: %d", n)
	}
	if ran {
		t.Fatal("job jalan sebelum waktunya")
	}

	// Majukan waktu ke 11:00.
	clk = base.Add(time.Hour)
	n, err := runner.RunDue(ctx)
	if err != nil {
		t.Fatalf("RunDue: %v", err)
	}
	if n != 1 || !ran {
		t.Fatalf("job harus jalan tepat waktu: n=%d ran=%v", n, ran)
	}

	// Riwayat tercatat sukses; jadwal ulang ke 12:00.
	runs, err := store.Runs(ctx, job.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].Status != coreSched.StatusSuccess {
		t.Fatalf("riwayat: %+v", runs)
	}
	got, _ := store.GetSchedule(ctx, job.ID)
	if !got.NextRunAt.Equal(base.Add(2 * time.Hour)) {
		t.Errorf("reschedule: want %v, got %v", base.Add(2*time.Hour), got.NextRunAt)
	}
	if got.LastRunAt == nil {
		t.Error("LastRunAt harus terisi")
	}
}

// TestDBJobStore_ReplayFailedRun — F4: replay job gagal dengan konteks (payload) yang sama.
func TestDBJobStore_ReplayFailedRun(t *testing.T) {
	store, ctx := newJobStoreEnv(t)

	shouldFail := true
	var seenPayload string
	reg := coreSched.NewRegistry()
	_ = reg.Register("import", func(_ context.Context, p []byte) error {
		seenPayload = string(p)
		if shouldFail {
			return context.DeadlineExceeded
		}
		return nil
	})
	clk := time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)
	runner := coreSched.NewRunner(reg, store, time.Minute).WithClock(func() time.Time { return clk })

	run, err := runner.Trigger(ctx, "pemkot-surabaya", "import", []byte(`{"file":"apbd.csv"}`))
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	if run.Status != coreSched.StatusFailed {
		t.Fatalf("run pertama harus gagal, got %s", run.Status)
	}

	shouldFail = false
	replay, err := runner.Replay(ctx, run.ID)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if replay.Status != coreSched.StatusSuccess {
		t.Errorf("replay harus sukses, got %s (%s)", replay.Status, replay.Error)
	}
	if seenPayload != `{"file":"apbd.csv"}` {
		t.Errorf("replay harus pakai payload sama, got %q", seenPayload)
	}
	if replay.Attempt != 2 {
		t.Errorf("attempt: want 2, got %d", replay.Attempt)
	}
}

// TestDBJobStore_OneShotDeadline — one-shot (deadline SLA) fires sekali lalu nonaktif.
func TestDBJobStore_OneShotDeadline(t *testing.T) {
	store, ctx := newJobStoreEnv(t)
	reg := coreSched.NewRegistry()
	_ = reg.Register("eskalasi", func(context.Context, []byte) error { return nil })

	deadline := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	clk := deadline
	runner := coreSched.NewRunner(reg, store, time.Minute).WithClock(func() time.Time { return clk })

	job, err := runner.Schedule(ctx, coreSched.ScheduledJob{
		TenantID: "t1", Name: "sla", JobKey: "eskalasi",
		CronExpr: "", NextRunAt: deadline, Enabled: true,
	})
	if err != nil {
		t.Fatalf("Schedule one-shot: %v", err)
	}
	if n, _ := runner.RunDue(ctx); n != 1 {
		t.Fatalf("one-shot harus jalan: %d", n)
	}
	got, _ := store.GetSchedule(ctx, job.ID)
	if got.Enabled {
		t.Error("one-shot harus nonaktif setelah jalan")
	}
	// Tidak jalan lagi.
	clk = deadline.Add(time.Hour)
	if n, _ := runner.RunDue(ctx); n != 0 {
		t.Errorf("one-shot jalan ulang: %d", n)
	}
}
