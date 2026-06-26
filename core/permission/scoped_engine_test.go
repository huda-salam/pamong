package permission_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core/permission"
)

// permSpmStrict ditandai strict pada Engine di test ini (lokal — di produksi dari konstanta modul).
const permSpmStrict = "keuangan:spm:terbitkan"

// fakeHierarchy: peta anak→induk untuk uji subtree tanpa DB. Menelusuri ke atas dari unit.
type fakeHierarchy struct {
	parent map[uuid.UUID]uuid.UUID
}

func (h fakeHierarchy) IsWithin(_ context.Context, root, unit uuid.UUID) (bool, error) {
	for u := unit; ; {
		if u == root {
			return true, nil
		}
		p, ok := h.parent[u]
		if !ok {
			return false, nil
		}
		u = p
	}
}

func newScopedEngine(tree permission.Hierarchy, strict ...string) *permission.ScopedEngine {
	cat := permission.NewMemoryCatalog().
		// operator memberi baca (non-strict); verifikator memberi baca + spm-strict;
		// super_admin global memberi keduanya.
		Define("operator", permission.LayerTenant, permSuratBaca).
		Define("verifikator", permission.LayerTenant, permSuratBaca, permSpmStrict).
		Define("super_admin", permission.LayerGlobal, permSuratBaca, permSpmStrict)
	return permission.NewScopedEngine(permission.NewEngine(cat, strict...), tree)
}

func mustAllow(t *testing.T, eng *permission.ScopedEngine, auth permission.Authority, perm string, unit uuid.UUID, want bool) {
	t.Helper()
	got, err := eng.AllowsInUnit(context.Background(), auth, perm, permission.ResourceScope{UnitKerjaID: unit})
	if err != nil {
		t.Fatalf("AllowsInUnit(%q,%s): error tak terduga: %v", perm, unit, err)
	}
	if got != want {
		t.Errorf("AllowsInUnit(%q, unit=%s) = %v, mau %v", perm, unit, got, want)
	}
}

// Tahap 2 — TenantWide menjangkau unit mana pun.
func TestAllowsInUnit_TenantWide(t *testing.T) {
	eng := newScopedEngine(fakeHierarchy{})
	bpkad, dinas := uuid.New(), uuid.New()
	auth := permission.Authority{
		RoleNames:  []string{"operator"},
		RoleGrants: []permission.Grant{{Permission: permSuratBaca, TenantWide: true}},
	}
	mustAllow(t, eng, auth, permSuratBaca, bpkad, true)
	mustAllow(t, eng, auth, permSuratBaca, dinas, true)
}

// Tahap 2 — grant terikat unit hanya menutupi unit itu.
func TestAllowsInUnit_UnitMatch(t *testing.T) {
	eng := newScopedEngine(fakeHierarchy{})
	bpkad, dinkes := uuid.New(), uuid.New()
	auth := permission.Authority{
		RoleNames:  []string{"operator"},
		RoleGrants: []permission.Grant{{Permission: permSuratBaca, UnitKerjaID: bpkad}},
	}
	mustAllow(t, eng, auth, permSuratBaca, bpkad, true)
	mustAllow(t, eng, auth, permSuratBaca, dinkes, false)
}

// Tahap 2 — Subtree menjangkau keturunan pada hierarki OPD; tanpa Subtree tidak.
func TestAllowsInUnit_Subtree(t *testing.T) {
	dinas, bidang := uuid.New(), uuid.New()
	tree := fakeHierarchy{parent: map[uuid.UUID]uuid.UUID{bidang: dinas}}
	eng := newScopedEngine(tree)

	withSubtree := permission.Authority{
		RoleNames:  []string{"operator"},
		RoleGrants: []permission.Grant{{Permission: permSuratBaca, UnitKerjaID: dinas, Subtree: true}},
	}
	mustAllow(t, eng, withSubtree, permSuratBaca, bidang, true) // keturunan
	mustAllow(t, eng, withSubtree, permSuratBaca, dinas, true)  // diri sendiri

	noSubtree := permission.Authority{
		RoleNames:  []string{"operator"},
		RoleGrants: []permission.Grant{{Permission: permSuratBaca, UnitKerjaID: dinas, Subtree: false}},
	}
	mustAllow(t, eng, noSubtree, permSuratBaca, bidang, false) // tak mewaris ke bawah
}

// Tahap 1 menggerbangi: tanpa role yang memberi perm (RBAC), scope apa pun tak menyelamatkan.
func TestAllowsInUnit_RBACGate(t *testing.T) {
	eng := newScopedEngine(fakeHierarchy{})
	bpkad := uuid.New()
	auth := permission.Authority{
		RoleNames:  nil, // tak ada role → Engine.Allows false
		RoleGrants: []permission.Grant{{Permission: permSuratBaca, TenantWide: true}},
	}
	mustAllow(t, eng, auth, permSuratBaca, bpkad, false)
}

// Tahap 1 strict-intersection: role yang tak sepakat memblokir, meski scope menutupi.
func TestAllowsInUnit_StrictDeny(t *testing.T) {
	eng := newScopedEngine(fakeHierarchy{}, permSpmStrict)
	bpkad := uuid.New()
	auth := permission.Authority{
		RoleNames: []string{"operator", "verifikator"}, // operator TAK memberi spm-strict
		RoleGrants: []permission.Grant{
			{Permission: permSpmStrict, TenantWide: true}, // dari verifikator
		},
	}
	mustAllow(t, eng, auth, permSpmStrict, bpkad, false)
}

// Global menang: role global mengizinkan perm strict, dan grant TenantWide-nya menutupi unit.
func TestAllowsInUnit_GlobalBypass(t *testing.T) {
	eng := newScopedEngine(fakeHierarchy{}, permSpmStrict)
	bpkad := uuid.New()
	auth := permission.Authority{
		RoleNames:  []string{"operator", "super_admin"},
		RoleGrants: []permission.Grant{{Permission: permSpmStrict, TenantWide: true}},
	}
	mustAllow(t, eng, auth, permSpmStrict, bpkad, true)
}

// Delegasi = jalur MANDIRI: delegatee tanpa role yang memberi perm tetap boleh, dalam scope
// delegasi; di luar scope ditolak.
func TestAllowsInUnit_DelegatedGrant(t *testing.T) {
	eng := newScopedEngine(fakeHierarchy{})
	bpkad, dinkes := uuid.New(), uuid.New()
	auth := permission.Authority{
		RoleNames:       nil, // tak punya role apa pun
		DelegatedGrants: []permission.Grant{{Permission: permSuratBaca, UnitKerjaID: bpkad}},
	}
	mustAllow(t, eng, auth, permSuratBaca, bpkad, true)
	mustAllow(t, eng, auth, permSuratBaca, dinkes, false)
}
