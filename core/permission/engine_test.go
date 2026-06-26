package permission_test

import (
	"testing"

	"github.com/huda-salam/pamong/core/permission"
)

// Konstanta permission lokal untuk test — di produksi selalu dari konstanta modul.
const (
	permSuratBuat      = "surat_masuk:surat:buat"
	permSuratBaca      = "surat_masuk:surat:baca"
	permSuratDisposisi = "surat_masuk:surat:disposisi"
	permTenantNonaktif = "identity:tenant:nonaktif"
)

func newEngine(strict ...string) *permission.Engine {
	cat := permission.NewMemoryCatalog().
		Define("operator_surat", permission.LayerTenant, permSuratBuat, permSuratBaca).
		Define("pimpinan_surat", permission.LayerTenant, permSuratBaca, permSuratDisposisi).
		Define("super_admin", permission.LayerGlobal, permTenantNonaktif)
	return permission.NewEngine(cat, strict...)
}

func TestAllows_RoleGrantsPermission(t *testing.T) {
	e := newEngine()
	if !e.Allows([]string{"operator_surat"}, permSuratBuat) {
		t.Fatalf("operator_surat seharusnya boleh %q", permSuratBuat)
	}
}

func TestAllows_DeniedWhenNoRoleGrants(t *testing.T) {
	e := newEngine()
	if e.Allows([]string{"operator_surat"}, permSuratDisposisi) {
		t.Fatalf("operator_surat seharusnya TIDAK boleh %q", permSuratDisposisi)
	}
}

func TestAllows_UnionAcrossRoles(t *testing.T) {
	e := newEngine()
	// Gabungan dua role: operator (buat) + pimpinan (disposisi) → keduanya didapat.
	roles := []string{"operator_surat", "pimpinan_surat"}
	if !e.Allows(roles, permSuratBuat) {
		t.Errorf("union: seharusnya boleh %q dari operator_surat", permSuratBuat)
	}
	if !e.Allows(roles, permSuratDisposisi) {
		t.Errorf("union: seharusnya boleh %q dari pimpinan_surat", permSuratDisposisi)
	}
}

func TestAllows_UnknownRoleIgnored(t *testing.T) {
	e := newEngine()
	// Role tak dikenal diabaikan, tidak memberi permission apa pun.
	if e.Allows([]string{"role_tak_dikenal"}, permSuratBaca) {
		t.Fatal("role tak dikenal tidak boleh memberi permission")
	}
}

func TestAllows_EmptyRolesDenied(t *testing.T) {
	e := newEngine()
	if e.Allows(nil, permSuratBaca) {
		t.Fatal("tanpa role tidak boleh ada permission")
	}
}

func TestAllows_GlobalRole(t *testing.T) {
	e := newEngine()
	if !e.Allows([]string{"super_admin"}, permTenantNonaktif) {
		t.Fatalf("super_admin (global) seharusnya boleh %q", permTenantNonaktif)
	}
}

func TestIsStrict(t *testing.T) {
	e := newEngine(permTenantNonaktif)
	if !e.IsStrict(permTenantNonaktif) {
		t.Errorf("%q seharusnya ditandai strict", permTenantNonaktif)
	}
	if e.IsStrict(permSuratBaca) {
		t.Errorf("%q tidak ditandai strict", permSuratBaca)
	}
}

func TestMemoryCatalog_Lookup(t *testing.T) {
	cat := permission.NewMemoryCatalog().
		Define("operator_surat", permission.LayerTenant, permSuratBuat)

	role, ok := cat.Lookup("operator_surat")
	if !ok {
		t.Fatal("operator_surat seharusnya terdaftar")
	}
	if role.Layer != permission.LayerTenant {
		t.Errorf("layer = %v, mau LayerTenant", role.Layer)
	}
	if len(role.Permissions) != 1 || role.Permissions[0] != permSuratBuat {
		t.Errorf("permissions = %v, mau [%q]", role.Permissions, permSuratBuat)
	}

	if _, ok := cat.Lookup("tak_ada"); ok {
		t.Error("role tak terdaftar seharusnya ok=false")
	}
}
