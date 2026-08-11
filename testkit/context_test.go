package testkit_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core/permission"
	"github.com/huda-salam/pamong/testkit"
)

const permUji = "uji:data:ubah"

// fakeTree = hierarki OPD yang tak punya relasi apa pun. Cukup untuk soal di sini: grant
// TenantWide dijawab SEBELUM hierarki disentuh, jadi bila jawabannya bergantung pada tree,
// itu sendiri sudah pertanda salah.
type fakeTree struct{}

func (fakeTree) IsWithin(context.Context, uuid.UUID, uuid.UUID) (bool, error) { return false, nil }

// prodTenantWide membangun evaluator PRODUKSI untuk actor yang memegang satu grant TenantWide —
// pembanding yang menentukan bagi TestContext.
func prodTenantWide(t *testing.T) (allowsUnit, allowsSubtree func(uuid.UUID) bool) {
	t.Helper()
	cat := permission.NewMemoryCatalog().Define("admin", permission.LayerTenant, permUji)
	eval := permission.NewScopedEngine(permission.NewEngine(cat), fakeTree{}).Bind(permission.Authority{
		Roles:      []permission.RoleRef{permission.TenantRef("admin")},
		RoleGrants: []permission.Grant{{Permission: permUji, TenantWide: true}},
	})
	ask := func(f func(context.Context, string, uuid.UUID) (bool, error)) func(uuid.UUID) bool {
		return func(u uuid.UUID) bool {
			ok, err := f(context.Background(), permUji, u)
			if err != nil {
				t.Fatalf("evaluator produksi error: %v", err)
			}
			return ok
		}
	}
	return ask(eval.AllowsInUnit), ask(eval.AllowsSubtree)
}

// TestContext_WewenangSeTenant_SelarasDenganProduksi — WithUnitAuthority(uuid.Nil) menyatakan
// wewenang SE-TENANT, dan di produksi itu berarti grant TenantWide: ia menutupi setiap unit
// KONKRET beserta seluruh keturunannya, bukan hanya "unit bernama nol".
//
// Test ini membandingkan fake dengan evaluator produksi alih-alih hanya memeriksa fake sendiri,
// karena kesalahan yang mungkin di sini justru kesalahan yang tak terasa: fake yang lebih KETAT
// dari produksi meloloskan test yang menegakkan invariant yang tak ada — dan tak ada yang curiga
// pada otorisasi yang "terlalu aman".
func TestContext_WewenangSeTenant_SelarasDenganProduksi(t *testing.T) {
	prodUnit, prodSubtree := prodTenantWide(t)
	ctx := testkit.Ctx(t,
		testkit.WithPermission(permUji),
		testkit.WithUnitAuthority(uuid.Nil), // = wewenang se-tenant
	)

	unit := uuid.New()
	for _, tc := range []struct {
		nama    string
		fakeErr error
		prod    bool
	}{
		{"se-tenant (uuid.Nil)", ctx.RequirePermissionInUnit(permUji, uuid.Nil), prodUnit(uuid.Nil)},
		{"unit konkret", ctx.RequirePermissionInUnit(permUji, unit), prodUnit(unit)},
		{"subtree unit konkret", ctx.RequirePermissionInSubtree(permUji, unit), prodSubtree(unit)},
	} {
		if !tc.prod {
			t.Fatalf("%s: evaluator produksi menolak grant TenantWide — premis test salah", tc.nama)
		}
		if tc.fakeErr != nil {
			t.Errorf("%s: TestContext menolak (%v) padahal produksi mengizinkan — fake lebih ketat "+
				"dari produksi", tc.nama, tc.fakeErr)
		}
	}
}

// TestContext_WewenangSatuUnit_TetapMenolakSelainnya — sisi lain yang menjaga kelonggaran di atas
// tidak menjadi "semua lolos": wewenang atas satu unit KONKRET tetap menolak unit lain, dan tetap
// menolak pertanyaan subtree (WithUnitAuthority saja bukan wewenang atas keturunan, ADR-021).
func TestContext_WewenangSatuUnit_TetapMenolakSelainnya(t *testing.T) {
	unit, lain := uuid.New(), uuid.New()
	ctx := testkit.Ctx(t, testkit.WithPermission(permUji), testkit.WithUnitAuthority(unit))

	if err := ctx.RequirePermissionInUnit(permUji, unit); err != nil {
		t.Fatalf("unit yang disebut harus lolos: %v", err)
	}
	if err := ctx.RequirePermissionInUnit(permUji, lain); err == nil {
		t.Error("unit lain harus ditolak")
	}
	if err := ctx.RequirePermissionInUnit(permUji, uuid.Nil); err == nil {
		t.Error("se-tenant (uuid.Nil) harus ditolak: wewenang satu unit bukan wewenang se-tenant")
	}
	if err := ctx.RequirePermissionInSubtree(permUji, unit); err == nil {
		t.Error("subtree harus ditolak: WithUnitAuthority tak menjangkau keturunan")
	}
}
