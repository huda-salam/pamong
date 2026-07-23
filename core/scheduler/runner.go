package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

// Runner mengeksekusi job terjadwal: memilih jadwal yang jatuh tempo, menjalankan handler
// ter-registry, mencatat riwayat, lalu menghitung ulang NextRunAt. Single-instance untuk
// PR-3.5.1 — proteksi double-run di multi-instance ditambahkan di PR-3.5.2 (lock terdistribusi).
//
// Runner TIDAK berisi logika job apa pun — ia hanya orkestrator. Logika ada di handler yang
// didaftarkan ke Registry (analog: workflow mengorkestrasi use case, tak berisi business logic).
type Runner struct {
	registry *Registry
	store    JobStore
	locker   Locker // opsional; nil = single-instance (tanpa proteksi double-run)
	lockTTL  time.Duration
	now      func() time.Time
	interval time.Duration
}

// NewRunner membuat Runner. interval adalah periode polling saat Start dipakai (default 30s
// bila <= 0). Ketelitian penjadwalan berorde interval ini (PRD: orde detik).
func NewRunner(registry *Registry, store JobStore, interval time.Duration) *Runner {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	return &Runner{
		registry: registry,
		store:    store,
		lockTTL:  5 * time.Minute,
		now:      time.Now,
		interval: interval,
	}
}

// WithClock mengganti sumber waktu (untuk test deterministik). Kembalikan runner agar bisa di-chain.
func (r *Runner) WithClock(now func() time.Time) *Runner {
	r.now = now
	return r
}

// WithLocker memasang lock terdistribusi agar satu job tidak jalan ganda di multi-instance
// (PR-3.5.2). ttl adalah masa sewa lock — pilih lebih lama dari durasi terpanjang yang wajar
// untuk job, agar lock tak kedaluwarsa saat job masih berjalan. ttl <= 0 memakai default 5m.
// Tanpa WithLocker, Runner berperilaku single-instance seperti PR-3.5.1.
func (r *Runner) WithLocker(locker Locker, ttl time.Duration) *Runner {
	r.locker = locker
	if ttl > 0 {
		r.lockTTL = ttl
	}
	return r
}

// Schedule mendaftarkan (atau memperbarui) satu jadwal. Memvalidasi cron & keberadaan JobKey
// di registry SAAT TULIS agar jadwal rusak ketahuan di pintu masuk, bukan saat runtime.
// NextRunAt dihitung dari cron bila belum diset; untuk one-shot (cron kosong) NextRunAt wajib diisi.
func (r *Runner) Schedule(ctx context.Context, job ScheduledJob) (ScheduledJob, error) {
	if _, err := r.registry.Get(job.JobKey); err != nil {
		return ScheduledJob{}, err
	}
	if job.ID == uuid.Nil {
		job.ID = uuid.New()
	}
	if job.CreatedAt.IsZero() {
		job.CreatedAt = r.now()
	}
	if job.CronExpr != "" {
		sched, err := ParseCron(job.CronExpr)
		if err != nil {
			return ScheduledJob{}, err
		}
		if job.NextRunAt.IsZero() {
			job.NextRunAt = sched.Next(r.now())
		}
	} else if job.NextRunAt.IsZero() {
		return ScheduledJob{}, ErrInvalidCron("", "one-shot job (cron kosong) wajib mengisi NextRunAt")
	}
	if err := r.store.SaveSchedule(ctx, job); err != nil {
		return ScheduledJob{}, err
	}
	return job, nil
}

// RunDue menjalankan semua jadwal yang jatuh tempo pada saat now(). Dipakai langsung oleh
// test (tanpa ticker) maupun oleh Start tiap tick. Mengembalikan jumlah job yang dieksekusi.
// Kegagalan satu job dicatat sebagai riwayat gagal dan tidak menghentikan job lain.
func (r *Runner) RunDue(ctx context.Context) (int, error) {
	now := r.now()
	due, err := r.store.DueSchedules(ctx, now)
	if err != nil {
		return 0, fmt.Errorf("ambil jadwal jatuh tempo: %w", err)
	}
	count := 0
	for _, job := range due {
		if !r.runLocked(ctx, job) {
			continue // job dipegang instance lain — bukan eksekusi kita
		}
		count++
	}
	return count, nil
}

// runLocked menjalankan satu job di bawah lock terdistribusi (bila terpasang). Mengembalikan
// true bila job dieksekusi oleh instance ini, false bila dilewati karena instance lain memegang
// lock. Tanpa locker, selalu eksekusi (single-instance).
//
// Lock dilepas setelah execute+advance. Re-check jatuh tempo setelah lock diambil menutup
// balapan "dua instance sama-sama membaca due lalu satu sudah advance": instance yang kalah
// lock skip; instance yang menang lock tapi menemukan job sudah di-advance (next_run maju)
// juga skip — mencegah eksekusi ganda meski TTL lock kedaluwarsa antar tick.
func (r *Runner) runLocked(ctx context.Context, job ScheduledJob) bool {
	if r.locker == nil {
		r.runOnce(ctx, job)
		return true
	}
	lock, ok, err := r.locker.Acquire(ctx, jobLockKey(job.ID), r.lockTTL)
	if err != nil {
		slog.Error("gagal mengambil lock job", "job", job.JobKey, "id", job.ID, "err", err)
		return false
	}
	if !ok {
		return false
	}
	defer func() { _ = r.locker.Release(ctx, lock) }()

	// Re-check di bawah lock: pastikan job masih jatuh tempo (belum di-advance instance lain).
	fresh, err := r.store.GetSchedule(ctx, job.ID)
	if err != nil {
		slog.Error("gagal membaca ulang jadwal di bawah lock", "id", job.ID, "err", err)
		return false
	}
	if !fresh.Enabled || fresh.NextRunAt.After(r.now()) {
		return false // sudah dijalankan & di-advance instance lain
	}
	r.runOnce(ctx, fresh)
	return true
}

