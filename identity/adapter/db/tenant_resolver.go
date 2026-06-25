package db

import (
	"context"

	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/port"
)

// TenantResolverAdapter mengimplementasi port.TenantResolver di atas TenantRegistry
// identity. Gateway memakai port-nya tanpa import identity/ (wiring di bootstrap).
type TenantResolverAdapter struct {
	registry domain.TenantRegistry
}

func NewTenantResolver(registry domain.TenantRegistry) *TenantResolverAdapter {
	return &TenantResolverAdapter{registry: registry}
}

var _ port.TenantResolver = (*TenantResolverAdapter)(nil)

// Resolve memetakan domain.Tenant ke port.TenantInfo. Tenant tak dikenal → ErrNotFound
// (dari registry); status aktif diteruskan apa adanya agar middleware yang memutuskan.
func (a *TenantResolverAdapter) Resolve(ctx context.Context, tenantID string) (*port.TenantInfo, error) {
	t, err := a.registry.FindByID(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return &port.TenantInfo{
		TenantID: t.TenantID,
		Tier:     t.Tier,
		DBHost:   t.DBHost,
		DBName:   t.DBName,
		DBSchema: t.DBSchema,
		IsActive: t.IsActive,
	}, nil
}
