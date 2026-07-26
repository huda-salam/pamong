-- Custom field per-tenant (PR-3.4.1, PRD F1). Layer kustomisasi TERPISAH dari definisi modul
-- inti: tenant menambah field tanpa mengubah kode modul, dan upgrade modul tak menimpanya (F4).
-- Di-merge dengan EntityDef inti saat runtime oleh core/customization.MergeEntity.
--
-- Kolom pencarian (tenant_id, module, entity, field_name) EKSPLISIT & ter-index; hanya bentuk
-- field yang memang varian per-tipe (Options, LinkTo, Precision, dst) disimpan sebagai JSONB
-- field_def — bukan seluruh baris jadi blob polimorfik (CODING_PHILOSOPHY #3).
CREATE SCHEMA IF NOT EXISTS gov;

CREATE TABLE IF NOT EXISTS gov.tenant_custom_fields (
    tenant_id    TEXT        NOT NULL,
    module       TEXT        NOT NULL,
    entity       TEXT        NOT NULL, -- nama EntityDef inti yang di-extend
    field_name   TEXT        NOT NULL,
    field_def    JSONB       NOT NULL, -- domain.FieldDef ter-serialisasi (tipe, required, dst)
    data_class   TEXT        NOT NULL DEFAULT 'internal', -- klasifikasi data; default aman
    insert_after TEXT        NOT NULL DEFAULT '',          -- urutan tampil; '' = akhir
    is_active    BOOLEAN     NOT NULL DEFAULT true,        -- soft-deactivate (data lama tetap)
    created_by   UUID        NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Satu definisi per (tenant, module, entity, field): Save bersifat upsert-by-name.
    CONSTRAINT pk_tenant_custom_field
        PRIMARY KEY (tenant_id, module, entity, field_name)
);

-- List mengambil field aktif untuk (tenant, module, entity).
CREATE INDEX IF NOT EXISTS idx_tenant_custom_field_lookup
    ON gov.tenant_custom_fields (tenant_id, module, entity)
    WHERE is_active;
