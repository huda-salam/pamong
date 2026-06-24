-- Migrasi modul surat_masuk. Definisi hidup di kode modul; runner menjalankannya
-- per-tenant (DB-per-tenant). Tracking: gov.migration_history + id.tenant_migrations.
-- Backward-compatible: hanya CREATE (additive). Breaking change butuh dua rilis.

CREATE SCHEMA IF NOT EXISTS surat_masuk;

CREATE TABLE surat_masuk.surat_masuks (
    -- Kolom standar framework: id (app-generated), version (optimistic lock), timestamps.
    id             UUID PRIMARY KEY,
    nomor_agenda   VARCHAR(64)  NOT NULL,
    nomor_surat    VARCHAR(128) NOT NULL,
    tanggal_surat  DATE         NOT NULL,
    tanggal_agenda DATE         NOT NULL,
    pengirim       VARCHAR(255) NOT NULL,
    perihal        TEXT         NOT NULL,
    sifat          VARCHAR(16)  NOT NULL,
    status         VARCHAR(32)  NOT NULL,
    version        INT          NOT NULL DEFAULT 1,
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at     TIMESTAMPTZ,
    CONSTRAINT uq_surat_nomor_agenda UNIQUE (nomor_agenda)
);

-- Index untuk field Searchable (pencarian) & filter umum.
CREATE INDEX idx_surat_perihal ON surat_masuk.surat_masuks (perihal);
CREATE INDEX idx_surat_tanggal_agenda ON surat_masuk.surat_masuks (tanggal_agenda);

CREATE TABLE surat_masuk.disposisis (
    id             UUID PRIMARY KEY,
    surat_id       UUID NOT NULL REFERENCES surat_masuk.surat_masuks(id),
    dari_jabatan   VARCHAR(128) NOT NULL,
    kepada_jabatan VARCHAR(128) NOT NULL,
    instruksi      TEXT NOT NULL,
    tanggal        TIMESTAMPTZ NOT NULL,
    version        INT NOT NULL DEFAULT 1,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at     TIMESTAMPTZ
);

CREATE INDEX idx_disposisi_surat ON surat_masuk.disposisis (surat_id);
