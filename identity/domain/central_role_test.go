package domain_test

import (
	"testing"
	"time"

	"github.com/huda-salam/pamong/identity/domain"
)

func TestCentralRole_Validate(t *testing.T) {
	cases := []struct {
		name    string
		role    domain.CentralRole
		wantErr error
	}{
		{"valid global", domain.CentralRole{Name: "super_admin", Label: "Super", ScopeType: domain.ScopeGlobal}, nil},
		{"valid scoped", domain.CentralRole{Name: "regional_helpdesk", Label: "RH", ScopeType: domain.ScopeScoped}, nil},
		{"nama spasi", domain.CentralRole{Name: "super admin", Label: "x", ScopeType: domain.ScopeGlobal}, domain.ErrCentralRoleNameInvalid},
		{"nama huruf besar", domain.CentralRole{Name: "SuperAdmin", Label: "x", ScopeType: domain.ScopeGlobal}, domain.ErrCentralRoleNameInvalid},
		{"label kosong", domain.CentralRole{Name: "super_admin", ScopeType: domain.ScopeGlobal}, domain.ErrCentralRoleLabelKosong},
		{"scope tak dikenal", domain.CentralRole{Name: "super_admin", Label: "x", ScopeType: "regional"}, domain.ErrScopeTypeInvalid},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := c.role.Validate(); err != c.wantErr {
				t.Fatalf("Validate() = %v, mau %v", err, c.wantErr)
			}
		})
	}
}

// TestCentralRoleAssignment_AppliesTo menguji inti DoD PR-2.3.2 secara murni (tanpa DB):
// role global berlaku di semua tenant; scoped hanya di tenant dalam scope; di luar masa
// berlaku tidak aktif. Termasuk dua kasus perbaikan review (fail-closed): scoped tanpa
// tenant_scope tak berlaku di mana pun, dan otoritas datang dari scope_type.
func TestCentralRoleAssignment_AppliesTo(t *testing.T) {
	now := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)
	yesterday := now.Add(-24 * time.Hour)
	tomorrow := now.Add(24 * time.Hour)

	cases := []struct {
		name     string
		scope    domain.ScopeType
		a        domain.CentralRoleAssignment
		tenantID string
		want     bool
	}{
		{"global berlaku tenant A", domain.ScopeGlobal, domain.CentralRoleAssignment{ValidFrom: yesterday}, "pemkot-surabaya", true},
		{"global berlaku tenant B", domain.ScopeGlobal, domain.CentralRoleAssignment{ValidFrom: yesterday}, "pemkot-malang", true},
		{"scoped cocok", domain.ScopeScoped, domain.CentralRoleAssignment{TenantScope: []string{"pemkot-surabaya"}, ValidFrom: yesterday}, "pemkot-surabaya", true},
		{"scoped tidak cocok", domain.ScopeScoped, domain.CentralRoleAssignment{TenantScope: []string{"pemkot-surabaya"}, ValidFrom: yesterday}, "pemkot-malang", false},
		{"scoped salah satu cocok", domain.ScopeScoped, domain.CentralRoleAssignment{TenantScope: []string{"a", "pemkot-malang"}, ValidFrom: yesterday}, "pemkot-malang", true},
		// Perbaikan review: scoped tanpa tenant_scope (data rusak lewat use case) → fail-closed.
		{"scoped tanpa scope = tak berlaku", domain.ScopeScoped, domain.CentralRoleAssignment{ValidFrom: yesterday}, "pemkot-surabaya", false},
		// Otoritas = scope_type: role global dgn tenant_scope nyasar tetap berlaku semua tenant.
		{"global abaikan tenant_scope nyasar", domain.ScopeGlobal, domain.CentralRoleAssignment{TenantScope: []string{"pemkot-malang"}, ValidFrom: yesterday}, "pemkot-surabaya", true},
		{"belum berlaku", domain.ScopeGlobal, domain.CentralRoleAssignment{ValidFrom: tomorrow}, "pemkot-surabaya", false},
		{"sudah kedaluwarsa", domain.ScopeGlobal, domain.CentralRoleAssignment{ValidFrom: yesterday, ValidUntil: &yesterday}, "pemkot-surabaya", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.a.AppliesTo(c.scope, c.tenantID, now); got != c.want {
				t.Fatalf("AppliesTo(%v, %q) = %v, mau %v", c.scope, c.tenantID, got, c.want)
			}
		})
	}
}
