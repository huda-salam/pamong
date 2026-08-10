package domain_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/tenantrole/domain"
)

func TestTenantRole_Validate(t *testing.T) {
	tests := []struct {
		name string
		in   domain.TenantRole
		want error
	}{
		{"valid", domain.TenantRole{Name: "bendahara_pengeluaran", Label: "Bendahara"}, nil},
		{"nama spasi", domain.TenantRole{Name: "bendahara pengeluaran", Label: "x"}, domain.ErrTenantRoleNameInvalid},
		{"nama huruf besar", domain.TenantRole{Name: "Bendahara", Label: "x"}, domain.ErrTenantRoleNameInvalid},
		{"nama terlalu pendek", domain.TenantRole{Name: "ab", Label: "x"}, domain.ErrTenantRoleNameInvalid},
		{"label kosong", domain.TenantRole{Name: "ppk_opd", Label: ""}, domain.ErrTenantRoleLabelKosong},
		{
			"permission modul bisnis & iam tetap boleh",
			domain.TenantRole{Name: "operator_surat", Label: "Operator", Permissions: []string{
				"surat_masuk:surat:buat", "iam:tenant_role:assign", "customization:label:ubah",
			}},
			nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.in.Validate(); err != tt.want {
				t.Errorf("Validate() = %v, mau %v", err, tt.want)
			}
		})
	}
}

func TestTenantRoleAssignment_Validate(t *testing.T) {
	uid := uuid.New()
	tests := []struct {
		name string
		in   domain.TenantRoleAssignment
		want error
	}{
		{"valid", domain.TenantRoleAssignment{UserID: uid, RoleID: uid, AssignedBy: uid}, nil},
		{"user kosong", domain.TenantRoleAssignment{RoleID: uid, AssignedBy: uid}, domain.ErrUserIDKosong},
		{"role kosong", domain.TenantRoleAssignment{UserID: uid, AssignedBy: uid}, domain.ErrRoleIDKosong},
		{"assigned_by kosong", domain.TenantRoleAssignment{UserID: uid, RoleID: uid}, domain.ErrAssignedByKosong},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.in.Validate(); err != tt.want {
				t.Errorf("Validate() = %v, mau %v", err, tt.want)
			}
		})
	}
}

func TestTenantRoleAssignment_AppliesTo(t *testing.T) {
	now := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)
	future := now.Add(time.Hour)
	past := now.Add(-time.Hour)

	if (&domain.TenantRoleAssignment{ValidFrom: future}).AppliesTo(now) {
		t.Error("belum mulai berlaku seharusnya tidak aktif")
	}
	if (&domain.TenantRoleAssignment{ValidFrom: past, ValidUntil: &past}).AppliesTo(now) {
		t.Error("sudah kedaluwarsa seharusnya tidak aktif")
	}
	if !(&domain.TenantRoleAssignment{ValidFrom: past, ValidUntil: &future}).AppliesTo(now) {
		t.Error("dalam masa berlaku seharusnya aktif")
	}
	if !(&domain.TenantRoleAssignment{ValidFrom: past}).AppliesTo(now) {
		t.Error("tanpa valid_until (tak terbatas) seharusnya aktif")
	}
}

// Pagar eskalasi tenant → platform: role TENANT tak boleh memberikan permission `identity:*`.
//
// Tanpa pagar ini, admin tenant pemegang `iam:tenant_role:buat` bisa membuat role berisi
// `identity:credential:buat`, menugaskannya ke dirinya sendiri (core/permission.Engine
// menggabungkan grant lintas lapis secara UNION), lalu menerbitkan kredensial ber-password
// pilihannya untuk person mana pun yang id-nya ia ketahui — termasuk admin platform yang
// ter-clone ke tenantnya — dan login sebagai orang itu. Satu tenant mengambil alih platform.
//
// Kelemahannya dorman sampai `/admin/identity/*` dipasang (PR-W2); test ini yang menahannya
// tetap tertutup.
func TestTenantRole_Validate_MenolakNamespaceIdentity(t *testing.T) {
	terlarang := []string{
		"identity:credential:buat",
		"identity:person:buat",
		"identity:assignment:tugaskan",
		"identity:central_role:assign",
		"identity:tenant:baca", // seluruh namespace, bukan hanya yang bermutasi
	}
	for _, perm := range terlarang {
		t.Run(perm, func(t *testing.T) {
			role := domain.TenantRole{
				Name: "admin_tenant", Label: "Admin Tenant",
				// Disisipkan di antara permission yang sah: pemeriksaan harus menyapu SELURUH
				// daftar, bukan hanya elemen pertama.
				Permissions: []string{"surat_masuk:surat:buat", perm, "iam:tenant_role:buat"},
			}
			if err := role.Validate(); err != domain.ErrPermissionTerlarangTenant {
				t.Fatalf("Validate() = %v, mau ErrPermissionTerlarangTenant — role tenant yang bisa "+
					"memberi %q membuka jalur eskalasi tenant → platform", err, perm)
			}
		})
	}
}
