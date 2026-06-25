package usecase

import (
	"context"

	"github.com/huda-salam/pamong/identity/domain"
)

// Resolver me-resolve person dari berbagai identifier. Sengaja TANPA permission check:
// ia adalah building block yang dipakai baik oleh alur login (sebelum actor punya
// permission, chicken-and-egg) maupun oleh use case admin yang sudah mengecek izin di
// lapisannya sendiri. Karena itu menerima context.Context biasa, bukan AuthContext.
type Resolver struct {
	persons     domain.PersonRepository
	employments domain.EmploymentRepository
}

func NewResolver(p domain.PersonRepository, e domain.EmploymentRepository) *Resolver {
	return &Resolver{persons: p, employments: e}
}

// ByNIK me-resolve person langsung dari anchor NIK.
func (r *Resolver) ByNIK(ctx context.Context, nik string) (*domain.Person, error) {
	return r.persons.FindByNIK(ctx, nik)
}

// ByNIP me-resolve person lewat employment (NIP → employment → person).
func (r *Resolver) ByNIP(ctx context.Context, nip string) (*domain.Person, error) {
	emp, err := r.employments.FindByNIP(ctx, nip)
	if err != nil {
		return nil, err
	}
	return r.persons.FindByID(ctx, emp.PersonID)
}
