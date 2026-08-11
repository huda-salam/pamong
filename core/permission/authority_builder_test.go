package permission_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/huda-salam/pamong/core/permission"
)

// stubGrants adalah GrantResolver in-memory (pola fake lokal seperti adapter lain).
type stubGrants struct {
	grants []permission.Grant
	err    error
	calls  []uuid.UUID
}

func (s *stubGrants) Grants(_ context.Context, userID uuid.UUID) ([]permission.Grant, error) {
	s.calls = append(s.calls, userID)
	if s.err != nil {
		return nil, s.err
	}
	return s.grants, nil
}

func centralCatalog() *permission.MemoryCatalog {
	return permission.NewMemoryCatalog().
		Define("platform_helpdesk", permission.LayerGlobal, "surat_masuk:surat:baca").
		Define("platform_auditor", permission.LayerGlobal, "surat_masuk:surat:baca", "gov:audit:baca").
		Define("operator_surat", permission.LayerTenant, "surat_masuk:surat:buat")
}

// TestCentralGrants_TenantWide: role sentral tidak punya konsep unit kerja — wewenangnya berlaku
// se-tenant. Grant unit-scoped akan membuat helpdesk kementerian mendadak butuh assignment unit.
func TestCentralGrants_TenantWide(t *testing.T) {
	got := permission.CentralGrants(centralCatalog(), []permission.RoleRef{
		permission.CentralRef("platform_helpdesk"),
	})
	if len(got) != 1 {
		t.Fatalf("mau 1 grant, dapat %d (%+v)", len(got), got)
	}
	if !got[0].TenantWide || got[0].Permission != "surat_masuk:surat:baca" {
		t.Fatalf("grant salah: %+v", got[0])
	}
	if got[0].UnitKerjaID != uuid.Nil || got[0].Subtree {
		t.Fatalf("grant TenantWide tak boleh membawa unit/subtree: %+v", got[0])
	}
}

// TestCentralGrants_RefTenantDiabaikan: emitter ini HANYA untuk lapis sentral. Ref tenant yang
// kebetulan bernama sama dengan role sentral tidak boleh memungut definisi sentral — itu persis
// eskalasi yang ditutup ADR-019/B8, dan di sini ia akan lolos sebagai grant TenantWide.
func TestCentralGrants_RefTenantDiabaikan(t *testing.T) {
	cat := permission.NewMemoryCatalog().
		Define("platform_helpdesk", permission.LayerGlobal, "gov:audit:baca")

	if got := permission.CentralGrants(cat, []permission.RoleRef{
		permission.TenantRef("platform_helpdesk"), // nama sentral, datang dari klaim TENANT
	}); len(got) != 0 {
		t.Fatalf("ref tenant tidak boleh menghasilkan grant sentral: %+v", got)
	}
}

// TestCentralGrants_RoleTakDikenal_TanpaGrant: nama yang tak ada di katalog tidak memberi apa pun
// (fail-closed), sama seperti perlakuan Engine.
func TestCentralGrants_RoleTakDikenal_TanpaGrant(t *testing.T) {
	if got := permission.CentralGrants(centralCatalog(), []permission.RoleRef{
		permission.CentralRef("role_yang_tak_pernah_ada"),
	}); len(got) != 0 {
		t.Fatalf("role tak dikenal tidak boleh memberi grant: %+v", got)
	}
}

// TestCentralGrants_PermissionGandaDidedup: dua role sentral yang memberi permission sama cukup
// satu grant — duplikat hanya memperpanjang loop covers() di setiap pengecekan.
func TestCentralGrants_PermissionGandaDidedup(t *testing.T) {
	got := permission.CentralGrants(centralCatalog(), []permission.RoleRef{
		permission.CentralRef("platform_helpdesk"),
		permission.CentralRef("platform_auditor"),
	})
	if len(got) != 2 {
		t.Fatalf("mau 2 grant unik, dapat %d (%+v)", len(got), got)
	}
}

