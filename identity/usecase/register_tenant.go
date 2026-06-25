package usecase

import (
	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/port"
)

// RegisterTenant mendaftarkan tenant baru ke registry sentral (id.tenant_registry).
type RegisterTenant struct {
	registry domain.TenantRegistry
}

func NewRegisterTenant(registry domain.TenantRegistry) *RegisterTenant {
	return &RegisterTenant{registry: registry}
}

// RegisterTenantInput DTO masuk.
type RegisterTenantInput struct {
	TenantID string
	Nama     string
	Tier     int
	DBHost   string
	DBName   string
	DBSchema string
}

// Execute: permission -> bentuk entity -> validasi -> persist. Tenant baru aktif.
func (uc *RegisterTenant) Execute(ctx port.AuthContext, in RegisterTenantInput) (*domain.Tenant, error) {
	if err := ctx.RequirePermission(domain.PermTenantDaftar); err != nil {
		return nil, err
	}
	tier := in.Tier
	if tier == 0 {
		tier = domain.TierShared // default onboarding: shared server, DB per-tenant
	}
	t := &domain.Tenant{
		TenantID: in.TenantID,
		Nama:     in.Nama,
		Tier:     tier,
		DBHost:   in.DBHost,
		DBName:   in.DBName,
		DBSchema: in.DBSchema,
		IsActive: true,
	}
	if err := t.Validate(); err != nil {
		return nil, err
	}
	if err := uc.registry.Save(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}
