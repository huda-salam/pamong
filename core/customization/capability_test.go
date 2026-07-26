package customization_test

import (
	"context"
	"errors"
	"testing"

	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/core/customization"
)

func ctx() context.Context { return context.Background() }

// newResolver merakit registry + memory store untuk test.
func newResolver(t *testing.T, caps ...customization.Capability) (*customization.CapabilityResolver, *customization.MemoryTenantCapabilityStore) {
	t.Helper()
	reg := customization.NewCapabilityRegistry()
	for _, c := range caps {
		if err := reg.Register(c); err != nil {
			t.Fatalf("Register(%q): %v", c.Name, err)
		}
	}
	store := customization.NewMemoryTenantCapabilityStore()
	return customization.NewCapabilityResolver(reg, store), store
}

func TestRegister_DuplicateDitolak(t *testing.T) {
	reg := customization.NewCapabilityRegistry()
	c := customization.Capability{Name: "keuangan.approval_paralel", Label: "Approval paralel"}
	if err := reg.Register(c); err != nil {
		t.Fatalf("register pertama: %v", err)
	}
	err := reg.Register(c)
	if err == nil {
		t.Fatal("registrasi ganda harus ditolak")
	}
	var fe *core.FrameworkError
	if !errors.As(err, &fe) || fe.Code != "CONFLICT" {
		t.Errorf("harap CONFLICT, dapat %v", err)
	}
}

func TestRegister_NamaInvalidDitolak(t *testing.T) {
	reg := customization.NewCapabilityRegistry()
	for _, name := range []string{"tanpatitik", "keuangan.", ".fitur", "keuangan..fitur", ""} {
		if err := reg.Register(customization.Capability{Name: name}); err == nil {
			t.Errorf("nama %q harus ditolak", name)
		}
	}
}

// TestIsEnabled_DefaultSaatTanpaOverride: fitur dormant (default false) & default-on keduanya
// mengikuti DefaultEnabled bila tenant belum meng-override.
func TestIsEnabled_DefaultSaatTanpaOverride(t *testing.T) {
	res, _ := newResolver(t,
		customization.Capability{Name: "keuangan.approval_paralel", DefaultEnabled: false},
		customization.Capability{Name: "surat.disposisi_massal", DefaultEnabled: true},
	)

	got, err := res.IsEnabled(ctx(), "pemkot-surabaya", "keuangan.approval_paralel")
	if err != nil {
		t.Fatalf("IsEnabled dormant: %v", err)
	}
	if got {
		t.Error("fitur dormant tanpa override harus nonaktif")
	}

	got, err = res.IsEnabled(ctx(), "pemkot-surabaya", "surat.disposisi_massal")
	if err != nil {
		t.Fatalf("IsEnabled default-on: %v", err)
	}
	if !got {
		t.Error("fitur default-on tanpa override harus aktif")
	}
}

// TestIsEnabled_OverrideMenang: override tenant menang atas default (aktifkan dormant &
// nonaktifkan default-on).
func TestIsEnabled_OverrideMenang(t *testing.T) {
	res, store := newResolver(t,
		customization.Capability{Name: "keuangan.approval_paralel", DefaultEnabled: false},
		customization.Capability{Name: "surat.disposisi_massal", DefaultEnabled: true},
	)

	if err := store.Set(ctx(), "pemkot-surabaya", "keuangan.approval_paralel", true, nil); err != nil {
		t.Fatalf("Set aktifkan dormant: %v", err)
	}
	if err := store.Set(ctx(), "pemkot-surabaya", "surat.disposisi_massal", false, nil); err != nil {
		t.Fatalf("Set nonaktifkan default-on: %v", err)
	}

	on, err := res.IsEnabled(ctx(), "pemkot-surabaya", "keuangan.approval_paralel")
	if err != nil || !on {
		t.Errorf("override aktif harus menang: on=%v err=%v", on, err)
	}
	off, err := res.IsEnabled(ctx(), "pemkot-surabaya", "surat.disposisi_massal")
	if err != nil || off {
		t.Errorf("override nonaktif harus menang: off=%v err=%v", off, err)
	}
}

// TestIsEnabled_IsolasiTenant: override satu tenant tak bocor ke tenant lain.
func TestIsEnabled_IsolasiTenant(t *testing.T) {
	res, store := newResolver(t,
		customization.Capability{Name: "keuangan.approval_paralel", DefaultEnabled: false},
	)
	if err := store.Set(ctx(), "pemkot-surabaya", "keuangan.approval_paralel", true, nil); err != nil {
		t.Fatalf("Set: %v", err)
	}

	on, _ := res.IsEnabled(ctx(), "pemkot-surabaya", "keuangan.approval_paralel")
	if !on {
		t.Error("tenant yang di-set harus aktif")
	}
	other, _ := res.IsEnabled(ctx(), "pemkot-malang", "keuangan.approval_paralel")
	if other {
		t.Error("tenant lain harus tetap default (nonaktif) — override tak boleh bocor")
	}
}

// TestIsEnabled_CapabilityTakDikenalFailClosed: capability tak terdaftar → error, bukan false diam.
func TestIsEnabled_CapabilityTakDikenalFailClosed(t *testing.T) {
	res, _ := newResolver(t)
	_, err := res.IsEnabled(ctx(), "pemkot-surabaya", "modul.fitur_hantu")
	if err == nil {
		t.Fatal("capability tak terdaftar harus error (fail-closed)")
	}
	var fe *core.FrameworkError
	if !errors.As(err, &fe) || fe.Code != "NOT_FOUND" {
		t.Errorf("harap NOT_FOUND, dapat %v", err)
	}
}

// TestList_TerurutNama: List deterministik terurut nama.
func TestList_TerurutNama(t *testing.T) {
	reg := customization.NewCapabilityRegistry()
	for _, n := range []string{"surat.b", "keuangan.a", "aset.c"} {
		if err := reg.Register(customization.Capability{Name: n}); err != nil {
			t.Fatalf("register %q: %v", n, err)
		}
	}
	got := reg.List()
	want := []string{"aset.c", "keuangan.a", "surat.b"}
	if len(got) != len(want) {
		t.Fatalf("jumlah %d, harap %d", len(got), len(want))
	}
	for i, c := range got {
		if c.Name != want[i] {
			t.Errorf("List[%d]=%q, harap %q", i, c.Name, want[i])
		}
	}
}

// TestSet_MenimpaOverride: Set kedua menimpa nilai sebelumnya.
func TestSet_MenimpaOverride(t *testing.T) {
	res, store := newResolver(t,
		customization.Capability{Name: "keuangan.approval_paralel", DefaultEnabled: false},
	)
	_ = store.Set(ctx(), "t", "keuangan.approval_paralel", true, nil)
	_ = store.Set(ctx(), "t", "keuangan.approval_paralel", false, nil)
	on, err := res.IsEnabled(ctx(), "t", "keuangan.approval_paralel")
	if err != nil {
		t.Fatalf("IsEnabled: %v", err)
	}
	if on {
		t.Error("Set terakhir (false) harus menang")
	}
}
