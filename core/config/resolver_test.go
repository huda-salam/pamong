package config_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core/config"
)

const tenant = "pemkot-surabaya"

func uuidPtr(u uuid.UUID) *uuid.UUID { return &u }

// newResolver menyiapkan resolver berbasis MemoryTenantConfigStore yang sudah diisi entry.
func newResolver(t *testing.T, entries ...config.ConfigEntry) *config.Resolver {
	t.Helper()
	store := config.NewMemoryTenantConfigStore()
	for _, e := range entries {
		if err := store.Set(context.Background(), e); err != nil {
			t.Fatalf("seed entry %+v: %v", e.Scope, err)
		}
	}
	return config.NewResolver(store)
}

func mustResolve(t *testing.T, r *config.Resolver, scope config.ConfigScope, key string) (string, bool) {
	t.Helper()
	v, ok, err := r.Resolve(context.Background(), scope, key)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	return v, ok
}

func TestResolve_TenantLevelOnly(t *testing.T) {
	r := newResolver(t, config.ConfigEntry{
		Scope: config.ConfigScope{TenantID: tenant},
		Key:   "keuangan.persediaan", Value: "fifo",
	})

	v, ok := mustResolve(t, r, config.ConfigScope{TenantID: tenant}, "keuangan.persediaan")
	if !ok || v != "fifo" {
		t.Fatalf("want fifo/true, got %q/%v", v, ok)
	}
}

func TestResolve_NoMatch(t *testing.T) {
	r := newResolver(t)
	if _, ok := mustResolve(t, r, config.ConfigScope{TenantID: tenant}, "tak.ada"); ok {
		t.Fatal("want ok=false untuk key tak terdaftar")
	}
}

// DoD PR-3.3.2: scope unit kerja meng-override level tenant (paling spesifik menang).
func TestResolve_UnitOverridesTenant(t *testing.T) {
	unit := uuid.New()
	r := newResolver(t,
		config.ConfigEntry{
			Scope: config.ConfigScope{TenantID: tenant},
			Key:   "keuangan.persediaan", Value: "fifo",
		},
		config.ConfigEntry{
			Scope: config.ConfigScope{TenantID: tenant, UnitKerjaID: uuidPtr(unit)},
			Key:   "keuangan.persediaan", Value: "average",
		},
	)

	// Query pada unit tsb → nilai unit-level menang.
	if v, ok := mustResolve(t, r,
		config.ConfigScope{TenantID: tenant, UnitKerjaID: uuidPtr(unit)},
		"keuangan.persediaan"); !ok || v != "average" {
		t.Fatalf("unit scope: want average, got %q/%v", v, ok)
	}

	// Query pada unit LAIN → jatuh ke tenant-level.
	other := uuid.New()
	if v, ok := mustResolve(t, r,
		config.ConfigScope{TenantID: tenant, UnitKerjaID: uuidPtr(other)},
		"keuangan.persediaan"); !ok || v != "fifo" {
		t.Fatalf("unit lain: want fifo (fallback tenant), got %q/%v", v, ok)
	}

	// Query level tenant (tanpa unit) → tenant-level, bukan unit override.
	if v, ok := mustResolve(t, r,
		config.ConfigScope{TenantID: tenant}, "keuangan.persediaan"); !ok || v != "fifo" {
		t.Fatalf("tenant scope: want fifo, got %q/%v", v, ok)
	}
}

func TestResolve_ResourceOverridesUnitAndTenant(t *testing.T) {
	unit := uuid.New()
	res := uuid.New()
	r := newResolver(t,
		config.ConfigEntry{
			Scope: config.ConfigScope{TenantID: tenant},
			Key:   "aset.penyusutan", Value: "garis_lurus",
		},
		config.ConfigEntry{
			Scope: config.ConfigScope{TenantID: tenant, UnitKerjaID: uuidPtr(unit)},
			Key:   "aset.penyusutan", Value: "saldo_menurun",
		},
		config.ConfigEntry{
			Scope: config.ConfigScope{TenantID: tenant, UnitKerjaID: uuidPtr(unit), ResourceID: uuidPtr(res)},
			Key:   "aset.penyusutan", Value: "unit_produksi",
		},
	)

	// Paling spesifik (resource) menang.
	if v, ok := mustResolve(t, r,
		config.ConfigScope{TenantID: tenant, UnitKerjaID: uuidPtr(unit), ResourceID: uuidPtr(res)},
		"aset.penyusutan"); !ok || v != "unit_produksi" {
		t.Fatalf("resource scope: want unit_produksi, got %q/%v", v, ok)
	}

	// Resource lain di unit sama → jatuh ke unit-level.
	if v, ok := mustResolve(t, r,
		config.ConfigScope{TenantID: tenant, UnitKerjaID: uuidPtr(unit), ResourceID: uuidPtr(uuid.New())},
		"aset.penyusutan"); !ok || v != "saldo_menurun" {
		t.Fatalf("resource lain: want saldo_menurun (fallback unit), got %q/%v", v, ok)
	}
}

func TestResolve_TenantIsolation(t *testing.T) {
	r := newResolver(t, config.ConfigEntry{
		Scope: config.ConfigScope{TenantID: tenant},
		Key:   "keuangan.persediaan", Value: "fifo",
	})
	if _, ok := mustResolve(t, r,
		config.ConfigScope{TenantID: "pemkot-malang"}, "keuangan.persediaan"); ok {
		t.Fatal("tenant lain tidak boleh melihat config tenant ini")
	}
}

