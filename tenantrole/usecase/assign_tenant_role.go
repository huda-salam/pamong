package usecase

import (
	"time"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/port"
	"github.com/huda-salam/pamong/tenantrole/domain"
)

// AssignTenantRole menugaskan role tenant ke seorang user (gov.user_profiles.id). UnitKerjaID
// opsional menyempitkan scope ke satu unit kerja (penegakan ditunda 2.3.5). Mutasi ter-audit
// lewat dekorator repo (ADR-003).
//
// DEFERRED(Phase-2.4): publish event penugasan role tenant untuk refresh/revoke token.
type AssignTenantRole struct {
	assignments domain.TenantRoleAssignmentRepository
}

func NewAssignTenantRole(assignments domain.TenantRoleAssignmentRepository) *AssignTenantRole {
	return &AssignTenantRole{assignments: assignments}
}

// AssignTenantRoleInput DTO masuk. UnitKerjaID nil = berlaku seluruh tenant.
type AssignTenantRoleInput struct {
	UserID      uuid.UUID
	RoleID      uuid.UUID
	UnitKerjaID *uuid.UUID
	ValidFrom   time.Time
	ValidUntil  *time.Time
}

// Execute: permission → bentuk assignment → validasi → persist.
func (uc *AssignTenantRole) Execute(ctx port.AuthContext, in AssignTenantRoleInput) (*domain.TenantRoleAssignment, error) {
	if err := ctx.RequirePermission(domain.PermTenantRoleAssign); err != nil {
		return nil, err
	}

	validFrom := in.ValidFrom
	if validFrom.IsZero() {
		validFrom = time.Now()
	}
	a := &domain.TenantRoleAssignment{
		ID:          uuid.New(),
		UserID:      in.UserID,
		RoleID:      in.RoleID,
		UnitKerjaID: in.UnitKerjaID,
		AssignedBy:  ctx.PersonID(),
		ValidFrom:   validFrom,
		ValidUntil:  in.ValidUntil,
	}
	if err := a.Validate(); err != nil {
		return nil, err
	}
	if err := uc.assignments.Save(ctx, a); err != nil {
		return nil, err
	}
	return a, nil
}
