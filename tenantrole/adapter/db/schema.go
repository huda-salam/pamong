package db

import (
	"context"

	"github.com/huda-salam/pamong/infra/db"
)

// tenantRoleDDL membuat schema gov + tabel role tenant bila belum ada. EnsureSchema-on-write
// (precedent identity.TenantDBWriter untuk gov.user_profiles & AuditStore untuk gov.audit_logs):
// tabel framework gov.* belum punya runner migrasi formal — itu DEFERRED ke runner migrasi
// framework-gov (lihat ROADMAP). Idempoten via IF NOT EXISTS.
//
// gov.user_role_assignments SENGAJA tanpa FK ke gov.user_profiles di jalur ensure ini: kedua
// tabel sama-sama ensure-on-write tanpa jaminan urutan pembuatan, jadi FK referensial ditunda
// ke migrasi framework-gov formal (CLAUDE.md mendefinisikannya REFERENCES gov.user_profiles).
// Resolver tidak butuh JOIN ke user_profiles — cukup user_role_assignments + tenant_roles
// (satu schema gov), jadi integritas baca tetap terjaga.
const tenantRoleDDL = `
CREATE SCHEMA IF NOT EXISTS gov;
CREATE TABLE IF NOT EXISTS gov.tenant_roles (
    id          UUID PRIMARY KEY,
    name        VARCHAR(100) UNIQUE NOT NULL,
    label       VARCHAR(255) NOT NULL,
    description TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS gov.tenant_role_permissions (
    role_id    UUID NOT NULL REFERENCES gov.tenant_roles(id) ON DELETE CASCADE,
    permission VARCHAR(150) NOT NULL,
    PRIMARY KEY (role_id, permission)
);
CREATE TABLE IF NOT EXISTS gov.user_role_assignments (
    id              UUID PRIMARY KEY,
    user_id         UUID NOT NULL,
    role_id         UUID NOT NULL REFERENCES gov.tenant_roles(id),
    unit_kerja_id   UUID,
    include_subtree BOOLEAN NOT NULL DEFAULT false,
    assigned_by     UUID NOT NULL,
    valid_from      TIMESTAMPTZ NOT NULL DEFAULT now(),
    valid_until     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_user_role_assignments_user ON gov.user_role_assignments (user_id);
CREATE INDEX IF NOT EXISTS idx_user_role_assignments_role ON gov.user_role_assignments (role_id);`

// ensureTenantRoleSchema memastikan schema gov + tabel role tenant ada. Dipanggil di awal
// setiap operasi baca/tulis (jalur ensure-on-write), karena tenant DB yang baru di-provision
// belum tentu memuat tabel ini.
func ensureTenantRoleSchema(ctx context.Context, exec db.Conn) error {
	_, err := exec.Exec(ctx, tenantRoleDDL)
	return err
}
