// Package scheduler menyediakan driven adapter Postgres untuk scheduler.JobStore.
// Seluruh kode yang menyentuh pgx HANYA ada di sini dan di infra/db — core/scheduler tidak
// pernah mengimport infra (linter: domain-no-infra-import).
package scheduler

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/huda-salam/pamong/core"
	coreSched "github.com/huda-salam/pamong/core/scheduler"
	"github.com/huda-salam/pamong/infra/db"
)

// schedulerDDL membuat schema gov & tabel jadwal + riwayat bila belum ada. Idempoten
// (pola AuditRepo/DBStore/outbox). Yang disimpan hanya PILIHAN (job_key + cron + payload),
// bukan logika — handler ada di scheduler.Registry (kode Go).
const schedulerDDL = `
CREATE SCHEMA IF NOT EXISTS gov;
CREATE TABLE IF NOT EXISTS gov.scheduled_jobs (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    TEXT        NOT NULL DEFAULT '',
    name         TEXT        NOT NULL,
    job_key      TEXT        NOT NULL,
    cron_expr    TEXT        NOT NULL DEFAULT '',
    payload      BYTEA,
    enabled      BOOLEAN     NOT NULL DEFAULT true,
    next_run_at  TIMESTAMPTZ NOT NULL,
    last_run_at  TIMESTAMPTZ,
    created_by   UUID,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- Index menutup filter due-scan (hanya baris aktif yang jatuh tempo yang di-scan).
CREATE INDEX IF NOT EXISTS idx_scheduled_jobs_due
    ON gov.scheduled_jobs (next_run_at)
    WHERE enabled;
CREATE TABLE IF NOT EXISTS gov.job_runs (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    schedule_id  UUID REFERENCES gov.scheduled_jobs(id) ON DELETE SET NULL,
    tenant_id    TEXT        NOT NULL DEFAULT '',
    job_key      TEXT        NOT NULL,
    payload      BYTEA,
    status       TEXT        NOT NULL,
    started_at   TIMESTAMPTZ NOT NULL,
    finished_at  TIMESTAMPTZ NOT NULL,
    error        TEXT        NOT NULL DEFAULT '',
    attempt      INT         NOT NULL DEFAULT 1
);
CREATE INDEX IF NOT EXISTS idx_job_runs_schedule
    ON gov.job_runs (schedule_id, started_at DESC);`

// DBJobStore mengimplementasi coreSched.JobStore di atas Postgres.
type DBJobStore struct {
	pool *db.Pool
}

var _ coreSched.JobStore = (*DBJobStore)(nil)

// NewDBJobStore membuat store. Panggil EnsureSchema sebelum dipakai.
func NewDBJobStore(pool *db.Pool) *DBJobStore { return &DBJobStore{pool: pool} }

// EnsureSchema membuat schema & tabel bila belum ada. Idempoten.
func (s *DBJobStore) EnsureSchema(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, schedulerDDL)
	return err
}

// SaveSchedule meng-upsert jadwal by ID.
func (s *DBJobStore) SaveSchedule(ctx context.Context, job coreSched.ScheduledJob) error {
	// gov:raw-ok reason=upsert-schedule query=scheduler-save-schedule
	_, err := s.pool.Exec(ctx, `
		INSERT INTO gov.scheduled_jobs
			(id, tenant_id, name, job_key, cron_expr, payload, enabled, next_run_at, last_run_at, created_by, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (id) DO UPDATE SET
			tenant_id = EXCLUDED.tenant_id,
			name      = EXCLUDED.name,
			job_key   = EXCLUDED.job_key,
			cron_expr = EXCLUDED.cron_expr,
			payload   = EXCLUDED.payload,
			enabled   = EXCLUDED.enabled,
			next_run_at = EXCLUDED.next_run_at,
			last_run_at = EXCLUDED.last_run_at`,
		job.ID, job.TenantID, job.Name, job.JobKey, job.CronExpr, job.Payload,
		job.Enabled, job.NextRunAt, job.LastRunAt, job.CreatedBy, job.CreatedAt)
	if err != nil {
		return fmt.Errorf("simpan scheduled_job: %w", err)
	}
	return nil
}

