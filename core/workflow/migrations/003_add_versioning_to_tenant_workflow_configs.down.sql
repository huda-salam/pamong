-- Kembalikan ke skema non-versi 3.2.4. Buang semua versi selain terbaru per (tenant, slot)
-- agar PK lama bisa dipasang kembali.
DELETE FROM gov.tenant_workflow_configs t
USING gov.tenant_workflow_configs t2
WHERE t.tenant_id = t2.tenant_id
  AND t.slot = t2.slot
  AND t.version < t2.version;

ALTER TABLE gov.tenant_workflow_configs DROP CONSTRAINT IF EXISTS uq_twc_version;
DROP INDEX IF EXISTS gov.idx_twc_lookup;
ALTER TABLE gov.tenant_workflow_configs ADD CONSTRAINT tenant_workflow_configs_pkey
    PRIMARY KEY (tenant_id, slot);

ALTER TABLE gov.tenant_workflow_configs DROP COLUMN IF EXISTS effective_from;
ALTER TABLE gov.tenant_workflow_configs DROP COLUMN IF EXISTS version;
