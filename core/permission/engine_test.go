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

// tr/cr membangun daftar RoleRef dengan LAPIS ASAL eksplisit: tr = nama yang datang dari klaim
// tenant_roles, cr = dari klaim central_roles. Sejak ADR-019 origin adalah bagian dari masukan
// evaluasi, jadi test menuliskannya alih-alih menyerahkannya ke default — dan test tabrakan
// nama di bawah justru bergantung pada perbedaan itu.
func tr(names ...string) []permission.RoleRef { return refsOf(permission.OriginTenant, names) }
func cr(names ...string) []permission.RoleRef { return refsOf(permission.OriginCentral, names) }

func refsOf(o permission.RoleOrigin, names []string) []permission.RoleRef {
	out := make([]permission.RoleRef, 0, len(names))
	for _, n := range names {
		out = append(out, permission.RoleRef{Origin: o, Name: n})
	}
	return out
}

// join menggabungkan beberapa daftar ref (actor yang memegang role dari dua lapis sekaligus).
func join(lists ...[]permission.RoleRef) []permission.RoleRef {
	var out []permission.RoleRef
	for _, l := range lists {
		out = append(out, l...)
	}
	return out
}

func newEngine(strict ...string) *permission.Engine {
	cat := permission.NewMemoryCatalog().
		Define("operator_surat", permission.LayerTenant, permSuratBuat, permSuratBaca).
		Define("pimpinan_surat", permission.LayerTenant, permSuratBaca, permSuratDisposisi).
		Define("super_admin", permission.LayerGlobal, permTenantNonaktif)
	return permission.NewEngine(cat, strict...)
}

func TestAllows_RoleGrantsPermission(t *testing.T) {
	e := newEngine()
	if !e.Allows(tr("operator_surat"), permSuratBuat) {
		t.Fatalf("operator_surat seharusnya boleh %q", permSuratBuat)
	}
}

func TestAllows_DeniedWhenNoRoleGrants(t *testing.T) {
	e := newEngine()
	if e.Allows(tr("operator_surat"), permSuratDisposisi) {
		t.Fatalf("operator_surat seharusnya TIDAK boleh %q", permSuratDisposisi)
	}
}