// TestBuildAuthority_GabungTigaSumber: Roles apa adanya (strict-intersection butuh SEMUA ref),
// RoleGrants = sentral ∪ tenant, DelegatedGrants terpisah (jalur mandiri).
func TestBuildAuthority_GabungTigaSumber(t *testing.T) {
	userID := uuid.New()
	unit := uuid.New()
	refs := []permission.RoleRef{
		permission.CentralRef("platform_helpdesk"),
		permission.TenantRef("operator_surat"),
	}
	roleGrants := &stubGrants{grants: []permission.Grant{
		{Permission: "surat_masuk:surat:buat", UnitKerjaID: unit},
	}}
	delegated := &stubGrants{grants: []permission.Grant{
		{Permission: "keuangan:spm:terbitkan", UnitKerjaID: unit, Subtree: true},
	}}

	auth, err := permission.BuildAuthority(context.Background(), centralCatalog(), roleGrants, delegated, userID, refs)
	if err != nil {
		t.Fatalf("BuildAuthority: %v", err)
	}
	if len(auth.Roles) != 2 {
		t.Fatalf("Roles harus utuh apa adanya (strict-intersection), dapat %+v", auth.Roles)
	}
	if len(auth.RoleGrants) != 2 {
		t.Fatalf("RoleGrants mau 2 (sentral ∪ tenant), dapat %+v", auth.RoleGrants)
	}
	if len(auth.DelegatedGrants) != 1 {
		t.Fatalf("DelegatedGrants mau 1 dan TERPISAH dari RoleGrants, dapat %+v", auth.DelegatedGrants)
	}
	for _, calls := range [][]uuid.UUID{roleGrants.calls, delegated.calls} {
		if len(calls) != 1 || calls[0] != userID {
			t.Fatalf("resolver harus ditanya untuk user dari klaim, dapat %v", calls)
		}
	}
}

// TestBuildAuthority_ResolverGagal_Error: jangkauan yang tak bisa dibaca WAJIB jadi error.
// Authority bolong tidak terasa seperti kegagalan — ia terasa seperti "tidak berwenang" bagi
// orang yang sebenarnya berwenang, dan menyembunyikan DB tenant yang tak terjangkau.
func TestBuildAuthority_ResolverGagal_Error(t *testing.T) {
	boom := errors.New("tenant db mati")
	for name, tc := range map[string]struct{ role, deleg *stubGrants }{
		"resolver role gagal":     {role: &stubGrants{err: boom}, deleg: &stubGrants{}},
		"resolver delegasi gagal": {role: &stubGrants{}, deleg: &stubGrants{err: boom}},
	} {
		t.Run(name, func(t *testing.T) {
			auth, err := permission.BuildAuthority(context.Background(), centralCatalog(),
				tc.role, tc.deleg, uuid.New(), []permission.RoleRef{permission.CentralRef("platform_helpdesk")})
			if !errors.Is(err, boom) {
				t.Fatalf("mau error resolver, dapat %v", err)
			}
			if len(auth.Roles) != 0 || len(auth.RoleGrants) != 0 || len(auth.DelegatedGrants) != 0 {
				t.Fatalf("Authority separuh terisi tak boleh dikembalikan bersama error: %+v", auth)
			}
		})
	}
}

// TestBuildAuthority_TanpaResolver_KosongBukanPermisif: konteks tanpa tenant (citizen) tak punya
// resolver. Hasilnya Authority tanpa grant — dan itu harus berarti DITOLAK, bukan diloloskan.
func TestBuildAuthority_TanpaResolver_KosongBukanPermisif(t *testing.T) {
	auth, err := permission.BuildAuthority(context.Background(), nil, nil, nil, uuid.New(), nil)
	if err != nil {
		t.Fatalf("BuildAuthority: %v", err)
	}
	engine := permission.NewScopedEngine(permission.NewEngine(centralCatalog()), stubTree{})
	ok, err := engine.AllowsInUnit(context.Background(), auth, "surat_masuk:surat:baca",
		permission.ResourceScope{UnitKerjaID: uuid.New()})
	if err != nil {
		t.Fatalf("AllowsInUnit: %v", err)
	}
	if ok {
		t.Fatal("Authority tanpa grant harus MENOLAK; permisif di sini = seluruh scope ABAC bocor")
	}
}

// stubTree = Hierarchy yang tak pernah menemukan keturunan.
type stubTree struct{}

func (stubTree) IsWithin(context.Context, uuid.UUID, uuid.UUID) (bool, error) { return false, nil }
