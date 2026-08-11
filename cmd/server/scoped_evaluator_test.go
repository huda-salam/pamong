package main

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/huda-salam/pamong/core/permission"
	"github.com/huda-salam/pamong/port"
)

// stubGrantResolver mencatat berapa kali ia ditanya — itulah yang membuktikan perakitan Authority
// benar-benar LAZY dan ter-memo, bukan sekadar "hasilnya benar".
type stubGrantResolver struct {
	grants []permission.Grant
	err    error
	calls  atomic.Int32
}

func (s *stubGrantResolver) Grants(context.Context, uuid.UUID) ([]permission.Grant, error) {
	s.calls.Add(1)
	if s.err != nil {
		return nil, s.err
	}
	return s.grants, nil
}

type stubHierarchy struct{ within bool }

func (h stubHierarchy) IsWithin(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return h.within, nil
}

// newScopedTestFactory merakit factory dengan bahan ABAC ter-stub (tanpa DB).
func newScopedTestFactory(t *testing.T, deps scopedDeps, buildErr error) *evaluatorFactory {
	t.Helper()
	f := newEvaluatorFactoryWith(buildCentral(), func(context.Context, string) (permission.RoleCatalog, error) {
		return buildTenantCat(), nil
	}, nil, 0)
	f.scopedDeps = func(context.Context, string) (scopedDeps, error) {
		if buildErr != nil {
			return scopedDeps{}, buildErr
		}
		return deps, nil
	}
	return f
}

func employeeClaims() *port.Claims {
	return &port.Claims{
		PersonID: uuid.New(), Persona: "employee", TenantID: "pemkot-a",
		TenantRoles: []string{"operator"},
	}
}

// TestBuildScoped_UnitDalamJangkauan_Lolos & _DiluarJangkauan_Ditolak adalah inti lapis ini:
// keputusan HARUS bergantung pada unit sasaran, bukan hanya pada kepemilikan permission.
func TestBuildScoped_JangkauanUnitMenentukan(t *testing.T) {
	unitSaya, unitLain := uuid.New(), uuid.New()
	roleGrants := &stubGrantResolver{grants: []permission.Grant{
		{Permission: "x:y:buat", UnitKerjaID: unitSaya},
	}}
	f := newScopedTestFactory(t, scopedDeps{
		roleGrants: roleGrants, delegated: &stubGrantResolver{}, tree: stubHierarchy{},
	}, nil)

	_, scoped, err := f.Build(context.Background(), employeeClaims())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if ok, err := scoped.AllowsInUnit(context.Background(), "x:y:buat", unitSaya); err != nil || !ok {
		t.Fatalf("unit sendiri: mau (true, nil), dapat (%v, %v)", ok, err)
	}
	if ok, err := scoped.AllowsInUnit(context.Background(), "x:y:buat", unitLain); err != nil || ok {
		t.Fatalf("unit orang lain: mau (false, nil), dapat (%v, %v)", ok, err)
	}
}

// TestBuildScoped_Lazy_TidakMerakitSebelumDipakai: mayoritas request tak pernah memanggil
// RequirePermissionInUnit. Merakit Authority eager berarti DUA query tenant DB dibebankan ke
// SETIAP request untuk kemampuan yang hanya dipakai sebagian — tepat di jalur terpanas.
func TestBuildScoped_Lazy_TidakMerakitSebelumDipakai(t *testing.T) {
	roleGrants := &stubGrantResolver{}
	f := newScopedTestFactory(t, scopedDeps{
		roleGrants: roleGrants, delegated: &stubGrantResolver{}, tree: stubHierarchy{},
	}, nil)

	if _, _, err := f.Build(context.Background(), employeeClaims()); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if n := roleGrants.calls.Load(); n != 0 {
		t.Fatalf("resolver ditanya %d kali sebelum ada pengecekan unit — perakitan tidak lazy", n)
	}
}

// TestBuildScoped_HasilDiMemo: dua pengecekan dalam satu request tak boleh menghasilkan dua pasang
// query, dan tak boleh berbeda jawabannya di tengah request.
func TestBuildScoped_HasilDiMemo(t *testing.T) {
	unit := uuid.New()
	roleGrants := &stubGrantResolver{grants: []permission.Grant{{Permission: "x:y:buat", UnitKerjaID: unit}}}
	f := newScopedTestFactory(t, scopedDeps{
		roleGrants: roleGrants, delegated: &stubGrantResolver{}, tree: stubHierarchy{},
	}, nil)

	_, scoped, err := f.Build(context.Background(), employeeClaims())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	for i := 0; i < 5; i++ {
		if _, err := scoped.AllowsInUnit(context.Background(), "x:y:buat", unit); err != nil {
			t.Fatalf("AllowsInUnit #%d: %v", i, err)
		}
	}
	if n := roleGrants.calls.Load(); n != 1 {
		t.Fatalf("resolver ditanya %d kali untuk 5 pengecekan — hasil tidak di-memo", n)
	}
}