// runOnce mengeksekusi handler satu job lalu memajukan jadwalnya (dipakai dengan/atau tanpa lock).
func (r *Runner) runOnce(ctx context.Context, job ScheduledJob) {
	r.execute(ctx, job)
	if err := r.advance(ctx, job); err != nil {
		slog.Error("gagal memperbarui jadwal setelah eksekusi", "job", job.JobKey, "id", job.ID, "err", err)
	}
}

// execute menjalankan handler satu job dan mencatat riwayatnya (success/failed).
// Payload di-snapshot ke JobRun agar Replay tetap punya konteks yang sama.
func (r *Runner) execute(ctx context.Context, job ScheduledJob) {
	scheduleID := job.ID
	run := JobRun{
		ID:         uuid.New(),
		ScheduleID: &scheduleID,
		TenantID:   job.TenantID,
		JobKey:     job.JobKey,
		Payload:    job.Payload,
		StartedAt:  r.now(),
		Attempt:    1,
	}
	r.invoke(ctx, &run)
	if err := r.store.RecordRun(ctx, run); err != nil {
		slog.Error("gagal mencatat riwayat job", "job", job.JobKey, "id", job.ID, "err", err)
	}
}

// invoke memanggil handler dan mengisi status/error/FinishedAt pada run. Panic handler
// ditangkap menjadi kegagalan agar satu job buruk tak menjatuhkan runner.
func (r *Runner) invoke(ctx context.Context, run *JobRun) {
	defer func() {
		if rec := recover(); rec != nil {
			run.Status = StatusFailed
			run.Error = fmt.Sprintf("panic: %v", rec)
			run.FinishedAt = r.now()
		}
	}()
	fn, err := r.registry.Get(run.JobKey)
	if err != nil {
		run.Status = StatusFailed
		run.Error = err.Error()
		run.FinishedAt = r.now()
		return
	}
	err = fn(ctx, run.Payload)
	run.FinishedAt = r.now()
	if err != nil {
		run.Status = StatusFailed
		run.Error = err.Error()
		return
	}
	run.Status = StatusSuccess
}

// advance menghitung NextRunAt berikutnya. Untuk one-shot: nonaktifkan setelah jalan.
// Untuk berulang: NextRunAt = cron.Next(now). Bila cron tak menghasilkan waktu (mustahil
// untuk ekspresi valid), jadwal dinonaktifkan agar tidak berputar.
func (r *Runner) advance(ctx context.Context, job ScheduledJob) error {
	last := r.now()
	if job.IsOneShot() {
		return r.store.UpdateAfterRun(ctx, job.ID, last, job.NextRunAt, false)
	}
	sched, err := ParseCron(job.CronExpr)
	if err != nil {
		// Ekspresi sudah divalidasi saat Schedule; bila entah bagaimana rusak, hentikan job.
		return r.store.UpdateAfterRun(ctx, job.ID, last, job.NextRunAt, false)
	}
	next := sched.Next(last)
	enabled := !next.IsZero()
	return r.store.UpdateAfterRun(ctx, job.ID, last, next, enabled)
}

// Trigger menjalankan job ad-hoc sekali di luar jadwal (mis. tombol "jalankan sekarang" admin,
// atau enqueue deadline oleh workflow). Mencatat riwayat tanpa ScheduleID. Mengembalikan run.
func (r *Runner) Trigger(ctx context.Context, tenantID, jobKey string, payload []byte) (JobRun, error) {
	if _, err := r.registry.Get(jobKey); err != nil {
		return JobRun{}, err
	}
	run := JobRun{
		ID:        uuid.New(),
		TenantID:  tenantID,
		JobKey:    jobKey,
		Payload:   payload,
		StartedAt: r.now(),
		Attempt:   1,
	}
	r.invoke(ctx, &run)
	if err := r.store.RecordRun(ctx, run); err != nil {
		return JobRun{}, err
	}
	return run, nil
}

// Replay menjalankan ulang satu run yang tercatat dengan konteks (JobKey + Payload) yang sama
// (PRD F4). Attempt dinaikkan dari run asal. Aman hanya bila job idempoten (tanggung jawab job).
func (r *Runner) Replay(ctx context.Context, runID uuid.UUID) (JobRun, error) {
	orig, err := r.store.GetRun(ctx, runID)
	if err != nil {
		return JobRun{}, err
	}
	replay := JobRun{
		ID:         uuid.New(),
		ScheduleID: orig.ScheduleID,
		TenantID:   orig.TenantID,
		JobKey:     orig.JobKey,
		Payload:    orig.Payload,
		StartedAt:  r.now(),
		Attempt:    orig.Attempt + 1,
	}
	r.invoke(ctx, &replay)
	if err := r.store.RecordRun(ctx, replay); err != nil {
		return JobRun{}, err
	}
	return replay, nil
}

// Start menjalankan loop polling di goroutine sampai ctx dibatalkan. Non-blocking (pola OutboxRelay).
func (r *Runner) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(r.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := r.RunDue(ctx); err != nil {
					slog.Error("siklus scheduler gagal", "err", err)
				}
			}
		}
	}()
}
