package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Port persistensi identity — didefinisikan di domain, diimplementasi di adapter/db
// terhadap identity DB (gov_identity, schema id). Domain tidak tahu Postgres.

// PersonRepository menyimpan & me-resolve person (anchor NIK).
type PersonRepository interface {
	Save(ctx context.Context, p *Person) error
	FindByID(ctx context.Context, id uuid.UUID) (*Person, error)
	FindByNIK(ctx context.Context, nik string) (*Person, error)
}

// EmploymentRepository menyimpan & me-resolve employment (NIP untuk ASN).
type EmploymentRepository interface {
	Save(ctx context.Context, e *Employment) error
	FindByID(ctx context.Context, id uuid.UUID) (*Employment, error)
	FindByNIP(ctx context.Context, nip string) (*Employment, error)
	ListByPerson(ctx context.Context, personID uuid.UUID) ([]*Employment, error)
}

// CredentialRepository menyimpan & me-resolve credential login.
type CredentialRepository interface {
	Save(ctx context.Context, c *Credential) error
	FindByTypeValue(ctx context.Context, t CredType, value string) (*Credential, error)
	ListByPerson(ctx context.Context, personID uuid.UUID) ([]*Credential, error)
}

// TenantAssignmentRepository menyimpan & me-resolve penugasan employment ke tenant
// (id.tenant_assignments, sentral).
type TenantAssignmentRepository interface {
	Save(ctx context.Context, a *TenantAssignment) error
	ListByEmployment(ctx context.Context, employmentID uuid.UUID) ([]*TenantAssignment, error)
}

// CentralRoleRepository menyimpan & me-resolve role sentral + grant permission-nya
// (id.central_roles + id.central_role_permissions, sentral). Save bersifat atomik:
// role beserta seluruh permission-nya ditulis dalam satu transaksi.
type CentralRoleRepository interface {
	Save(ctx context.Context, r *CentralRole) error
	FindByID(ctx context.Context, id uuid.UUID) (*CentralRole, error)
	FindByName(ctx context.Context, name string) (*CentralRole, error)
	List(ctx context.Context) ([]*CentralRole, error)
}

// CentralRoleAssignmentRepository menyimpan & me-resolve assignment role sentral ke person
// (id.central_role_assignments, sentral).
type CentralRoleAssignmentRepository interface {
	Save(ctx context.Context, a *CentralRoleAssignment) error
	ListByPerson(ctx context.Context, personID uuid.UUID) ([]*CentralRoleAssignment, error)
}

// TenantRegistry menyimpan & me-resolve registry tenant (id.tenant_registry, sentral).
type TenantRegistry interface {
	Save(ctx context.Context, t *Tenant) error
	FindByID(ctx context.Context, tenantID string) (*Tenant, error)
	List(ctx context.Context) ([]*Tenant, error)
	SetActive(ctx context.Context, tenantID string, active bool) error
}

// RevokedTokenStore menyimpan & mengecek daftar token (jti) yang dicabut
// (id.revoked_tokens, sentral). Codec verifikasi (identity/adapter/token) berkonsultasi ke
// IsRevoked SETELAH tanda tangan & klaim token sah. Denylist per-jti: token berumur pendek,
// entri cukup hidup sampai expiresAt lalu boleh dipurge.
type RevokedTokenStore interface {
	// Revoke menandai satu jti dicabut. expiresAt = exp token (batas hidup entri); reason
	// untuk audit/diagnosa. Idempoten: mencabut jti yang sama dua kali tidak error.
	Revoke(ctx context.Context, jti, personID uuid.UUID, expiresAt time.Time, reason string) error
	// IsRevoked true bila jti pernah dicabut.
	IsRevoked(ctx context.Context, jti uuid.UUID) (bool, error)
}