// TestBuildScoped_MemoAmanKonkuren: handler boleh fan-out ke goroutine; perakitan tetap sekali.
func TestBuildScoped_MemoAmanKonkuren(t *testing.T) {
	unit := uuid.New()
	roleGrants := &stubGrantResolver{grants: []permission.Grant{{Permission: "x:y:buat", UnitKerjaID: unit}}}
	f := newScopedTestFactory(t, scopedDeps{
		roleGrants: roleGrants, delegated: &stubGrantResolver{}, tree: stubHierarchy{},
	}, nil)

	_, scoped, err := f.Build(context.Background(), employeeClaims())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = scoped.AllowsInUnit(context.Background(), "x:y:buat", unit)
		}()
	}
	wg.Wait()
	if n := roleGrants.calls.Load(); n != 1 {
		t.Fatalf("resolver ditanya %d kali dari 16 goroutine — perakitan tidak ter-serialisasi", n)
	}
}

// TestBuildScoped_ResolverGagal_ErrorBukanIzin: DB tenant tak terjangkau harus menjadi ERROR
// (503/500 di gateway), bukan false yang menyamar sebagai "tidak berwenang" — dan sama sekali
// bukan true. Errornya juga harus tetap sama pada pengecekan berikutnya (memo ikut menyimpan
// kegagalan) supaya satu request tak menjawab dua hal berbeda.
func TestBuildScoped_ResolverGagal_ErrorBukanIzin(t *testing.T) {
	boom := errors.New("tenant db mati")
	f := newScopedTestFactory(t, scopedDeps{}, boom)

	_, scoped, err := f.Build(context.Background(), employeeClaims())
	if err != nil {
		t.Fatalf("Build tak boleh gagal (perakitan ditunda): %v", err)
	}
	for i := 0; i < 2; i++ {
		ok, err := scoped.AllowsInUnit(context.Background(), "x:y:buat", uuid.New())
		if ok {
			t.Fatal("kegagalan resolver meloloskan pengecekan — fail-open di lapis otorisasi")
		}
		if !errors.Is(err, boom) {
			t.Fatalf("pengecekan #%d: mau error resolver, dapat %v", i, err)
		}
	}
}

