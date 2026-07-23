-- Rollback PR-3.5.1: buang riwayat lebih dulu (FK ke scheduled_jobs), lalu jadwal.
DROP TABLE IF EXISTS gov.job_runs;
DROP TABLE IF EXISTS gov.scheduled_jobs;
