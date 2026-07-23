-- Menjadikan pilihan template workflow ber-versi + effective date (PR-3.3.2b), menutup utang
-- PR-3.2.4 butir (a): pilihan lama bisa dibaca & di-rollback. Sebelumnya PK (tenant_id, slot)
-- membuat Set = UPSERT (pilihan lama hilang). Kini append-only: version = max+1 per (tenant, slot).
--
-- ALTER idempoten (IF NOT EXISTS / IF EXISTS) agar aman di atas skema 3.2.4.
ALTER TABLE gov.tenant_workflow_configs
    ADD COLUMN IF NOT EXISTS version INT NOT NULL DEFAULT 1;
ALTER TABLE gov.tenant_workflow_configs
    ADD COLUMN IF NOT EXISTS effective_from TIMESTAMPTZ NOT NULL DEFAULT now();

-- Baris 3.2.4 yang ada jadi versi 1, effective sejak set_at-nya.
UPDATE gov.tenant_workflow_configs SET effective_from = set_at WHERE effective_from IS NULL;

-- PK lama (satu baris per tenant+slot) diganti keunikan per-versi.
ALTER TABLE gov.tenant_workflow_configs DROP CONSTRAINT IF EXISTS tenant_workflow_configs_pkey;
ALTER TABLE gov.tenant_workflow_configs ADD CONSTRAINT uq_twc_version
    UNIQUE (tenant_id, slot, version);
CREATE INDEX IF NOT EXISTS idx_twc_lookup
    ON gov.tenant_workflow_configs (tenant_id, slot);
