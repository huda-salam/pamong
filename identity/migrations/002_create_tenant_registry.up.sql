-- Registry tenant di identity DB sentral (schema id). Resolver membaca tabel ini untuk
-- menentukan lokasi DB tiap tenant (tier portabilitas). Harus sentral — tidak bisa di
-- tenant DB (chicken-and-egg saat resolve). PR-2.2.1.

CREATE TABLE id.tenant_registry (
    tenant_id         VARCHAR(100) PRIMARY KEY,
    nama              VARCHAR(255) NOT NULL,
    tier              SMALLINT NOT NULL DEFAULT 1 CHECK (tier IN (1,2,3)),
    db_host           VARCHAR(255) NOT NULL,
    db_name           VARCHAR(100) NOT NULL,
    db_schema         VARCHAR(100) NOT NULL DEFAULT '',
    migration_version VARCHAR(50)  NOT NULL DEFAULT '',
    is_active         BOOLEAN NOT NULL DEFAULT true,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_tenant_registry_active ON id.tenant_registry (is_active);