// TestBuildScoped_TanpaTenant_MenolakBukanPermisif: token tanpa tenant (citizen, atau token
// sementara sebelum pemilihan tenant) TETAP mendapat evaluator — terikat Authority KOSONG.
//
// Kalau ia dikembalikan nil, gateway.Context memaknainya PERMISIF, dan seluruh scope ABAC terbuka
// untuk konteks yang justru paling sedikit terverifikasi.
func TestBuildScoped_TanpaTenant_MenolakBukanPermisif(t *testing.T) {
	f := newScopedTestFactory(t, scopedDeps{}, nil)

	_, scoped, err := f.Build(context.Background(), &port.Claims{
		PersonID: uuid.New(), Persona: "citizen", TenantID: "",
		// Sengaja MEMBAWA nama role sentral: kalaupun sebuah token tanpa tenant membawanya,
		// TenantWide tak punya arti tanpa tenant dan tak boleh diemisikan.
		CentralRoles: []string{"super_admin"},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if scoped == nil {
		t.Fatal("konteks tanpa tenant tidak boleh mendapat scoped evaluator nil (nil = PERMISIF)")
	}
	if ok, err := scoped.AllowsInUnit(context.Background(), "x:y:baca", uuid.New()); err != nil || ok {
		t.Fatalf("konteks tanpa tenant: mau (false, nil), dapat (%v, %v)", ok, err)
	}
	// Termasuk pertanyaan "berwenang se-tenant?" (uuid.Nil).
	if ok, _ := scoped.AllowsInUnit(context.Background(), "x:y:baca", uuid.Nil); ok {
		t.Fatal("konteks tanpa tenant menjawab BOLEH untuk wewenang se-tenant")
	}
}

// TestBuildScoped_GrantSentral_TenantWide: role sentral yang lolos ke tenant ini berlaku se-tenant
// — termasuk menjawab pertanyaan wewenang se-tenant (uuid.Nil) yang menjadi dasar containment.
func TestBuildScoped_GrantSentral_TenantWide(t *testing.T) {
	f := newScopedTestFactory(t, scopedDeps{
		roleGrants: &stubGrantResolver{}, delegated: &stubGrantResolver{}, tree: stubHierarchy{},
	}, nil)

	claims := employeeClaims()
	claims.CentralRoles = []string{"super_admin"} // memberi "x:y:baca" (LayerGlobal)

	_, scoped, err := f.Build(context.Background(), claims)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, unit := range []uuid.UUID{uuid.New(), uuid.Nil} {
		if ok, err := scoped.AllowsInUnit(context.Background(), "x:y:baca", unit); err != nil || !ok {
			t.Fatalf("role sentral pada unit %v: mau (true, nil), dapat (%v, %v)", unit, ok, err)
		}
	}
}

// TestBuildScoped_Delegasi_JalurMandiri: delegasi memberi wewenang yang TAK ADA di role — itu inti
// delegasi/PLT, dan karenanya jalurnya tak boleh tunduk pada Tahap 1 RBAC.
func TestBuildScoped_Delegasi_JalurMandiri(t *testing.T) {
	unit := uuid.New()
	f := newScopedTestFactory(t, scopedDeps{
		roleGrants: &stubGrantResolver{},
		delegated: &stubGrantResolver{grants: []permission.Grant{
			{Permission: "keuangan:spm:terbitkan", UnitKerjaID: unit},
		}},
		tree: stubHierarchy{},
	}, nil)

	_, scoped, err := f.Build(context.Background(), employeeClaims())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	// "keuangan:spm:terbitkan" tak diberikan role mana pun di katalog test.
	if ok, err := scoped.AllowsInUnit(context.Background(), "keuangan:spm:terbitkan", unit); err != nil || !ok {
		t.Fatalf("delegasi: mau (true, nil), dapat (%v, %v)", ok, err)
	}
	if ok, _ := scoped.AllowsInUnit(context.Background(), "keuangan:spm:terbitkan", uuid.New()); ok {
		t.Fatal("delegasi tak boleh menjangkau unit di luar jangkauannya")
	}
}

// lookupOnlyCatalog meniru bentuk katalog PRODUKSI (identitydb.CentralRoleCatalog): ia memenuhi
// permission.RoleCatalog dan TIDAK punya LookupRef.
//
// Ia ada untuk mengunci satu cacat yang lolos seluruh test lain: perakitan yang memperoleh
// RefCatalog lewat type-assert atas katalog sentral akan selalu gagal di produksi dan membuat
// CentralGrants tak pernah mengemisikan apa pun — super_admin platform mendapat 403 dari SETIAP
// pengecekan unit. Test yang memakai MemoryCatalog tak bisa menangkapnya (ia pemenuh KEDUA
// kontrak), jadi bentuk katalog produksi harus hadir sebagai tipe tersendiri di sini.
type lookupOnlyCatalog struct{ inner *permission.MemoryCatalog }

var _ permission.RoleCatalog = (*lookupOnlyCatalog)(nil)

func (c *lookupOnlyCatalog) Lookup(name string) (permission.Role, bool) { return c.inner.Lookup(name) }

func TestBuildScoped_KatalogSentralTanpaLookupRef_GrantSentralTetapTerbit(t *testing.T) {
	central := &lookupOnlyCatalog{inner: permission.NewMemoryCatalog().
		Define("super_admin", permission.LayerGlobal, "x:y:baca")}

	f := newEvaluatorFactoryWith(central, func(context.Context, string) (permission.RoleCatalog, error) {
		return buildTenantCat(), nil
	}, nil, 0)
	f.scopedDeps = func(context.Context, string) (scopedDeps, error) {
		return scopedDeps{roleGrants: &stubGrantResolver{}, delegated: &stubGrantResolver{}, tree: stubHierarchy{}}, nil
	}

	claims := employeeClaims()
	claims.CentralRoles = []string{"super_admin"}

	_, scoped, err := f.Build(context.Background(), claims)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if ok, err := scoped.AllowsInUnit(context.Background(), "x:y:baca", uuid.New()); err != nil || !ok {
		t.Fatalf("role sentral dengan katalog produksi (tanpa LookupRef): mau (true, nil), dapat (%v, %v)\n"+
			"false di sini = grant sentral tak diemisikan; admin platform akan 403 di semua unit", ok, err)
	}
}
