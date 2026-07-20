-- Menyimpan pilihan template workflow per-tenant + parameter binding peran.
-- Satu baris per (tenant_id, slot) — itu PK natural, UPSERT pada Set.
-- Tidak ada FK ke gov.workflow_definitions karena template bisa selesai dibuat
-- setelah config ditetapkan (dan FK lintas-skema dilarang di monorepo ini).
--
-- Scope saat ini = tenant (flat). Saat PR-3.3.2 (tenant config ber-scope) hadir,
-- pilihan template direkonsiliasi ke resolver scoped tenant[/unit/resource] — lihat
-- ROADMAP backlog "[PR-3.3.2] Rekonsiliasi penyimpanan template selection".
CREATE SCHEMA IF NOT EXISTS gov;

CREATE TABLE IF NOT EXISTS gov.tenant_workflow_configs (
    tenant_id     TEXT        NOT NULL,
    slot          TEXT        NOT NULL,   -- tipe workflow, mis. "surat_masuk.disposisi"
    template_id   TEXT        NOT NULL,   -- WorkflowDefinition.ID yang dipilih
    role_bindings JSONB       NOT NULL DEFAULT '{}',  -- peran generik → role konkret tenant
    set_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    set_by        UUID,                   -- NULL = ditetapkan oleh seed/framework
    PRIMARY KEY (tenant_id, slot)
);
