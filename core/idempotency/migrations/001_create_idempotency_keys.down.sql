-- Membalik 001_create_idempotency_keys.up.sql. Schema gov TIDAK di-drop di sini: ia dipakai
-- bersama tabel framework lain (audit, workflow, config, notification, dst).
DROP TABLE IF EXISTS gov.idempotency_keys;
