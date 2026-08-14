package main

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/huda-salam/pamong/core/domain"
)

// modulPalsu adalah domain.Module minimal untuk menguji pengumpulan seed dari manifest.
type modulPalsu struct {
	nama      string
	workflows []domain.WorkflowRef
}

func (m modulPalsu) Manifest() domain.Manifest {
	return domain.Manifest{Name: m.nama, Version: "1.0.0", Domain: "uji", Workflows: m.workflows}
}

func (modulPalsu) Bootstrap(context.Context, *domain.App) error { return nil }

func registryDengan(t *testing.T, mods ...domain.Module) *domain.Registry {
	t.Helper()
	reg := domain.NewRegistry()
	reg.Register(mods...)
	return reg
}

func TestCollectWorkflowSeeds_MengumpulkanDariSemuaModul(t *testing.T) {
	fsys := fstest.MapFS{"workflows/a.yaml": &fstest.MapFile{Data: []byte("id: a")}}
	reg := registryDengan(t,
		modulPalsu{nama: "modul_a", workflows: []domain.WorkflowRef{{FS: fsys, Path: "workflows/a.yaml"}}},
		modulPalsu{nama: "modul_b", workflows: []domain.WorkflowRef{{FS: fsys, Path: "workflows/b.yaml"}}},
	)

	seeds, err := collectWorkflowSeeds(reg)
	if err != nil {
		t.Fatalf("collectWorkflowSeeds: %v", err)
	}
	if len(seeds) != 2 {
		t.Fatalf("seeds = %d, ingin 2", len(seeds))
	}
}

// WorkflowRef tanpa FS ditolak SAAT BOOT. Kalau dibiarkan lewat, seed hanya bisa dibaca dari
// direktori kerja proses: lulus di mesin developer (yang menjalankan dari root repo), gagal di
// produksi — dan gagalnya baru terlihat saat tenant pertama memakai workflow.
func TestCollectWorkflowSeeds_MenolakRefTanpaFS(t *testing.T) {
	reg := registryDengan(t, modulPalsu{
		nama:      "modul_lupa_embed",
		workflows: []domain.WorkflowRef{{Path: "workflows/a.yaml"}},
	})

	_, err := collectWorkflowSeeds(reg)

	if err == nil {
		t.Fatal("WorkflowRef tanpa FS diterima; ingin ditolak saat boot")
	}
	if !strings.Contains(err.Error(), "modul_lupa_embed") {
		t.Errorf("pesan error tak menyebut modul asal: %v", err)
	}
}

func TestCollectWorkflowSeeds_MenolakRefTanpaPath(t *testing.T) {
	fsys := fstest.MapFS{}
	reg := registryDengan(t, modulPalsu{
		nama:      "modul_lupa_path",
		workflows: []domain.WorkflowRef{{FS: fsys}},
	})

	if _, err := collectWorkflowSeeds(reg); err == nil {
		t.Fatal("WorkflowRef tanpa Path diterima; ingin ditolak saat boot")
	}
}

// Modul tanpa workflow sama sekali bukan kesalahan — mayoritas modul Tier 1 tak punya alur.
func TestCollectWorkflowSeeds_ModulTanpaWorkflow(t *testing.T) {
	reg := registryDengan(t, modulPalsu{nama: "modul_polos"})

	seeds, err := collectWorkflowSeeds(reg)
	if err != nil {
		t.Fatalf("collectWorkflowSeeds: %v", err)
	}
	if len(seeds) != 0 {
		t.Fatalf("seeds = %d, ingin 0", len(seeds))
	}
}

// Tenant kosong ditolak: tumpukan workflow SELALU milik satu tenant, dan membangunnya untuk
// tenant "" akan meminta pool yang tak pernah bisa di-resolve.
func TestWorkflowFactory_TenantKosongDitolak(t *testing.T) {
	f := newWorkflowFactory(nil, nil, nil, nil)

	if _, err := f.RuntimeFor(context.Background(), ""); err == nil {
		t.Fatal("tenant kosong diterima; ingin ditolak")
	}
}
