package usecase

import (
	"time"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/port"
)

// AttachEmployment melekatkan relasi kepegawaian ke person yang sudah ada. Satu person
// bisa punya >1 employment. ASN wajib NIP; non-ASN tanpa NIP (ditegakkan domain.Validate).
type AttachEmployment struct {
	persons     domain.PersonRepository
	employments domain.EmploymentRepository
	publisher   port.EventPublisher
}

func NewAttachEmployment(p domain.PersonRepository, e domain.EmploymentRepository, publisher port.EventPublisher) *AttachEmployment {
	return &AttachEmployment{persons: p, employments: e, publisher: publisher}
}

// AttachEmploymentInput DTO masuk.
type AttachEmploymentInput struct {
	PersonID     uuid.UUID
	Status       domain.EmploymentStatus
	NIP          string
	InstansiAsal string
	ValidFrom    time.Time
	ValidUntil   *time.Time
}

// Execute: permission -> pastikan person ada -> bentuk entity -> validasi -> persist.
func (uc *AttachEmployment) Execute(ctx port.AuthContext, in AttachEmploymentInput) (*domain.Employment, error) {
	if err := ctx.RequirePermission(domain.PermEmploymentLampir); err != nil {
		return nil, err
	}

	// Person harus ada — employment menggantung pada person nyata (FK + kejelasan error).
	if _, err := uc.persons.FindByID(ctx, in.PersonID); err != nil {
		return nil, err
	}

	validFrom := in.ValidFrom
	if validFrom.IsZero() {
		validFrom = time.Now()
	}
	e := &domain.Employment{
		ID:           uuid.New(),
		PersonID:     in.PersonID,
		Status:       in.Status,
		NIP:          in.NIP,
		InstansiAsal: in.InstansiAsal,
		IsActive:     true,
		ValidFrom:    validFrom,
		ValidUntil:   in.ValidUntil,
	}
	if err := e.Validate(); err != nil {
		return nil, err
	}
	if err := uc.employments.Save(ctx, e); err != nil {
		return nil, err
	}

	if err := uc.publisher.Publish(ctx, port.Event{
		Name:     domain.EventEmploymentDibuat,
		CausedBy: ctx.PersonID().String(),
		Payload: domain.EmploymentDibuatPayload{
			EmploymentID: e.ID, PersonID: e.PersonID, Status: string(e.Status), NIP: e.NIP,
		},
	}); err != nil {
		return nil, err
	}
	return e, nil
}
