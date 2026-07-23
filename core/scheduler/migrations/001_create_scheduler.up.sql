-- Scheduler: jadwal job + riwayat eksekusi (PR-3.5.1).
-- Yang tersimpan hanya PILIHAN — job_key (rujukan handler ter-registry), ekspresi cron,
-- dan payload — bukan logika. Handler adalah kode Go ter-compile di scheduler.Registry.
-- Ini menutup vektor "kode arbitrary di DB" (CLAUDE.md — Fleksibilitas & titik ekstensi).
--
-- cron_expr kosong menandai job SEKALI-JALAN (one-shot): fires di next_run_at lalu enabled
-- menjadi false. Bentuk ini yang dipakai deadline scheduling SLA workflow (tak perlu
-- mekanisme terpisah).
CREATE SCHEMA IF NOT EXISTS gov;

CREATE TABLE IF NOT EXISTS gov.scheduled_jobs (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    TEXT        NOT NULL DEFAULT '', -- kosong = job level-platform
    name         TEXT        NOT NULL,            -- nama deskriptif jadwal
    job_key      TEXT        NOT NULL,            -- key handler di scheduler.Registry
    cron_expr    TEXT        NOT NULL DEFAULT '', -- kosong = one-shot
    payload      BYTEA,                           -- argumen job (opaque)
    enabled      BOOLEAN     NOT NULL DEFAULT true,
    next_run_at  TIMESTAMPTZ NOT NULL,
    last_run_at  TIMESTAMPTZ,
    created_by   UUID,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Index parsial menutup due-scan: hanya baris aktif yang jatuh tempo yang di-scan.
CREATE INDEX IF NOT EXISTS idx_scheduled_jobs_due
    ON gov.scheduled_jobs (next_run_at)
    WHERE enabled;

CREATE TABLE IF NOT EXISTS gov.job_runs (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    schedule_id  UUID REFERENCES gov.scheduled_jobs(id) ON DELETE SET NULL, -- NULL = ad-hoc
    tenant_id    TEXT        NOT NULL DEFAULT '',
    job_key      TEXT        NOT NULL,
    payload      BYTEA,                           -- snapshot untuk replay konteks-sama
    status       TEXT        NOT NULL,            -- 'success' | 'failed'
    started_at   TIMESTAMPTZ NOT NULL,
    finished_at  TIMESTAMPTZ NOT NULL,
    error        TEXT        NOT NULL DEFAULT '',
    attempt      INT         NOT NULL DEFAULT 1
);

CREATE INDEX IF NOT EXISTS idx_job_runs_schedule
    ON gov.job_runs (schedule_id, started_at DESC);
