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

// --- Resolusi konflik penuh (PR-2.3.3): global-precedence + strict-intersection ---

// strictEngine menyiapkan dua role tenant yang TIDAK sepakat soal permTenantNonaktif:
// bendahara memberinya, operator_surat tidak. Plus role global super_admin yang memberinya.
// permTenantNonaktif ditandai strict.
func strictEngine() *permission.Engine {
	cat := permission.NewMemoryCatalog().
		Define("bendahara", permission.LayerTenant, permSuratBaca, permTenantNonaktif).
		Define("operator_surat", permission.LayerTenant, permSuratBuat, permSuratBaca).
		Define("helpdesk_regional", permission.LayerScoped, permTenantNonaktif).
		Define("super_admin", permission.LayerGlobal, permTenantNonaktif)
	return permission.NewEngine(cat, permTenantNonaktif)
}

func TestAllows_StrictIntersection_DenyWhenRolesDisagree(t *testing.T) {
	e := strictEngine()
	// bendahara memberi, operator_surat tidak → intersection gagal → DITOLAK.
	if e.Allows([]string{"bendahara", "operator_surat"}, permTenantNonaktif) {
		t.Fatal("strict: role non-global yang tak sepakat harus memblokir (intersection)")
	}
}

func TestAllows_StrictIntersection_AllowWhenAllGrant(t *testing.T) {
	e := strictEngine()
	// Hanya role-role non-global yang memberi → intersection lolos.
	if !e.Allows([]string{"bendahara", "helpdesk_regional"}, permTenantNonaktif) {
		t.Fatal("strict: semua role non-global memberi → harus IZIN")
	}
	// Satu role saja yang memberi (tak ada yang menolak) → lolos.
	if !e.Allows([]string{"bendahara"}, permTenantNonaktif) {
		t.Fatal("strict: satu role non-global memberi, tanpa penolak → harus IZIN")
	}
}

func TestAllows_GlobalOverridesStrict(t *testing.T) {
	e := strictEngine()
	// super_admin (global) memberi → IZIN walau role tenant (operator) menolak,
	// dan terlepas dari urutan dalam slice (global dicek di seluruh daftar).
	if !e.Allows([]string{"operator_surat", "super_admin"}, permTenantNonaktif) {
		t.Fatal("global harus menang tanpa syarat, termasuk atas strict-deny")
	}
	if !e.Allows([]string{"super_admin", "operator_surat"}, permTenantNonaktif) {
		t.Fatal("urutan tidak boleh memengaruhi: global tetap menang")
	}
}

func TestAllows_GlobalWithoutGrantIsNeutral(t *testing.T) {
	// Role global yang TIDAK memberi perm tidak memblokir intersection non-global.
	cat := permission.NewMemoryCatalog().
		Define("bendahara", permission.LayerTenant, permTenantNonaktif).
		Define("platform_helpdesk", permission.LayerGlobal, permSuratBaca) // tak memberi nonaktif
	e := permission.NewEngine(cat, permTenantNonaktif)
	if !e.Allows([]string{"bendahara", "platform_helpdesk"}, permTenantNonaktif) {
		t.Fatal("global tanpa grant harus netral, tidak memblokir role non-global")
	}
}

func TestAllows_NonStrictUnionUnaffected(t *testing.T) {
	e := strictEngine()
	// permSuratBuat TIDAK strict → union biasa: operator_surat cukup walau bendahara tak memberi.
	if !e.Allows([]string{"bendahara", "operator_surat"}, permSuratBuat) {
		t.Fatal("non-strict harus tetap union (lebih permisif menang)")
	}
}

func TestAllows_StrictDeniedWhenOnlyGlobalNonGranting(t *testing.T) {
	// Hanya pegang role global yang tak memberi perm strict → tidak ada lapis non-global
	// yang memberi → DITOLAK (global netral, intersection kosong).
	cat := permission.NewMemoryCatalog().
		Define("platform_helpdesk", permission.LayerGlobal, permSuratBaca)
	e := permission.NewEngine(cat, permTenantNonaktif)
	if e.Allows([]string{"platform_helpdesk"}, permTenantNonaktif) {
		t.Fatal("strict tanpa role non-global pemberi harus DITOLAK")
	}
}

func TestCompositeCatalog_CentralWinsOnNameCollision(t *testing.T) {
	// Nama "super_admin" ada di kedua catalog dengan layer berbeda; central (didahulukan)
	// harus menang → LayerGlobal, bukan LayerTenant milik catalog tenant.
	central := permission.NewMemoryCatalog().
		Define("super_admin", permission.LayerGlobal, permTenantNonaktif)
	tenant := permission.NewMemoryCatalog().
		Define("super_admin", permission.LayerTenant, permSuratBaca).
		Define("operator_surat", permission.LayerTenant, permSuratBuat)
	comp := permission.NewCompositeCatalog(central, tenant)

	role, ok := comp.Lookup("super_admin")
	if !ok || role.Layer != permission.LayerGlobal {
		t.Fatalf("central harus menang pada bentrok nama, dapat layer=%v ok=%v", role.Layer, ok)
	}
	// Role yang hanya ada di tenant tetap terlihat.
	if r, ok := comp.Lookup("operator_surat"); !ok || r.Layer != permission.LayerTenant {
		t.Fatalf("role tenant harus terlihat lewat composite, dapat ok=%v layer=%v", ok, r.Layer)
	}
	if _, ok := comp.Lookup("tak_ada"); ok {
		t.Error("role tak terdaftar di kedua catalog seharusnya ok=false")
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
