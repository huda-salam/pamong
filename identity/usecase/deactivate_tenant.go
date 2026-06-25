package usecase

import (
	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/port"
)

// DeactivateTenant menonaktifkan tenant (is_active=false). Data tenant tidak dihapus;
// hanya ditandai nonaktif sehingga tidak bisa di-resolve untuk akses baru.
type DeactivateTenant struct {
	registry domain.TenantRegistry
}

func NewDeactivateTenant(registry domain.TenantRegistry) *DeactivateTenant {
	return &DeactivateTenant{registry: registry}
}

// Execute: permission -> set nonaktif. Tenant tak dikenal -> ErrNotFound (dari adapter).
func (uc *DeactivateTenant) Execute(ctx port.AuthContext, tenantID string) error {
	if err := ctx.RequirePermission(domain.PermTenantNonaktif); err != nil {
		return err
	}
	return uc.registry.SetActive(ctx, tenantID, false)
}
