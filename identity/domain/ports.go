package domain

import (
	"context"

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
