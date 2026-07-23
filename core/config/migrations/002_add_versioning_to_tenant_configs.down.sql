-- Kembalikan ke skema non-versi 3.3.2. Membuang seluruh versi selain yang terbaru per scope
-- agar keunikan lama (satu baris per scope) bisa dipasang kembali.
DELETE FROM gov.tenant_configs t
USING gov.tenant_configs t2
WHERE t.tenant_id = t2.tenant_id
  AND t.config_key = t2.config_key
  AND t.unit_kerja_id IS NOT DISTINCT FROM t2.unit_kerja_id
  AND t.resource_id   IS NOT DISTINCT FROM t2.resource_id
  AND t.version < t2.version;

ALTER TABLE gov.tenant_configs DROP CONSTRAINT IF EXISTS uq_tenant_config_version;
ALTER TABLE gov.tenant_configs ADD CONSTRAINT uq_tenant_config_scope
    UNIQUE NULLS NOT DISTINCT (tenant_id, config_key, unit_kerja_id, resource_id);

ALTER TABLE gov.tenant_configs DROP COLUMN IF EXISTS effective_from;
ALTER TABLE gov.tenant_configs DROP COLUMN IF EXISTS version;
