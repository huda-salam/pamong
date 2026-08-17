package main

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/huda-salam/pamong/core/domain"
	coreWf "github.com/huda-salam/pamong/core/workflow"
	"github.com/huda-salam/pamong/infra/db"
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
	f := newWorkflowFactory(nil, nil, nil, nil, nil, nil)

	if _, err := f.RuntimeFor(context.Background(), ""); err == nil {
		t.Fatal("tenant kosong diterima; ingin ditolak")
	}
}

// poolProviderPalsu mencatat berapa kali pool diminta dan bisa mengembalikan pool BERBEDA pada
// panggilan berikutnya — mensimulasikan tenant yang dipindah ke server DB lain (Tier 2/3).
type poolProviderPalsu struct {
	urutan  []*db.Pool
	panggil int
}

func (p *poolProviderPalsu) Tenant(context.Context, string) (*db.Pool, error) {
	p.panggil++
	if p.panggil <= len(p.urutan) {
		return p.urutan[p.panggil-1], nil
	}
	return p.urutan[len(p.urutan)-1], nil
}

// Tumpukan workflow harus dirakit dari pool yang di-resolve SAAT ITU, bukan dari pool yang
// dibekukan saat tenant pertama dipakai.
//
// TenantConnManager membaca id.tenant_registry pada setiap panggilan Tenant() dan mengunci pool
// pada (host, nama DB). Jadi ketika tenant dipindah ke server DB sendiri — operasi yang memang
// dirancang berjalan TANPA perubahan kode — semua konsumen lain mengikuti registry ke DB baru.
// Tumpukan yang ter-cache tidak: ia terus menulis instance & transisi ke DB LAMA sampai proses
// di-restart, tanpa satu pun error yang memberi tahu.
func TestWorkflowFactory_PoolDimintaUlangTiapPermintaan(t *testing.T) {
	lama, baru := db.NewPool(nil), db.NewPool(nil)
	prov := &poolProviderPalsu{urutan: []*db.Pool{lama, baru}}
	f := newWorkflowFactory(prov, coreWf.NewActionRegistry(), nil, nil, nil, nil)
	// Tandai kedua DB "sudah disiapkan" supaya test ini murni soal perakitan, tanpa menyentuh DB.
	f.prepared[lama] = struct{}{}
	f.prepared[baru] = struct{}{}

	rt1, err := f.RuntimeFor(context.Background(), "pemkot-surabaya")
	if err != nil {
		t.Fatalf("RuntimeFor pertama: %v", err)
	}
	rt2, err := f.RuntimeFor(context.Background(), "pemkot-surabaya")
	if err != nil {
		t.Fatalf("RuntimeFor kedua: %v", err)
	}

	if prov.panggil != 2 {
		t.Errorf("pool diminta %d kali untuk 2 permintaan; ingin 2 (registry dibaca ulang)", prov.panggil)
	}
	if rt1.Instances == rt2.Instances {
		t.Error("tumpukan yang sama dipakai ulang; tenant yang pindah DB akan terus menulis ke DB lama")
	}
}
