-- Lock terdistribusi ber-sewa untuk scheduler (PR-3.5.2): satu job tidak jalan ganda di
-- multi-instance. Satu baris per key; locked_until adalah batas sewa (lease). Baris yang
-- locked_until < now dianggap bebas dan boleh diambil alih — mencegah deadlock permanen
-- bila instance pemegang mati. token menjaga agar hanya pemegang saat ini yang boleh release.
CREATE SCHEMA IF NOT EXISTS gov;

CREATE TABLE IF NOT EXISTS gov.job_locks (
    lock_key     TEXT PRIMARY KEY,
    token        TEXT        NOT NULL, -- pemegang unik; guard release
    locked_until TIMESTAMPTZ NOT NULL  -- batas sewa; < now = bebas/diambil alih
);
