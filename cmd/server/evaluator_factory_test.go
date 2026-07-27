package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/huda-salam/pamong/core/permission"
	"github.com/huda-salam/pamong/port"
)

// newTestFactory merakit factory tanpa permission strict & tanpa TTL (cache selama umur
// proses) — default untuk test perilaku dasar. Test strict & TTL memakai konstruktor penuh.
func newTestFactory(central permission.RoleCatalog, build tenantCatalogBuilder) *evaluatorFactory {
	return newEvaluatorFactoryWith(central, build, nil, 0)
}

// buildCentral: super_admin (global) memberi "x:y:baca"; regional (scoped) memberi "x:y:lihat".
func buildCentral() permission.RoleCatalog {
	return permission.NewMemoryCatalog().
		Define("super_admin", permission.LayerGlobal, "x:y:baca").
		Define("regional", permission.LayerScoped, "x:y:lihat")
}

// buildTenantCat: role tenant "operator" memberi "x:y:buat".
func buildTenantCat() permission.RoleCatalog {
	return permission.NewMemoryCatalog().
		Define("operator", permission.LayerTenant, "x:y:buat")
}

func TestEvaluatorFactory_Citizen_CentralOnly(t *testing.T) {
	// Citizen (TenantID kosong) → engine central-only; builder tenant TAK boleh dipanggil.
	var builderCalled bool
	f := newTestFactory(buildCentral(), func(context.Context, string) (permission.RoleCatalog, error) {
		builderCalled = true
		return nil, nil
	})

	eval, err := f.Build(context.Background(), &port.Claims{Persona: "citizen", TenantID: ""})
	if err != nil {
		t.Fatalf("Build citizen: %v", err)
	}
	if builderCalled {
		t.Error("citizen tak boleh memicu build catalog tenant")
	}
	// super_admin (global) mengizinkan; role tenant "operator" tak dikenal engine central-only.
	if !eval.Allows([]string{"super_admin"}, "x:y:baca") {
		t.Error("central global harus mengizinkan x:y:baca")
	}
	if eval.Allows([]string{"operator"}, "x:y:buat") {
		t.Error("engine central-only tak boleh mengenal role tenant")
	}
}

func TestEvaluatorFactory_Employee_Composite(t *testing.T) {
	f := newTestFactory(buildCentral(), func(context.Context, string) (permission.RoleCatalog, error) {
		return buildTenantCat(), nil
	})

	eval, err := f.Build(context.Background(), &port.Claims{Persona: "employee", TenantID: "pemkot-a"})
	if err != nil {
		t.Fatalf("Build employee: %v", err)
	}
	// Composite: role tenant DAN role central sama-sama dikenal.
	if !eval.Allows([]string{"operator"}, "x:y:buat") {
		t.Error("composite harus mengenal role tenant operator")
	}
	if !eval.Allows([]string{"super_admin"}, "x:y:baca") {
		t.Error("composite harus tetap mengenal role central global")
	}
}

func TestEvaluatorFactory_TenantCatalogCached(t *testing.T) {
	var buildCount int
	f := newTestFactory(buildCentral(), func(context.Context, string) (permission.RoleCatalog, error) {
		buildCount++
		return buildTenantCat(), nil
	})

	for i := 0; i < 3; i++ {
		if _, err := f.Build(context.Background(), &port.Claims{TenantID: "pemkot-a"}); err != nil {
			t.Fatalf("Build #%d: %v", i, err)
		}
	}
	if buildCount != 1 {
		t.Fatalf("catalog tenant harus di-build sekali lalu di-cache, got %d", buildCount)
	}

	// Tenant berbeda → build lagi (cache per tenant).
	if _, err := f.Build(context.Background(), &port.Claims{TenantID: "pemkot-b"}); err != nil {
		t.Fatalf("Build tenant lain: %v", err)
	}
	if buildCount != 2 {
		t.Fatalf("tenant berbeda harus build terpisah, got %d", buildCount)
	}
}

func TestEvaluatorFactory_BuildError_TidakDicache(t *testing.T) {
	var buildCount int
	f := newTestFactory(buildCentral(), func(context.Context, string) (permission.RoleCatalog, error) {
		buildCount++
		return nil, errors.New("db tenant tak terjangkau")
	})

	if _, err := f.Build(context.Background(), &port.Claims{TenantID: "pemkot-a"}); err == nil {
		t.Fatal("error build harus dipropagasi")
	}
	// Percobaan kedua harus mencoba build lagi (kegagalan tak di-cache).
	if _, err := f.Build(context.Background(), &port.Claims{TenantID: "pemkot-a"}); err == nil {
		t.Fatal("error build kedua harus tetap dipropagasi")
	}
	if buildCount != 2 {
		t.Fatalf("kegagalan tak boleh di-cache (harus retry), build count %d", buildCount)
	}
}

// buildTenantStrict: DUA role tenant. "verifikator" memberi "keu:spm:verifikasi";
// "operator" TIDAK memberinya. Dipakai menguji resolusi strict (intersection).
func buildTenantStrict() permission.RoleCatalog {
	return permission.NewMemoryCatalog().
		Define("verifikator", permission.LayerTenant, "keu:spm:verifikasi").
		Define("operator", permission.LayerTenant, "keu:spm:buat")
}

