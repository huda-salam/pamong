// Package usecase berisi orkestrator delegasi/PLT (PR-2.3.5): melimpahkan subset permission
// dari pejabat ke pelaksana untuk rentang waktu terbatas. Business logic murni — hanya
// bergantung pada domain/ports (hexagonal). Pola mengikuti use case role tenant.
package usecase

import (
	"time"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/delegation/domain"
	"github.com/huda-salam/pamong/port"
)

// CreateDelegation melimpahkan subset permission dari pejabat (FromUserID) ke pelaksana/PLT
// (ToUserID) untuk rentang waktu terbatas. Menolak permission yang non-delegable. Mutasi
// ter-audit lewat dekorator repo (ADR-003 / PRD F5 "delegasi tercatat di audit").
//
// DEFERRED(Phase-2.4): publish event delegasi (refresh/revoke klaim token) + use case revoke.
type CreateDelegation struct {
	delegations  domain.DelegationRepository
	nonDelegable domain.NonDelegableSet
}

func NewCreateDelegation(delegations domain.DelegationRepository, nonDelegable domain.NonDelegableSet) *CreateDelegation {
	return &CreateDelegation{delegations: delegations, nonDelegable: nonDelegable}
}

// CreateDelegationInput DTO masuk. FromUserID = pejabat asal (mis. atasan yang berhalangan);
// ToUserID = pelaksana. UnitKerjaID nil = jangkauan seluruh tenant.
type CreateDelegationInput struct {
	FromUserID     uuid.UUID
	ToUserID       uuid.UUID
	Permissions    []string
	UnitKerjaID    *uuid.UUID
	IncludeSubtree bool
	Reason         string
	ValidFrom      time.Time
	ValidUntil     time.Time
}

// Execute: permission → tolak non-delegable → bentuk entity → validasi → persist.
func (uc *CreateDelegation) Execute(ctx port.AuthContext, in CreateDelegationInput) (*domain.Delegation, error) {
	if err := ctx.RequirePermission(domain.PermDelegasiBuat); err != nil {
		return nil, err
	}

	perms := dedupStrings(in.Permissions)
	for _, p := range perms {
		if uc.nonDelegable.Contains(p) {
			return nil, domain.ErrPermNonDelegable
		}
	}

	validFrom := in.ValidFrom
	if validFrom.IsZero() {
		validFrom = time.Now()
	}
	d := &domain.Delegation{
		ID:             uuid.New(),
		FromUserID:     in.FromUserID,
		ToUserID:       in.ToUserID,
		Permissions:    perms,
		UnitKerjaID:    in.UnitKerjaID,
		IncludeSubtree: in.IncludeSubtree,
		Reason:         in.Reason,
		ValidFrom:      validFrom,
		ValidUntil:     in.ValidUntil,
		AssignedBy:     ctx.PersonID(),
	}
	if err := d.Validate(); err != nil {
		return nil, err
	}
	if err := uc.delegations.Save(ctx, d); err != nil {
		return nil, err
	}
	return d, nil
}

// dedupStrings mengembalikan elemen unik dengan urutan kemunculan pertama dipertahankan.
// nil/kosong → nil.
func dedupStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
