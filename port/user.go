package port

import (
	"context"
	"github.com/google/uuid"
)

type UserProfile struct {
	ID               uuid.UUID
	NIK              string
	NIP              string
	NamaLengkap      string
	EmploymentStatus string
	IsCrossTenant    bool
	JabatanLokal     string
}

type UserResolver interface {
	ResolveByID(ctx context.Context, id uuid.UUID) (*UserProfile, error)
	ResolveByNIP(ctx context.Context, nip string) (*UserProfile, error)
	ResolveByNIK(ctx context.Context, nik string) (*UserProfile, error)
	IsCrossTenant(ctx context.Context, id uuid.UUID) (bool, error)
	HasCentralRole(ctx context.Context, id uuid.UUID, role string) (bool, error)
}