// TestEvaluatorFactory_StrictPropagated memastikan permission strict yang disuntik saat
// konstruksi factory diteruskan ke Engine yang dibangun: perm strict → INTERSECTION antar
// role non-global (memegang role yang tak memberi perm memblokirnya), sedangkan perm biasa
// tetap union. Menutup jalur wiring 5.1.2c (manifest → factory → Engine).
func TestEvaluatorFactory_StrictPropagated(t *testing.T) {
	strict := []permission.Permission{"keu:spm:verifikasi"}
	f := newEvaluatorFactoryWith(buildCentral(), func(context.Context, string) (permission.RoleCatalog, error) {
		return buildTenantStrict(), nil
	}, strict, 0)

	eval, err := f.Build(context.Background(), &port.Claims{Persona: "employee", TenantID: "pemkot-a"})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Hanya "verifikator" (memberi perm) → intersection terpenuhi → IZIN.
	if !eval.Allows([]string{"verifikator"}, "keu:spm:verifikasi") {
		t.Error("strict: satu-satunya role non-global yang memberi perm harus mengizinkan")
	}
	// Memegang "operator" (tak memberi perm strict) di samping "verifikator" → intersection
	// gagal → TOLAK (segregation of duties).
	if eval.Allows([]string{"verifikator", "operator"}, "keu:spm:verifikasi") {
		t.Error("strict: memegang role yang tak memberi perm strict harus memblokir (intersection)")
	}
	// Perm biasa tetap union: "operator" saja cukup untuk "keu:spm:buat".
	if !eval.Allows([]string{"operator", "verifikator"}, "keu:spm:buat") {
		t.Error("perm biasa harus tetap union walau ada strict lain terdaftar")
	}
}

// TestEvaluatorFactory_StrictKosong_UnionMurni: tanpa strict yang disuntik, semua perm union
// (perilaku pra-5.1.2c) — memegang role tambahan tak memblokir.
func TestEvaluatorFactory_StrictKosong_UnionMurni(t *testing.T) {
	f := newTestFactory(buildCentral(), func(context.Context, string) (permission.RoleCatalog, error) {
		return buildTenantStrict(), nil
	})
	eval, err := f.Build(context.Background(), &port.Claims{Persona: "employee", TenantID: "pemkot-a"})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	// Tanpa strict, "keu:spm:verifikasi" jadi perm biasa → union → satu role memberi sudah cukup.
	if !eval.Allows([]string{"verifikator", "operator"}, "keu:spm:verifikasi") {
		t.Error("tanpa strict, perm harus union (tidak diblokir role lain)")
	}
}

// TestEvaluatorFactory_TTL_RebuildSetelahKedaluwarsa: dengan TTL > 0 dan jam palsu, entri
// cache dibangun ulang setelah TTL lewat (perubahan definisi role tenant terlihat tanpa
// restart). Sebelum TTL lewat, tetap dari cache (tak build ulang).
func TestEvaluatorFactory_TTL_RebuildSetelahKedaluwarsa(t *testing.T) {
	var buildCount int
	f := newEvaluatorFactoryWith(buildCentral(), func(context.Context, string) (permission.RoleCatalog, error) {
		buildCount++
		return buildTenantCat(), nil
	}, nil, 5*time.Minute)

	now := time.Unix(1_700_000_000, 0)
	f.now = func() time.Time { return now }

	// Build pertama → 1x build.
	if _, err := f.Build(context.Background(), &port.Claims{TenantID: "pemkot-a"}); err != nil {
		t.Fatalf("Build #1: %v", err)
	}
	// Dalam TTL (maju 4 menit < 5 menit) → tetap cache, tak build ulang.
	now = now.Add(4 * time.Minute)
	if _, err := f.Build(context.Background(), &port.Claims{TenantID: "pemkot-a"}); err != nil {
		t.Fatalf("Build #2 (dalam TTL): %v", err)
	}
	if buildCount != 1 {
		t.Fatalf("dalam TTL harus dari cache, build count %d", buildCount)
	}
	// Lewat TTL (total 6 menit >= 5 menit) → build ulang.
	now = now.Add(2 * time.Minute)
	if _, err := f.Build(context.Background(), &port.Claims{TenantID: "pemkot-a"}); err != nil {
		t.Fatalf("Build #3 (lewat TTL): %v", err)
	}
	if buildCount != 2 {
		t.Fatalf("setelah TTL lewat harus build ulang, build count %d", buildCount)
	}
}

// TestEvaluatorFactory_TTLNol_CacheSelamanya: TTL 0 → entri tak pernah kedaluwarsa walau
// waktu maju jauh (perilaku pra-refresh dipertahankan sebagai opsi eksplisit).
func TestEvaluatorFactory_TTLNol_CacheSelamanya(t *testing.T) {
	var buildCount int
	f := newEvaluatorFactoryWith(buildCentral(), func(context.Context, string) (permission.RoleCatalog, error) {
		buildCount++
		return buildTenantCat(), nil
	}, nil, 0)

	now := time.Unix(1_700_000_000, 0)
	f.now = func() time.Time { return now }

	if _, err := f.Build(context.Background(), &port.Claims{TenantID: "pemkot-a"}); err != nil {
		t.Fatalf("Build #1: %v", err)
	}
	now = now.Add(1000 * time.Hour) // maju jauh
	if _, err := f.Build(context.Background(), &port.Claims{TenantID: "pemkot-a"}); err != nil {
		t.Fatalf("Build #2: %v", err)
	}
	if buildCount != 1 {
		t.Fatalf("TTL 0 harus cache selamanya, build count %d", buildCount)
	}
}

// TestEvaluatorFactory_TTL_ConcurrentBuild: build konkuren untuk tenant sama tetap aman
// (race detector) dan menghasilkan evaluator yang valid.
func TestEvaluatorFactory_TTL_ConcurrentBuild(t *testing.T) {
	f := newEvaluatorFactoryWith(buildCentral(), func(context.Context, string) (permission.RoleCatalog, error) {
		return buildTenantCat(), nil
	}, nil, time.Minute)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := f.Build(context.Background(), &port.Claims{TenantID: "pemkot-a"}); err != nil {
				t.Errorf("Build konkuren: %v", err)
			}
		}()
	}
	wg.Wait()
}
