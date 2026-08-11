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
		Roles:      tr("operator"),
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
		Roles:      tr("operator"),
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
		Roles:      tr("operator"),
		RoleGrants: []permission.Grant{{Permission: permSuratBaca, UnitKerjaID: dinas, Subtree: true}},
	}
	mustAllow(t, eng, withSubtree, permSuratBaca, bidang, true) // keturunan
	mustAllow(t, eng, withSubtree, permSuratBaca, dinas, true)  // diri sendiri

	noSubtree := permission.Authority{
		Roles:      tr("operator"),
		RoleGrants: []permission.Grant{{Permission: permSuratBaca, UnitKerjaID: dinas, Subtree: false}},
	}
	mustAllow(t, eng, noSubtree, permSuratBaca, bidang, false) // tak mewaris ke bawah
}

// Tahap 1 menggerbangi: tanpa role yang memberi perm (RBAC), scope apa pun tak menyelamatkan.
func TestAllowsInUnit_RBACGate(t *testing.T) {
	eng := newScopedEngine(fakeHierarchy{})
	bpkad := uuid.New()
	auth := permission.Authority{
		Roles:      nil, // tak ada role → Engine.Allows false
		RoleGrants: []permission.Grant{{Permission: permSuratBaca, TenantWide: true}},
	}
	mustAllow(t, eng, auth, permSuratBaca, bpkad, false)
}

// Tahap 1 strict-intersection: role yang tak sepakat memblokir, meski scope menutupi.
func TestAllowsInUnit_StrictDeny(t *testing.T) {
	eng := newScopedEngine(fakeHierarchy{}, permSpmStrict)
	bpkad := uuid.New()
	auth := permission.Authority{
		Roles: tr("operator", "verifikator"), // operator TAK memberi spm-strict
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
		Roles:      join(tr("operator"), cr("super_admin")),
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
		Roles:           nil, // tak punya role apa pun
		DelegatedGrants: []permission.Grant{{Permission: permSuratBaca, UnitKerjaID: bpkad}},
	}
	mustAllow(t, eng, auth, permSuratBaca, bpkad, true)
	mustAllow(t, eng, auth, permSuratBaca, dinkes, false)
}

// --- AllowsSubtreeIn (PR-W3b · ADR-021) ---
//
// Pertanyaan "berwenang atas unit ini BESERTA keturunannya" berbeda mendasar dari "berwenang atas
// unit ini". Ia dipakai saat actor MEMBERIKAN jangkauan subtree kepada orang lain: tanpa itu,
// pemegang wewenang atas satu unit saja bisa menerbitkan assignment ber-`include_subtree` dan
// membagikan jangkauan atas turunan yang ia sendiri tak punya.

// TestAllowsSubtree_GrantUnitTanpaSubtree_Ditolak adalah inti aturannya: grant yang terikat PERSIS
// pada sebuah unit menutupi unit itu, tapi TIDAK keturunannya.
func TestAllowsSubtree_GrantUnitTanpaSubtree_Ditolak(t *testing.T) {
	unit := uuid.New()
	auth := permission.Authority{
		Roles:      []permission.RoleRef{permission.TenantRef("admin")},
		RoleGrants: []permission.Grant{{Permission: "iam:tenant_role:assign", UnitKerjaID: unit}},
	}
	eng := permission.NewScopedEngine(
		permission.NewEngine(permission.NewMemoryCatalog().
			Define("admin", permission.LayerTenant, "iam:tenant_role:assign")),
		fakeHierarchy{},
	)

	// Kontrol: unit itu sendiri BOLEH.
	if ok, err := eng.AllowsInUnit(context.Background(), auth, "iam:tenant_role:assign",
		permission.ResourceScope{UnitKerjaID: unit}); err != nil || !ok {
		t.Fatalf("AllowsInUnit pada unit sendiri: mau (true, nil), dapat (%v, %v)", ok, err)
	}
	// Yang diuji: subtree atas unit yang sama harus DITOLAK.
	ok, err := eng.AllowsSubtreeIn(context.Background(), auth, "iam:tenant_role:assign",
		permission.ResourceScope{UnitKerjaID: unit})
	if err != nil {
		t.Fatalf("AllowsSubtreeIn: %v", err)
	}
	if ok {
		t.Fatal("grant tanpa Subtree menjawab BOLEH untuk jangkauan subtree — " +
			"pemegang satu unit bisa membagikan jangkauan seluruh turunannya")
	}
}

