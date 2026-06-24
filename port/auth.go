package port

import (
	"context"

	"github.com/google/uuid"
)

// AuthContext membawa identitas actor, tenant, dan context pembatalan.
// Use case menerima AuthContext sebagai pengganti context.Context biasa;
// ia memenuhi context.Context sehingga bisa diteruskan ke port lain (repo, seq, dsb).
type AuthContext interface {
	context.Context

	PersonID() uuid.UUID
	Persona() string
	EmploymentStatus() string
	TenantID() string
	HasRole(role string) bool
	HasCentralRole(role string) bool
	RequirePermission(perm string) error
	IsCitizen() bool
	IsCrossTenant() bool
}
