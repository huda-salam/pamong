package domain_test

import (
	"errors"
	"testing"

	"github.com/huda-salam/pamong/identity/domain"
)

func validTenant() domain.Tenant {
	return domain.Tenant{
		TenantID: "pemkot-surabaya", Nama: "Pemkot Surabaya",
		Tier: domain.TierShared, DBHost: "db1", DBName: "gov_pemkot_surabaya",
	}
}

func TestTenant_Validate(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*domain.Tenant)
		wantErr error
	}{
		{"valid", func(*domain.Tenant) {}, nil},
		{"tenant_id huruf besar", func(t *domain.Tenant) { t.TenantID = "Pemkot" }, domain.ErrTenantIDInvalid},
		{"tenant_id terlalu pendek", func(t *domain.Tenant) { t.TenantID = "ab" }, domain.ErrTenantIDInvalid},
		{"nama kosong", func(t *domain.Tenant) { t.Nama = "" }, domain.ErrTenantNamaKosong},
		{"tier 0", func(t *domain.Tenant) { t.Tier = 0 }, domain.ErrTenantTierInvalid},
		{"tier 4", func(t *domain.Tenant) { t.Tier = 4 }, domain.ErrTenantTierInvalid},
		{"db_host kosong", func(t *domain.Tenant) { t.DBHost = "" }, domain.ErrTenantDBKosong},
		{"db_name kosong", func(t *domain.Tenant) { t.DBName = "" }, domain.ErrTenantDBKosong},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tenant := validTenant()
			c.mutate(&tenant)
			err := tenant.Validate()
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("Validate() = %v, mau %v", err, c.wantErr)
			}
		})
	}
}
