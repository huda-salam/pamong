-- Penugasan employment ke tenant (persona employee) di identity DB sentral (schema id).
-- Citizen TIDAK perlu baris ini. is_home_tenant=false = penugasan cross-tenant (PJ/PLT)
-- yang butuh permission khusus di use case. Additive / backward-compatible. PR-2.2.4.

CREATE TABLE id.tenant_assignments (
    id              UUID PRIMARY KEY,
    employment_id   UUID NOT NULL REFERENCES id.employments(id),
    tenant_id       VARCHAR(100) NOT NULL,
    is_home_tenant  BOOLEAN NOT NULL DEFAULT true,
    assigned_by     UUID NOT NULL REFERENCES id.persons(id),
    valid_from      TIMESTAMPTZ NOT NULL DEFAULT now(),
    valid_until     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (employment_id, tenant_id, valid_from)
);
CREATE INDEX idx_tenant_assignments_employment ON id.tenant_assignments (employment_id);
CREATE INDEX idx_tenant_assignments_tenant ON id.tenant_assignments (tenant_id);