// Set bersifat append-only ber-versi (PR-3.3.3): dua Set pada scope yang sama menghasilkan
// dua versi; Resolve (now) mengembalikan yang terbaru.
func TestSet_AppendsVersions(t *testing.T) {
	store := config.NewMemoryTenantConfigStore()
	r := config.NewResolver(store)
	scope := config.ConfigScope{TenantID: tenant}

	if err := r.Set(context.Background(), config.ConfigEntry{Scope: scope, Key: "k", Value: "fifo"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Set(context.Background(), config.ConfigEntry{Scope: scope, Key: "k", Value: "average"}); err != nil {
		t.Fatal(err)
	}
	if v, ok := mustResolve(t, r, scope, "k"); !ok || v != "average" {
		t.Fatalf("resolve now: want average (versi terbaru), got %q/%v", v, ok)
	}
	// Kedua versi tersimpan (append-only), dengan nomor versi 1 & 2.
	cands, _ := store.Candidates(context.Background(), tenant, "k")
	if len(cands) != 2 {
		t.Fatalf("want 2 versi setelah 2 Set, got %d", len(cands))
	}
	seen := map[int]bool{}
	for _, c := range cands {
		seen[c.Version] = true
	}
	if !seen[1] || !seen[2] {
		t.Fatalf("want versi 1 & 2, got %v", seen)
	}
}

// DoD PR-3.3.3: ganti metode → periode lama tetap pakai metode lama, periode baru pakai baru.
func TestResolveAsOf_NonRetroactive(t *testing.T) {
	store := config.NewMemoryTenantConfigStore()
	r := config.NewResolver(store)
	scope := config.ConfigScope{TenantID: tenant}

	jan2025 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	jan2026 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	if err := r.Set(context.Background(), config.ConfigEntry{
		Scope: scope, Key: "keuangan.persediaan", Value: "fifo", EffectiveFrom: jan2025,
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.Set(context.Background(), config.ConfigEntry{
		Scope: scope, Key: "keuangan.persediaan", Value: "average", EffectiveFrom: jan2026,
	}); err != nil {
		t.Fatal(err)
	}

	asOf := func(ts time.Time) string {
		v, ok, err := r.ResolveAsOf(context.Background(), scope, "keuangan.persediaan", ts)
		if err != nil || !ok {
			t.Fatalf("ResolveAsOf %v: ok=%v err=%v", ts, ok, err)
		}
		return v
	}
	// Periode 2025 (terkunci) tetap fifo meski pilihan sudah diganti.
	if v := asOf(time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)); v != "fifo" {
		t.Errorf("2025: want fifo, got %q", v)
	}
	// Periode 2026 pakai metode baru.
	if v := asOf(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)); v != "average" {
		t.Errorf("2026: want average, got %q", v)
	}
	// Sebelum versi pertama berlaku → tidak ada nilai.
	if _, ok, _ := r.ResolveAsOf(context.Background(), scope, "keuangan.persediaan",
		time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)); ok {
		t.Error("2024: seharusnya belum ada pilihan yang berlaku")
	}
}

// Spesifisitas scope mengalahkan kebaruan: override unit-kerja menang atas pilihan tenant
// yang lebih baru.
func TestResolveAsOf_SpecificityBeatsRecency(t *testing.T) {
	store := config.NewMemoryTenantConfigStore()
	r := config.NewResolver(store)
	unit := uuid.New()

	old := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// Unit-level lama, tenant-level lebih baru.
	_ = r.Set(context.Background(), config.ConfigEntry{
		Scope: config.ConfigScope{TenantID: tenant, UnitKerjaID: uuidPtr(unit)},
		Key:   "k", Value: "unit_pick", EffectiveFrom: old,
	})
	_ = r.Set(context.Background(), config.ConfigEntry{
		Scope: config.ConfigScope{TenantID: tenant},
		Key:   "k", Value: "tenant_pick", EffectiveFrom: newer,
	})

	// Query pada unit, asOf setelah keduanya berlaku → unit menang meski tenant lebih baru.
	v, ok, err := r.ResolveAsOf(context.Background(),
		config.ConfigScope{TenantID: tenant, UnitKerjaID: uuidPtr(unit)}, "k",
		time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))
	if err != nil || !ok || v != "unit_pick" {
		t.Fatalf("want unit_pick, got %q ok=%v err=%v", v, ok, err)
	}
}

func TestSet_RejectsInvalidEntry(t *testing.T) {
	store := config.NewMemoryTenantConfigStore()
	unit := uuid.New()
	cases := map[string]config.ConfigEntry{
		"tenant kosong":       {Scope: config.ConfigScope{}, Key: "k", Value: "v"},
		"key kosong":          {Scope: config.ConfigScope{TenantID: tenant}, Key: "", Value: "v"},
		"resource tanpa unit": {Scope: config.ConfigScope{TenantID: tenant, ResourceID: uuidPtr(unit)}, Key: "k", Value: "v"},
	}
	for name, e := range cases {
		if err := store.Set(context.Background(), e); err == nil {
			t.Errorf("%s: want error, got nil", name)
		}
	}
}
