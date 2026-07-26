-- Persistensi capability override per-tenant (PR-3.4.1, carry-over dari PR-3.4.2). Mekanisme
-- gate (registry deklarasi + resolver) sudah lengkap di core/customization/capability.go; tabel
-- ini adalah write-path persisten untuk TenantCapabilityStore, berbagi tabel/permission dengan
-- custom field (alasan penundaan 3.4.2 → 3.4.1).
--
-- Hanya override EKSPLISIT yang tersimpan — ketiadaan baris berarti "pakai DefaultEnabled",
-- bukan "nonaktif" (CapabilityResolver.IsEnabled). Definisi capability tetap di KODE (tak
-- disimpan di DB) — sejalan "tidak ada logika tereksekusi tersimpan di DB".
CREATE SCHEMA IF NOT EXISTS gov;

CREATE TABLE IF NOT EXISTS gov.tenant_capability_overrides (
    tenant_id  TEXT        NOT NULL,
    capability TEXT        NOT NULL, -- {modul}.{fitur}
    enabled    BOOLEAN     NOT NULL,
    set_by     UUID,                 -- NULL = ditetapkan seed/framework
    set_at     TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Satu override per (tenant, capability): Set bersifat upsert.
    CONSTRAINT pk_tenant_capability_override
        PRIMARY KEY (tenant_id, capability)
);