func TestAllows_UnionAcrossRoles(t *testing.T) {
	e := newEngine()
	// Gabungan dua role: operator (buat) + pimpinan (disposisi) → keduanya didapat.
	roles := tr("operator_surat", "pimpinan_surat")
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
	if e.Allows(tr("role_tak_dikenal"), permSuratBaca) {
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
	if !e.Allows(cr("super_admin"), permTenantNonaktif) {
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
	if e.Allows(tr("bendahara", "operator_surat"), permTenantNonaktif) {
		t.Fatal("strict: role non-global yang tak sepakat harus memblokir (intersection)")
	}
}

func TestAllows_StrictIntersection_AllowWhenAllGrant(t *testing.T) {
	e := strictEngine()
	// Hanya role-role non-global yang memberi → intersection lolos.
	if !e.Allows(join(tr("bendahara"), cr("helpdesk_regional")), permTenantNonaktif) {
		t.Fatal("strict: semua role non-global memberi → harus IZIN")
	}
	// Satu role saja yang memberi (tak ada yang menolak) → lolos.
	if !e.Allows(tr("bendahara"), permTenantNonaktif) {
		t.Fatal("strict: satu role non-global memberi, tanpa penolak → harus IZIN")
	}
}

func TestAllows_GlobalOverridesStrict(t *testing.T) {
	e := strictEngine()
	// super_admin (global) memberi → IZIN walau role tenant (operator) menolak,
	// dan terlepas dari urutan dalam slice (global dicek di seluruh daftar).
	if !e.Allows(join(tr("operator_surat"), cr("super_admin")), permTenantNonaktif) {
		t.Fatal("global harus menang tanpa syarat, termasuk atas strict-deny")
	}
	if !e.Allows(join(cr("super_admin"), tr("operator_surat")), permTenantNonaktif) {
		t.Fatal("urutan tidak boleh memengaruhi: global tetap menang")
	}
}

func TestAllows_GlobalWithoutGrantIsNeutral(t *testing.T) {
	// Role global yang TIDAK memberi perm tidak memblokir intersection non-global.
	cat := permission.NewMemoryCatalog().
		Define("bendahara", permission.LayerTenant, permTenantNonaktif).
		Define("platform_helpdesk", permission.LayerGlobal, permSuratBaca) // tak memberi nonaktif
	e := permission.NewEngine(cat, permTenantNonaktif)
	if !e.Allows(join(tr("bendahara"), cr("platform_helpdesk")), permTenantNonaktif) {
		t.Fatal("global tanpa grant harus netral, tidak memblokir role non-global")
	}
}

func TestAllows_NonStrictUnionUnaffected(t *testing.T) {
	e := strictEngine()
	// permSuratBuat TIDAK strict → union biasa: operator_surat cukup walau bendahara tak memberi.
	if !e.Allows(tr("bendahara", "operator_surat"), permSuratBuat) {
		t.Fatal("non-strict harus tetap union (lebih permisif menang)")
	}
}

func TestAllows_StrictDeniedWhenOnlyGlobalNonGranting(t *testing.T) {
	// Hanya pegang role global yang tak memberi perm strict → tidak ada lapis non-global
	// yang memberi → DITOLAK (global netral, intersection kosong).
	cat := permission.NewMemoryCatalog().
		Define("platform_helpdesk", permission.LayerGlobal, permSuratBaca)
	e := permission.NewEngine(cat, permTenantNonaktif)
	if e.Allows(cr("platform_helpdesk"), permTenantNonaktif) {
		t.Fatal("strict tanpa role non-global pemberi harus DITOLAK")
	}
}

// --- B8 / ADR-019: lapis asal role dibawa sampai ke titik evaluasi ---

// TestCompositeCatalog_NameCollisionResolvedPerOrigin mengunci inti ADR-019. Sebelumnya
// composite mencoba central dulu atas nama TELANJANG, sehingga role tenant bernama persis
// seperti role sentral me-resolve ke definisi sentral — mewarisi permission-nya sekaligus
// LayerGlobal. Kini nama yang sama di dua lapis adalah DUA role yang berbeda.
func TestCompositeCatalog_NameCollisionResolvedPerOrigin(t *testing.T) {
	central := permission.NewMemoryCatalog().
		Define("super_admin", permission.LayerGlobal, permTenantNonaktif)
	tenant := permission.NewMemoryCatalog().
		Define("super_admin", permission.LayerTenant, permSuratBaca).
		Define("operator_surat", permission.LayerTenant, permSuratBuat)
	comp := permission.NewCompositeCatalog(central, tenant)

	// Ref dari klaim central_roles → definisi sentral.
	role, ok := comp.LookupRef(permission.CentralRef("super_admin"))
	if !ok || role.Layer != permission.LayerGlobal {
		t.Fatalf("ref central super_admin harus definisi sentral, dapat layer=%v ok=%v", role.Layer, ok)
	}
	if len(role.Permissions) != 1 || role.Permissions[0] != permTenantNonaktif {
		t.Fatalf("ref central harus membawa permission sentral, dapat %v", role.Permissions)
	}

	// Ref dari klaim tenant_roles dengan NAMA YANG SAMA → definisi tenant, lapis tenant.
	role, ok = comp.LookupRef(permission.TenantRef("super_admin"))
	if !ok || role.Layer != permission.LayerTenant {
		t.Fatalf("ref tenant super_admin harus definisi tenant ber-LayerTenant, dapat layer=%v ok=%v", role.Layer, ok)
	}
	if len(role.Permissions) != 1 || role.Permissions[0] != permSuratBaca {
		t.Fatalf("ref tenant harus membawa permission tenant, dapat %v", role.Permissions)
	}

	// Role yang hanya ada di tenant tetap terlihat lewat ref tenant.
	if r, ok := comp.LookupRef(permission.TenantRef("operator_surat")); !ok || r.Layer != permission.LayerTenant {
		t.Fatalf("role tenant harus terlihat lewat composite, dapat ok=%v layer=%v", ok, r.Layer)
	}
	// ...tapi TIDAK lewat ref central: lapis salah = tak ditemukan, bukan dinaikkan.
	if _, ok := comp.LookupRef(permission.CentralRef("operator_surat")); ok {
		t.Error("role tenant tidak boleh ditemukan lewat ref central")
	}
	if _, ok := comp.LookupRef(permission.TenantRef("tak_ada")); ok {
		t.Error("role tak terdaftar di kedua catalog seharusnya ok=false")
	}
}

// TestAllows_TenantRoleImpersonatingCentralNameGainsNothing adalah skenario serangan B8 utuh,
// dijalankan lewat Engine: admin tenant membuat role tenant bernama persis "super_admin"
// (daftar permission-nya boleh kosong) dan menugaskannya ke dirinya sendiri. Sebelum ADR-019,
// klaim tenant_roles=["super_admin"] cukup untuk membuka seluruh permission role sentral
// ber-LayerGlobal — termasuk identity:* — tanpa pernah menyebut namespace itu (jalur kedua
// yang melewati B6). Kini ia hanya mendapat apa yang role tenant-nya sendiri berikan.
func TestAllows_TenantRoleImpersonatingCentralNameGainsNothing(t *testing.T) {
	central := permission.NewMemoryCatalog().
		Define("super_admin", permission.LayerGlobal, permTenantNonaktif)
	tenant := permission.NewMemoryCatalog().
		Define("super_admin", permission.LayerTenant) // dibuat admin tenant, tanpa permission
	e := permission.NewEngine(permission.NewCompositeCatalog(central, tenant))

	if e.Allows(tr("super_admin"), permTenantNonaktif) {
		t.Fatal("B8: role TENANT bernama sama dengan role sentral tidak boleh mewarisi permission sentral")
	}
	// Pemegang role sentral yang asli tetap dapat izinnya.
	if !e.Allows(cr("super_admin"), permTenantNonaktif) {
		t.Fatal("role sentral asli harus tetap berlaku lewat ref central")
	}
}

// TestAllows_TenantRoleImpersonatingCentralNameNotGlobalForStrict menutup sisi kedua warisan
// LayerGlobal: bukan hanya permission-nya, tapi PRIORITASNYA. Role tenant penyamar yang
// memberi perm strict tidak boleh menang tanpa syarat atas role non-global lain yang menolak.
func TestAllows_TenantRoleImpersonatingCentralNameNotGlobalForStrict(t *testing.T) {
	central := permission.NewMemoryCatalog().
		Define("super_admin", permission.LayerGlobal, permTenantNonaktif)
	tenant := permission.NewMemoryCatalog().
		Define("super_admin", permission.LayerTenant, permTenantNonaktif).
		Define("operator_surat", permission.LayerTenant, permSuratBuat) // tak memberi perm strict
	e := permission.NewEngine(permission.NewCompositeCatalog(central, tenant), permTenantNonaktif)

	if e.Allows(tr("super_admin", "operator_surat"), permTenantNonaktif) {
		t.Fatal("B8: role tenant penyamar harus tunduk strict-intersection, bukan menang sebagai global")
	}
}

// TestCompositeCatalog_NilLayerFailsClosed: composite tanpa katalog tenant (citizen) tak boleh
// mencari ref tenant di katalog central sebagai gantinya.
func TestCompositeCatalog_NilLayerFailsClosed(t *testing.T) {
	central := permission.NewMemoryCatalog().
		Define("super_admin", permission.LayerGlobal, permTenantNonaktif)
	comp := permission.NewCompositeCatalog(central, nil)

	if _, ok := comp.LookupRef(permission.TenantRef("super_admin")); ok {
		t.Fatal("ref tenant pada composite tanpa katalog tenant harus tak ditemukan")
	}
	if _, ok := comp.LookupRef(permission.CentralRef("super_admin")); !ok {
		t.Fatal("ref central harus tetap ditemukan")
	}
}

// TestMemoryCatalog_LookupRefRespectsOrigin: katalog memori (dipakai test & bootstrap awal)
// juga menegakkan origin, bukan hanya composite — kalau tidak, seluruh test di atas menguji
// jalur yang berbeda dari yang dipakai bootstrap.
func TestMemoryCatalog_LookupRefRespectsOrigin(t *testing.T) {
	cat := permission.NewMemoryCatalog().
		Define("operator_surat", permission.LayerTenant, permSuratBuat).
		Define("helpdesk_regional", permission.LayerScoped, permTenantNonaktif).
		Define("super_admin", permission.LayerGlobal, permTenantNonaktif)

	cases := []struct {
		ref  permission.RoleRef
		want bool
	}{
		{permission.TenantRef("operator_surat"), true},
		{permission.CentralRef("operator_surat"), false}, // tenant tak boleh naik lewat ref central
		{permission.CentralRef("helpdesk_regional"), true},
		{permission.TenantRef("helpdesk_regional"), false},
		{permission.CentralRef("super_admin"), true},
		{permission.TenantRef("super_admin"), false},
	}
	for _, c := range cases {
		if _, ok := cat.LookupRef(c.ref); ok != c.want {
			t.Errorf("LookupRef(%+v) ok=%v, mau %v", c.ref, ok, c.want)
		}
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
