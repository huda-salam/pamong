package usecase

import (
	"time"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/port"
)

// AssignEmploymentToTenant menugaskan sebuah employment ke tenant, lalu menerbitkan
// event identity.employment.ditugaskan yang memicu sync engine meng-clone person ke
// gov.user_profiles tenant tujuan (DoD PR-2.2.4).
//
// PR ini menangani penugasan home-tenant. Penugasan cross-tenant (is_home_tenant=false,
// mis. PJ Bupati) tetap diterima di sini namun butuh permission tambahan; orkestrasi
// penuh cross-tenant (pemilihan tenant, PLT) dilengkapi PR-2.4.5.
type AssignEmploymentToTenant struct {
	persons     domain.PersonRepository
	employments domain.EmploymentRepository
	assignments domain.TenantAssignmentRepository
	publisher   port.EventPublisher
}

func NewAssignEmploymentToTenant(
	p domain.PersonRepository,
	e domain.EmploymentRepository,
	a domain.TenantAssignmentRepository,
	pub port.EventPublisher,
) *AssignEmploymentToTenant {
	return &AssignEmploymentToTenant{persons: p, employments: e, assignments: a, publisher: pub}
}

// AssignEmploymentToTenantInput DTO masuk. CrossTenant=false (default) = penugasan ke
// home tenant; CrossTenant=true menandai is_home_tenant=false dan menuntut permission ekstra.
type AssignEmploymentToTenantInput struct {
	EmploymentID uuid.UUID
	TenantID     string
	CrossTenant  bool
	ValidFrom    time.Time
	ValidUntil   *time.Time
}

// Execute: permission -> resolve employment+person -> persist assignment -> terbitkan
// event ditugaskan (pemicu clone). Memory bus mengirim sinkron; outbox transaksional
// (3.1.2) menyusul agar event tak hilang saat crash setelah commit.
func (uc *AssignEmploymentToTenant) Execute(ctx port.AuthContext, in AssignEmploymentToTenantInput) (*domain.TenantAssignment, error) {
	if err := ctx.RequirePermission(domain.PermAssignmentTugaskan); err != nil {
		return nil, err
	}
	// Cross-tenant butuh otorisasi tambahan (catatan skema id.tenant_assignments).
	if in.CrossTenant {
		if err := ctx.RequirePermission(domain.PermAssignmentCrossTenant); err != nil {
			return nil, err
		}
	}

	emp, err := uc.employments.FindByID(ctx, in.EmploymentID)
	if err != nil {
		return nil, err
	}
	person, err := uc.persons.FindByID(ctx, emp.PersonID)
	if err != nil {
		return nil, err
	}

	// Titik validasi bisnis penugasan. Sengaja kosong di PR-2.2.4 — cross-tenant baru
	// dijaga permission. Diisi PR-2.4.5 (lihat validateAssignment).
	if err := uc.validateAssignment(ctx, in, emp); err != nil {
		return nil, err
	}

	validFrom := in.ValidFrom
	if validFrom.IsZero() {
		validFrom = time.Now()
	}
	a := &domain.TenantAssignment{
		ID:           uuid.New(),
		EmploymentID: emp.ID,
		TenantID:     in.TenantID,
		IsHomeTenant: !in.CrossTenant,
		AssignedBy:   ctx.PersonID(),
		ValidFrom:    validFrom,
		ValidUntil:   in.ValidUntil,
	}
	if err := a.Validate(); err != nil {
		return nil, err
	}
	if err := uc.assignments.Save(ctx, a); err != nil {
		return nil, err
	}

	if err := uc.publisher.Publish(ctx, port.Event{
		Name:     domain.EventEmploymentDitugaskan,
		TenantID: in.TenantID,
		CausedBy: ctx.PersonID().String(),
		Payload: domain.EmploymentDitugaskanPayload{
			AssignmentID:     a.ID,
			EmploymentID:     emp.ID,
			PersonID:         person.ID,
			TenantID:         in.TenantID,
			NIK:              person.NIK,
			NIP:              emp.NIP,
			NamaLengkap:      person.NamaLengkap,
			EmploymentStatus: string(emp.Status),
			IsCrossTenant:    in.CrossTenant,
		},
	}); err != nil {
		return nil, err
	}
	return a, nil
}

// validateAssignment adalah titik validasi bisnis penugasan ke tenant. Stub kosong di
// PR-2.2.4: saat ini penugasan (termasuk cross-tenant) hanya dijaga gerbang permission.
//
// TODO(PR-2.4.5): isi validasi — tenant tujuan ada & aktif (lewat TenantRegistry),
// employment masih aktif, dan cegah duplikat penugasan ke tenant yang sama. Pengisian ini
// akan menambah dependency TenantRegistry ke use case. Lihat ROADMAP "Backlog teknis".
func (uc *AssignEmploymentToTenant) validateAssignment(_ port.AuthContext, _ AssignEmploymentToTenantInput, _ *domain.Employment) error {
	return nil
}
