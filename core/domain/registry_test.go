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
	perms     domain.PermissionManifest
}

func (m dummyModule) Manifest() domain.Manifest {
	return domain.Manifest{Name: m.name, Version: "1.0.0", DependsOn: m.dependsOn, Permissions: m.perms}
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

// group membungkus satu permission jadi PermissionGroup untuk ringkas di test.
func permGroup(perms ...string) domain.PermissionGroup {
	g := domain.PermissionGroup{Name: "g"}
	for _, p := range perms {
		g.Permissions = append(g.Permissions, domain.PermissionDef{Name: p})
	}
	return g
}

func TestRegistry_ExportImportValid(t *testing.T) {
	r := domain.NewRegistry()
	r.Register(
		dummyModule{name: "kepegawaian", perms: domain.PermissionManifest{
			Groups:  []domain.PermissionGroup{permGroup("kepegawaian:jabatan:baca")},
			Exports: []string{"kepegawaian:jabatan:baca"},
		}},
		dummyModule{name: "surat_masuk", perms: domain.PermissionManifest{
			Groups: []domain.PermissionGroup{permGroup("surat_masuk:surat:buat")},
			Imports: []domain.PermissionImport{
				{From: "kepegawaian", Permission: "kepegawaian:jabatan:baca"},
			},
		}},
	)
	if err := r.Validate(); err != nil {
		t.Fatalf("export/import valid tak boleh error: %v", err)
	}
	exp := r.ExportedPermissions()
	if exp["kepegawaian:jabatan:baca"] != "kepegawaian" {
		t.Errorf("ExportedPermissions tak memuat ekspor kepegawaian: %v", exp)
	}
}

func TestRegistry_ExportTidakDidefinisikan(t *testing.T) {
	r := domain.NewRegistry()
	r.Register(dummyModule{name: "surat_masuk", perms: domain.PermissionManifest{
		Exports: []string{"surat_masuk:surat:baca"}, // tak ada di Groups
	}})
	err := r.Validate()
	if err == nil || !strings.Contains(err.Error(), "tidak didefinisikan di Groups") {
		t.Fatalf("export tanpa definisi harus ditolak, dapat: %v", err)
	}
}

func TestRegistry_ExportPrefixSalah(t *testing.T) {
	r := domain.NewRegistry()
	r.Register(dummyModule{name: "surat_masuk", perms: domain.PermissionManifest{
		Groups:  []domain.PermissionGroup{permGroup("kepegawaian:jabatan:baca")},
		Exports: []string{"kepegawaian:jabatan:baca"}, // bukan milik surat_masuk
	}})
	err := r.Validate()
	if err == nil || !strings.Contains(err.Error(), "bukan miliknya") {
		t.Fatalf("export prefix salah harus ditolak, dapat: %v", err)
	}
}

func TestRegistry_ImportModulTakTerdaftar(t *testing.T) {
	r := domain.NewRegistry()
	r.Register(dummyModule{name: "surat_masuk", perms: domain.PermissionManifest{
		Imports: []domain.PermissionImport{
			{From: "kepegawaian", Permission: "kepegawaian:jabatan:baca"},
		},
	}})
	err := r.Validate()
	if err == nil || !strings.Contains(err.Error(), "tidak terdaftar") {
		t.Fatalf("import dari modul tak terdaftar harus ditolak, dapat: %v", err)
	}
}

func TestRegistry_ImportTidakDiExport(t *testing.T) {
	r := domain.NewRegistry()
	r.Register(
		dummyModule{name: "kepegawaian", perms: domain.PermissionManifest{
			Groups: []domain.PermissionGroup{permGroup("kepegawaian:jabatan:baca")},
			// TIDAK meng-export apa pun
		}},
		dummyModule{name: "surat_masuk", perms: domain.PermissionManifest{
			Imports: []domain.PermissionImport{
				{From: "kepegawaian", Permission: "kepegawaian:jabatan:baca"},
			},
		}},
	)
	err := r.Validate()
	if err == nil || !strings.Contains(err.Error(), "tidak meng-export-nya") {
		t.Fatalf("import permission yang tak di-export harus ditolak, dapat: %v", err)
	}
}

func TestRegistry_ImportPrefixSalah(t *testing.T) {
	r := domain.NewRegistry()
	r.Register(
		dummyModule{name: "kepegawaian"},
		dummyModule{name: "surat_masuk", perms: domain.PermissionManifest{
			Imports: []domain.PermissionImport{
				{From: "kepegawaian", Permission: "aset:barang:baca"}, // prefix != From
			},
		}},
	)
	err := r.Validate()
	if err == nil || !strings.Contains(err.Error(), "prefix permission bukan") {
		t.Fatalf("import prefix salah harus ditolak, dapat: %v", err)
	}
}

func names(mods []domain.Module) []string {
	out := make([]string, len(mods))
	for i, m := range mods {
		out[i] = m.Manifest().Name
	}
	return out
}