// TestAllowsSubtree_GrantBerSubtree_Lolos: unit itu sendiri maupun keturunannya.
func TestAllowsSubtree_GrantBerSubtree_Lolos(t *testing.T) {
	induk, anak := uuid.New(), uuid.New()
	auth := permission.Authority{
		Roles: []permission.RoleRef{permission.TenantRef("admin")},
		RoleGrants: []permission.Grant{
			{Permission: "iam:tenant_role:assign", UnitKerjaID: induk, Subtree: true},
		},
	}
	eng := permission.NewScopedEngine(
		permission.NewEngine(permission.NewMemoryCatalog().
			Define("admin", permission.LayerTenant, "iam:tenant_role:assign")),
		fakeHierarchy{parent: map[uuid.UUID]uuid.UUID{anak: induk}},
	)

	for name, unit := range map[string]uuid.UUID{"unit sendiri": induk, "keturunan": anak} {
		if ok, err := eng.AllowsSubtreeIn(context.Background(), auth, "iam:tenant_role:assign",
			permission.ResourceScope{UnitKerjaID: unit}); err != nil || !ok {
			t.Fatalf("%s: mau (true, nil), dapat (%v, %v)", name, ok, err)
		}
	}
}

// TestAllowsSubtree_TenantWide_Lolos: wewenang se-tenant menjangkau apa pun, termasuk subtree.
func TestAllowsSubtree_TenantWide_Lolos(t *testing.T) {
	auth := permission.Authority{
		Roles:      []permission.RoleRef{permission.CentralRef("super_admin")},
		RoleGrants: []permission.Grant{{Permission: "iam:tenant_role:assign", TenantWide: true}},
	}
	eng := permission.NewScopedEngine(
		permission.NewEngine(permission.NewMemoryCatalog().
			Define("super_admin", permission.LayerGlobal, "iam:tenant_role:assign")),
		fakeHierarchy{},
	)
	if ok, err := eng.AllowsSubtreeIn(context.Background(), auth, "iam:tenant_role:assign",
		permission.ResourceScope{UnitKerjaID: uuid.New()}); err != nil || !ok {
		t.Fatalf("TenantWide: mau (true, nil), dapat (%v, %v)", ok, err)
	}
}

// TestAllowsSubtree_Delegasi_JalurMandiri: delegasi ber-Subtree memang melimpahkan jangkauan
// turunan, dan seperti AllowsInUnit ia tak tunduk pada Tahap 1 RBAC.
func TestAllowsSubtree_Delegasi_JalurMandiri(t *testing.T) {
	unit := uuid.New()
	auth := permission.Authority{
		DelegatedGrants: []permission.Grant{
			{Permission: "keuangan:spm:terbitkan", UnitKerjaID: unit, Subtree: true},
		},
	}
	eng := permission.NewScopedEngine(permission.NewEngine(permission.NewMemoryCatalog()), fakeHierarchy{})

	if ok, err := eng.AllowsSubtreeIn(context.Background(), auth, "keuangan:spm:terbitkan",
		permission.ResourceScope{UnitKerjaID: unit}); err != nil || !ok {
		t.Fatalf("delegasi ber-subtree: mau (true, nil), dapat (%v, %v)", ok, err)
	}
	// Delegasi TANPA subtree tidak boleh menjawab ya.
	auth.DelegatedGrants[0].Subtree = false
	if ok, _ := eng.AllowsSubtreeIn(context.Background(), auth, "keuangan:spm:terbitkan",
		permission.ResourceScope{UnitKerjaID: unit}); ok {
		t.Fatal("delegasi tanpa subtree menjawab BOLEH untuk jangkauan subtree")
	}
}
