-- Tabel definisi workflow ber-versi di tenant DB (schema gov).
-- Setiap perubahan definisi menciptakan baris baru (versi baru); baris lama tetap
-- tersimpan sehingga instance yang sedang berjalan tetap mengacu ke versi saat mulai.
-- PR-3.2.3.

CREATE SCHEMA IF NOT EXISTS gov;

CREATE TABLE IF NOT EXISTS gov.workflow_definitions (
    workflow_id      TEXT        NOT NULL,
    version          INT         NOT NULL,
    entity           TEXT        NOT NULL DEFAULT '',
    initial_state    TEXT        NOT NULL,
    authoring_source TEXT        NOT NULL DEFAULT 'developer',
    states           JSONB       NOT NULL,
    transitions      JSONB       NOT NULL,
    effective_from   TIMESTAMPTZ NOT NULL,
    created_by       UUID,                          -- NULL untuk seed developer
    prev_version     INT,                           -- NULL jika versi pertama
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workflow_id, version)
);

-- Lookup utama: ambil versi terbaru per workflow_id.
CREATE INDEX IF NOT EXISTS idx_wfdef_lookup
    ON gov.workflow_definitions (workflow_id, version DESC);
