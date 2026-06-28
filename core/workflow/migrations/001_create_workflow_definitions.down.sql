-- Rollback PR-3.2.3: hapus tabel dan indeks workflow_definitions.
DROP INDEX IF EXISTS gov.idx_wfdef_lookup;
DROP TABLE IF EXISTS gov.workflow_definitions;
