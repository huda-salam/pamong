-- Tenant config ber-scope bertingkat: tenant → unit kerja → resource (PR-3.3.2).
-- Menyimpan nilai config per-tenant yang di-resolve dengan aturan "paling spesifik menang"
-- oleh core/config.Resolver. Value adalah string; maknanya milik pemakai (mis. core/strategy
-- menyimpan strategy key di sini).
--
-- Scope dinyatakan lewat kolom nullable unit_kerja_id/resource_id:
--   (NULL, NULL)   → berlaku untuk seluruh tenant
--   (unit, NULL)   → berlaku untuk satu unit kerja (override level tenant)
--   (unit, res)    → berlaku untuk satu resource (override level unit)
-- Skema ini sengaja kaya sejak awal (titik ekstensi #2) agar scope bisa diperdalam tanpa
-- migrasi; saat ini hampir semua pemakaian hanya mengisi tenant_id.
CREATE SCHEMA IF NOT EXISTS gov;

CREATE TABLE IF NOT EXISTS gov.tenant_configs (
    tenant_id     TEXT        NOT NULL,
    unit_kerja_id UUID,                 -- NULL = level tenant
    resource_id   UUID,                 -- NULL = level unit/tenant
    config_key    TEXT        NOT NULL, -- mis. "keuangan.persediaan" (= decision point strategy)
    value         TEXT        NOT NULL, -- string; pemakai yang menafsirkan
    set_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    set_by        UUID,                 -- NULL = ditetapkan oleh seed/framework

    -- resource ber-nested di bawah unit kerja: tak boleh ada resource tanpa unit.
    CONSTRAINT ck_tenant_config_scope
        CHECK (resource_id IS NULL OR unit_kerja_id IS NOT NULL),

    -- Satu nilai per (tenant, key, scope). NULLS NOT DISTINCT (Postgres 15+) agar dua baris
    -- ber-scope tenant-level untuk key yang sama dianggap konflik (NULL diperlakukan sama).
    CONSTRAINT uq_tenant_config_scope
        UNIQUE NULLS NOT DISTINCT (tenant_id, config_key, unit_kerja_id, resource_id)
);

-- Resolver mengambil semua kandidat untuk (tenant, key) lalu memilih paling spesifik.
CREATE INDEX IF NOT EXISTS idx_tenant_config_lookup
    ON gov.tenant_configs (tenant_id, config_key);
