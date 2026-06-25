package domain_test

import (
	"context"
	"strings"
	"testing"

	"github.com/huda-salam/pamong/core/domain"
)

// dummyModule adalah modul uji minimal: hanya manifest, bootstrap no-op.
type dummyModule struct {
	name      string
	dependsOn []string
}

func (m dummyModule) Manifest() domain.Manifest {
	return domain.Manifest{Name: m.name, Version: "1.0.0", DependsOn: m.dependsOn}
}
func (m dummyModule) Bootstrap(context.Context, *domain.App) error { return nil }

func TestRegistry_RegisterDanList(t *testing.T) {
	r := domain.NewRegistry()
	r.Register(dummyModule{name: "kepegawaian"}, dummyModule{name: "surat_masuk", dependsOn: []string{"kepegawaian"}})

	if err := r.Validate(); err != nil {
		t.Fatalf("dua modul valid tak boleh error: %v", err)
	}
	mods := r.Modules()
	if len(mods) != 2 {
		t.Fatalf("jumlah modul = %d, mau 2", len(mods))
	}
	// Urutan registrasi dipertahankan.
	if mods[0].Manifest().Name != "kepegawaian" || mods[1].Manifest().Name != "surat_masuk" {
		t.Errorf("urutan modul tidak sesuai registrasi: %v", names(mods))
	}
	if _, ok := r.Get("surat_masuk"); !ok {
		t.Error("Get(surat_masuk) harus ketemu")
	}
}

func TestRegistry_DependencyMenggantung(t *testing.T) {
	r := domain.NewRegistry()
	r.Register(dummyModule{name: "surat_masuk", dependsOn: []string{"kepegawaian"}}) // kepegawaian tak terdaftar

	err := r.Validate()
	if err == nil || !strings.Contains(err.Error(), "tidak terdaftar") {
		t.Fatalf("dependency menggantung harus ditolak, dapat: %v", err)
	}
}

func TestRegistry_Siklus(t *testing.T) {
	r := domain.NewRegistry()
	r.Register(
		dummyModule{name: "a", dependsOn: []string{"b"}},
		dummyModule{name: "b", dependsOn: []string{"a"}},
	)
	err := r.Validate()
	if err == nil || !strings.Contains(err.Error(), "sirkular") {
		t.Fatalf("siklus harus terdeteksi, dapat: %v", err)
	}
}

func TestRegistry_NamaDuplikat(t *testing.T) {
	r := domain.NewRegistry()
	r.Register(dummyModule{name: "x"}, dummyModule{name: "x"})
	err := r.Validate()
	if err == nil || !strings.Contains(err.Error(), "duplikat") {
		t.Fatalf("nama duplikat harus ditolak, dapat: %v", err)
	}
}

func TestRegistry_NamaKosong(t *testing.T) {
	r := domain.NewRegistry()
	r.Register(dummyModule{name: ""})
	err := r.Validate()
	if err == nil || !strings.Contains(err.Error(), "tanpa Name") {
		t.Fatalf("nama kosong harus ditolak, dapat: %v", err)
	}
}

func names(mods []domain.Module) []string {
	out := make([]string, len(mods))
	for i, m := range mods {
		out[i] = m.Manifest().Name
	}
	return out
}
