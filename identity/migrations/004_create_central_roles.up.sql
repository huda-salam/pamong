-- Role sentral (global & scoped) di identity DB sentral (schema id). PR-2.3.2.
-- Dikelola admin platform; berlaku lintas tenant. Additive / backward-compatible.
--
-- Tiga tabel:
--   id.central_roles             — definisi role: nama, label, scope (global|scoped).
--   id.central_role_permissions  — grant role->permission (join table; bentuk RBAC kanonik,
--                                  pola yang dipakai ulang tenant role 2.3.3 & manifest 2.3.4).
--   id.central_role_assignments  — assignment role ke person + tenant_scope[] (scoped).
--
-- Definisi permission TIDAK disimpan di sini — sumbernya manifest modul (kode). Tabel ini
-- hanya menyimpan grant (string permission apa adanya); validasi terhadap registry manifest
-- menyusul PR-2.3.4.

CREATE TABLE id.central_roles (
    id          UUID PRIMARY KEY,
    name        VARCHAR(100) UNIQUE NOT NULL,
    label       VARCHAR(255) NOT NULL,
    scope_type  VARCHAR(10) NOT NULL CHECK (scope_type IN ('global','scoped')),
    description TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE id.central_role_permissions (
    role_id    UUID NOT NULL REFERENCES id.central_roles(id) ON DELETE CASCADE,
    permission VARCHAR(150) NOT NULL,
    PRIMARY KEY (role_id, permission)
);

-- tenant_scope: daftar tenant tempat assignment scoped berlaku. OTORITAS global vs scoped
-- adalah central_roles.scope_type (BUKAN kekosongan kolom ini): role scoped dgn tenant_scope
-- kosong → tak berlaku di mana pun (resolver fail-closed). SENGAJA tanpa FK ke
-- id.tenant_registry: agar token region (mis. 'prov:jatim') bisa masuk kelak tanpa ubah
-- skema (wildcard provinsi, ditunda).
CREATE TABLE id.central_role_assignments (
    id           UUID PRIMARY KEY,
    person_id    UUID NOT NULL REFERENCES id.persons(id),
    role_id      UUID NOT NULL REFERENCES id.central_roles(id),
    tenant_scope VARCHAR(100)[],
    assigned_by  UUID NOT NULL REFERENCES id.persons(id),
    valid_from   TIMESTAMPTZ NOT NULL DEFAULT now(),
    valid_until  TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_central_role_assignments_person ON id.central_role_assignments (person_id);
CREATE INDEX idx_central_role_assignments_role ON id.central_role_assignments (role_id);
