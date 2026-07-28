-- Membalik 001_create_sequences.up.sql. Schema gov TIDAK di-drop di sini: ia dipakai
-- bersama tabel framework lain (audit, workflow, config, idempotency, dst).
DROP TABLE IF EXISTS gov.sequences;
