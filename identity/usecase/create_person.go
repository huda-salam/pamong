// Package usecase berisi orchestrator identitas: create person, attach employment,
// resolve. Hanya bergantung pada port domain — tidak tahu infra konkret.
package usecase

import (
	"time"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/port"
)

// CreatePerson membuat master person baru (anchor NIK). Person adalah akar identitas;
// employment & credential menyusul lewat use case lain.
type CreatePerson struct {
	persons   domain.PersonRepository
	publisher port.EventPublisher
}

func NewCreatePerson(persons domain.PersonRepository, publisher port.EventPublisher) *CreatePerson {
	return &CreatePerson{persons: persons, publisher: publisher}
}

// CreatePersonInput DTO masuk; ID & timestamp di-generate sistem.
type CreatePersonInput struct {
	NIK         string
	NamaLengkap string
	NoHP        string
	Email       string
	TglLahir    *time.Time
}

// Execute: permission -> bentuk entity -> validasi -> persist -> terbitkan event
// identity.person.dibuat. Konflik NIK duplikat dipetakan adapter ke core.ErrConflict.
func (uc *CreatePerson) Execute(ctx port.AuthContext, in CreatePersonInput) (*domain.Person, error) {
	if err := ctx.RequirePermission(domain.PermPersonBuat); err != nil {
		return nil, err
	}

	p := &domain.Person{
		ID:          uuid.New(),
		NIK:         in.NIK,
		NamaLengkap: in.NamaLengkap,
		NoHP:        in.NoHP,
		Email:       in.Email,
		TglLahir:    in.TglLahir,
		IsActive:    true,
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	if err := uc.persons.Save(ctx, p); err != nil {
		return nil, err
	}

	if err := uc.publisher.Publish(ctx, port.Event{
		Name:     domain.EventPersonDibuat,
		CausedBy: ctx.PersonID().String(),
		Payload: domain.PersonDibuatPayload{
			PersonID: p.ID, NIK: p.NIK, NamaLengkap: p.NamaLengkap,
		},
	}); err != nil {
		return nil, err
	}
	return p, nil
}