// GetSchedule mengambil satu jadwal by ID.
func (s *DBJobStore) GetSchedule(ctx context.Context, id uuid.UUID) (coreSched.ScheduledJob, error) {
	// gov:raw-ok reason=get-schedule query=scheduler-get-schedule
	row := s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, name, job_key, cron_expr, payload, enabled, next_run_at, last_run_at, created_by, created_at
		FROM gov.scheduled_jobs WHERE id = $1`, id)
	job, err := scanSchedule(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return coreSched.ScheduledJob{}, core.ErrNotFound("scheduled job", id.String())
	}
	return job, err
}

// DueSchedules mengembalikan jadwal aktif yang jatuh tempo, terurut next_run_at.
func (s *DBJobStore) DueSchedules(ctx context.Context, now time.Time) ([]coreSched.ScheduledJob, error) {
	// gov:raw-ok reason=due-scan query=scheduler-due-schedules
	rows, err := s.pool.Query(ctx, `
		SELECT id, tenant_id, name, job_key, cron_expr, payload, enabled, next_run_at, last_run_at, created_by, created_at
		FROM gov.scheduled_jobs
		WHERE enabled AND next_run_at <= $1
		ORDER BY next_run_at`, now)
	if err != nil {
		return nil, fmt.Errorf("query due schedules: %w", err)
	}
	defer rows.Close()

	var out []coreSched.ScheduledJob
	for rows.Next() {
		job, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, rows.Err()
}

// UpdateAfterRun memperbarui last/next run + enabled setelah eksekusi.
func (s *DBJobStore) UpdateAfterRun(ctx context.Context, id uuid.UUID, lastRun, nextRun time.Time, enabled bool) error {
	// gov:raw-ok reason=advance-schedule query=scheduler-update-after-run
	_, err := s.pool.Exec(ctx, `
		UPDATE gov.scheduled_jobs
		SET last_run_at = $2, next_run_at = $3, enabled = $4
		WHERE id = $1`, id, lastRun, nextRun, enabled)
	if err != nil {
		return fmt.Errorf("update schedule setelah run: %w", err)
	}
	return nil
}

// RecordRun menyimpan satu baris riwayat.
func (s *DBJobStore) RecordRun(ctx context.Context, run coreSched.JobRun) error {
	// gov:raw-ok reason=insert-run query=scheduler-record-run
	_, err := s.pool.Exec(ctx, `
		INSERT INTO gov.job_runs
			(id, schedule_id, tenant_id, job_key, payload, status, started_at, finished_at, error, attempt)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		run.ID, run.ScheduleID, run.TenantID, run.JobKey, run.Payload,
		string(run.Status), run.StartedAt, run.FinishedAt, run.Error, run.Attempt)
	if err != nil {
		return fmt.Errorf("catat job_run: %w", err)
	}
	return nil
}

// GetRun mengambil satu baris riwayat by ID (dipakai Replay).
func (s *DBJobStore) GetRun(ctx context.Context, id uuid.UUID) (coreSched.JobRun, error) {
	// gov:raw-ok reason=get-run query=scheduler-get-run
	row := s.pool.QueryRow(ctx, `
		SELECT id, schedule_id, tenant_id, job_key, payload, status, started_at, finished_at, error, attempt
		FROM gov.job_runs WHERE id = $1`, id)
	run, err := scanRun(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return coreSched.JobRun{}, core.ErrNotFound("job run", id.String())
	}
	return run, err
}

// Runs mengembalikan riwayat satu jadwal, terbaru dulu, dibatasi limit.
func (s *DBJobStore) Runs(ctx context.Context, scheduleID uuid.UUID, limit int) ([]coreSched.JobRun, error) {
	if limit <= 0 {
		limit = 50
	}
	// gov:raw-ok reason=run-history query=scheduler-runs
	rows, err := s.pool.Query(ctx, `
		SELECT id, schedule_id, tenant_id, job_key, payload, status, started_at, finished_at, error, attempt
		FROM gov.job_runs
		WHERE schedule_id = $1
		ORDER BY started_at DESC
		LIMIT $2`, scheduleID, limit)
	if err != nil {
		return nil, fmt.Errorf("query job_runs: %w", err)
	}
	defer rows.Close()

	var out []coreSched.JobRun
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

// scannable menyatukan pgx.Row & baris cursor agar scan helper dipakai keduanya.
type scannable interface {
	Scan(dest ...any) error
}

func scanSchedule(row scannable) (coreSched.ScheduledJob, error) {
	var (
		j         coreSched.ScheduledJob
		lastRun   *time.Time
		createdBy *uuid.UUID
	)
	if err := row.Scan(&j.ID, &j.TenantID, &j.Name, &j.JobKey, &j.CronExpr, &j.Payload,
		&j.Enabled, &j.NextRunAt, &lastRun, &createdBy, &j.CreatedAt); err != nil {
		return coreSched.ScheduledJob{}, err
	}
	j.LastRunAt = lastRun
	j.CreatedBy = createdBy
	return j, nil
}

func scanRun(row scannable) (coreSched.JobRun, error) {
	var (
		r          coreSched.JobRun
		scheduleID *uuid.UUID
		status     string
	)
	if err := row.Scan(&r.ID, &scheduleID, &r.TenantID, &r.JobKey, &r.Payload,
		&status, &r.StartedAt, &r.FinishedAt, &r.Error, &r.Attempt); err != nil {
		return coreSched.JobRun{}, err
	}
	r.ScheduleID = scheduleID
	r.Status = coreSched.RunStatus(status)
	return r, nil
}
