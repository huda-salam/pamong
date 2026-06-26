package domain

import (
	"context"

	"github.com/google/uuid"
)

// Port persistensi role tenant — didefinisikan di domain, diimplementasi di adapter/db
// terhadap TENANT DB (schema gov). Domain tidak tahu Postgres.

// TenantRoleRepository menyimpan & me-resolve role tenant + grant permission-nya
// (gov.tenant_roles + gov.tenant_role_permissions). Save bersifat atomik: role beserta
// seluruh permission-nya ditulis dalam satu transaksi.
type TenantRoleRepository interface {
	Save(ctx context.Context, r *TenantRole) error
	FindByID(ctx context.Context, id uuid.UUID) (*TenantRole, error)
	FindByName(ctx context.Context, name string) (*TenantRole, error)
	List(ctx context.Context) ([]*TenantRole, error)
}

// TenantRoleAssignmentRepository menyimpan & me-resolve assignment role tenant ke user
// (gov.user_role_assignments).
type TenantRoleAssignmentRepository interface {
	Save(ctx context.Context, a *TenantRoleAssignment) error
	ListByUser(ctx context.Context, userID uuid.UUID) ([]*TenantRoleAssignment, error)
}
