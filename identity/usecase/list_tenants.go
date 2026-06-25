package usecase

import (
	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/port"
)

// ListTenants mengembalikan seluruh tenant terdaftar (untuk admin platform).
type ListTenants struct {
	registry domain.TenantRegistry
}

func NewListTenants(registry domain.TenantRegistry) *ListTenants {
	return &ListTenants{registry: registry}
}

// Execute: permission -> list. Read-only.
func (uc *ListTenants) Execute(ctx port.AuthContext) ([]*domain.Tenant, error) {
	if err := ctx.RequirePermission(domain.PermTenantBaca); err != nil {
		return nil, err
	}
	return uc.registry.List(ctx)
}
