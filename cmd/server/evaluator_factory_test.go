package main

import (
	"context"
	"errors"
	"testing"

	"github.com/huda-salam/pamong/core/permission"
	"github.com/huda-salam/pamong/port"
)

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
	f := newEvaluatorFactoryWith(buildCentral(), func(context.Context, string) (permission.RoleCatalog, error) {
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
	f := newEvaluatorFactoryWith(buildCentral(), func(context.Context, string) (permission.RoleCatalog, error) {
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
	f := newEvaluatorFactoryWith(buildCentral(), func(context.Context, string) (permission.RoleCatalog, error) {
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
	f := newEvaluatorFactoryWith(buildCentral(), func(context.Context, string) (permission.RoleCatalog, error) {
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
