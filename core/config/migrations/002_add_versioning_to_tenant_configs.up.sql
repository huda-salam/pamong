-- Menjadikan tenant config ber-versi + effective date (PR-3.3.3). Pilihan config kini
-- append-only: tiap perubahan menambah versi baru dengan effective_from, pilihan lama tetap
-- terbaca untuk tanggal lama (non-retroaktif, titik ekstensi #7). Menggantikan keunikan
-- 3.3.2 yang hanya satu baris per scope.
--
-- ALTER idempoten (IF NOT EXISTS / IF EXISTS) agar aman dijalankan di atas skema 3.3.2.
ALTER TABLE gov.tenant_configs
    ADD COLUMN IF NOT EXISTS version INT NOT NULL DEFAULT 1;
ALTER TABLE gov.tenant_configs
    ADD COLUMN IF NOT EXISTS effective_from TIMESTAMPTZ NOT NULL DEFAULT now();

-- Backfill: baris 3.3.2 yang ada jadi versi 1, effective sejak set_at-nya.
UPDATE gov.tenant_configs SET effective_from = set_at WHERE effective_from IS NULL;

-- Keunikan lama (satu baris per scope) diganti keunikan per-versi.
ALTER TABLE gov.tenant_configs DROP CONSTRAINT IF EXISTS uq_tenant_config_scope;
ALTER TABLE gov.tenant_configs ADD CONSTRAINT uq_tenant_config_version
    UNIQUE NULLS NOT DISTINCT (tenant_id, config_key, unit_kerja_id, resource_id, version);
