package usecase

import (
	"time"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/port"
)

// AssignCentralRole menugaskan role sentral ke seorang person. Untuk role scoped, tenant_scope
// menetapkan tenant mana yang berlaku; untuk role global, tenant_scope harus kosong (berlaku
// semua tenant). Koherensi scope_type role vs tenant_scope ditegakkan di sini karena di sinilah
// role-nya dimuat. Mutasi ter-audit lewat dekorator repo (ADR-003).
//
// DEFERRED(Phase-2.4): publish identity.central_role.diassign untuk refresh/revoke token.
type AssignCentralRole struct {
	roles       domain.CentralRoleRepository
	assignments domain.CentralRoleAssignmentRepository
}

func NewAssignCentralRole(
	roles domain.CentralRoleRepository,
	assignments domain.CentralRoleAssignmentRepository,
) *AssignCentralRole {
	return &AssignCentralRole{roles: roles, assignments: assignments}
}

// AssignCentralRoleInput DTO masuk. TenantScope wajib untuk role scoped, harus kosong untuk global.
type AssignCentralRoleInput struct {
	PersonID    uuid.UUID
	RoleID      uuid.UUID
	TenantScope []string
	ValidFrom   time.Time
	ValidUntil  *time.Time
}

// Execute: permission -> muat role -> cek koherensi scope -> bentuk assignment -> validasi -> persist.
func (uc *AssignCentralRole) Execute(ctx port.AuthContext, in AssignCentralRoleInput) (*domain.CentralRoleAssignment, error) {
	if err := ctx.RequirePermission(domain.PermCentralRoleAssign); err != nil {
		return nil, err
	}

	role, err := uc.roles.FindByID(ctx, in.RoleID)
	if err != nil {
		return nil, err
	}
	if err := checkScopeCoherence(role.ScopeType, in.TenantScope); err != nil {
		return nil, err
	}

	validFrom := in.ValidFrom
	if validFrom.IsZero() {
		validFrom = time.Now()
	}
	a := &domain.CentralRoleAssignment{
		ID:          uuid.New(),
		PersonID:    in.PersonID,
		RoleID:      in.RoleID,
		TenantScope: in.TenantScope,
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

// checkScopeCoherence memastikan tenant_scope sesuai sub-tipe role: scoped wajib ada scope,
// global tidak boleh punya scope.
func checkScopeCoherence(scope domain.ScopeType, tenantScope []string) error {
	switch scope {
	case domain.ScopeScoped:
		if len(tenantScope) == 0 {
			return domain.ErrScopeWajibUntukScoped
		}
	case domain.ScopeGlobal:
		if len(tenantScope) > 0 {
			return domain.ErrScopeDilarangGlobal
		}
	}
	return nil
}
