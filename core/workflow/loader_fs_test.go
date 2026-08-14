package workflow_test

import (
	"errors"
	"testing"
	"testing/fstest"

	"github.com/huda-salam/pamong/core/workflow"
)

const yamlUji = `
id: uji.alur.standar
entity: uji.Entitas
version: 1
initial_state: awal
states:
  awal:
    label: Awal
    actions: [lanjut]
  selesai:
    label: Selesai
    is_terminal: true
transitions:
  - from: awal
    to: selesai
    on: lanjut
`

func TestSeedFS_MendaftarkanDefinisiDariFSTerEmbed(t *testing.T) {
	fsys := fstest.MapFS{"workflows/uji.yaml": &fstest.MapFile{Data: []byte(yamlUji)}}
	store := workflow.NewMemoryStore()

	if err := workflow.SeedFS(fsys, "workflows/uji.yaml", store); err != nil {
		t.Fatalf("SeedFS: %v", err)
	}

	def, err := store.Get("uji.alur.standar")
	if err != nil {
		t.Fatalf("definisi tak terdaftar: %v", err)
	}
	if def.InitialState != "awal" || len(def.Transitions) != 1 {
		t.Fatalf("definisi tak utuh: %+v", def)
	}
}

// Seed idempoten: definisi yang sudah ada di store (mis. sudah di-override tenant) TIDAK
// ditimpa oleh baseline developer.
func TestSeedFS_TidakMenimpaYangSudahAda(t *testing.T) {
	fsys := fstest.MapFS{"workflows/uji.yaml": &fstest.MapFile{Data: []byte(yamlUji)}}
	store := workflow.NewMemoryStore()
	if err := workflow.SeedFS(fsys, "workflows/uji.yaml", store); err != nil {
		t.Fatalf("seed pertama: %v", err)
	}
	before, _ := store.Get("uji.alur.standar")

	if err := workflow.SeedFS(fsys, "workflows/uji.yaml", store); err != nil {
		t.Fatalf("seed kedua: %v", err)
	}

	after, _ := store.Get("uji.alur.standar")
	if after.Version != before.Version {
		t.Fatalf("versi berubah %d→%d; seed ulang seharusnya no-op", before.Version, after.Version)
	}
}

// FS nil = salah wiring manifest (WorkflowRef tanpa go:embed). Harus error eksplisit, bukan
// panic nil-pointer di kedalaman fs.ReadFile.
func TestSeedFS_MenolakFSNil(t *testing.T) {
	if err := workflow.SeedFS(nil, "workflows/uji.yaml", workflow.NewMemoryStore()); err == nil {
		t.Fatal("FS nil diterima; ingin error")
	}
}

func TestSeedFS_FileTakAda(t *testing.T) {
	fsys := fstest.MapFS{}
	if err := workflow.SeedFS(fsys, "workflows/hilang.yaml", workflow.NewMemoryStore()); err == nil {
		t.Fatal("file tak ada diterima; ingin error")
	}
}

// --- Regresi review PR-W4a: seed tak boleh menimpa karena salah membaca kegagalan ---

// storeGagalBaca adalah DefinitionStore (BUKAN DefinitionSeeder) yang Get-nya gagal karena infra,
// bukan karena definisi tak ada. Sengaja bukan seeder agar jalur cadangan SeedYAML yang diuji.
type storeGagalBaca struct {
	registered int
}

func (s *storeGagalBaca) Register(workflow.WorkflowDefinition) error {
	s.registered++
	return nil
}

func (s *storeGagalBaca) Get(string) (workflow.WorkflowDefinition, error) {
	return workflow.WorkflowDefinition{}, errors.New("dial tcp: connection refused")
}

func (s *storeGagalBaca) GetVersion(string, int) (workflow.WorkflowDefinition, error) {
	return workflow.WorkflowDefinition{}, errors.New("dial tcp: connection refused")
}

// Gagal MEMBACA store bukan berarti "definisi belum ada". Kalau disamakan, satu error koneksi
// sesaat saat cold-start tenant akan mendaftarkan ulang baseline developer di atas definisi yang
// sudah dikustomisasi tenant — dan baseline itu menjadi versi TERBARU yang dipakai instance baru.
func TestSeedFS_KegagalanBacaTidakDianggapBelumAda(t *testing.T) {
	fsys := fstest.MapFS{"workflows/uji.yaml": &fstest.MapFile{Data: []byte(yamlUji)}}
	store := &storeGagalBaca{}

	err := workflow.SeedFS(fsys, "workflows/uji.yaml", store)

	if err == nil {
		t.Fatal("kegagalan baca store ditelan; ingin error")
	}
	if store.registered != 0 {
		t.Fatalf("definisi didaftarkan %d kali meski store gagal dibaca", store.registered)
	}
}

// Store ber-DefinitionSeeder memakai jalur atomiknya (satu langkah), bukan Get-lalu-Register.
func TestSeedFS_MemakaiJalurSeederBilaTersedia(t *testing.T) {
	fsys := fstest.MapFS{"workflows/uji.yaml": &fstest.MapFile{Data: []byte(yamlUji)}}
	store := workflow.NewMemoryStore()

	if err := workflow.SeedFS(fsys, "workflows/uji.yaml", store); err != nil {
		t.Fatalf("SeedFS: %v", err)
	}
	// Tenant "mengkustomisasi" definisi jadi versi 2.
	kustom, _ := store.Get("uji.alur.standar")
	kustom.Version = 2
	if err := store.Register(kustom); err != nil {
		t.Fatalf("register versi tenant: %v", err)
	}

	// Seed ulang (cold-start berikutnya) TIDAK boleh menyentuh apa pun.
	if err := workflow.SeedFS(fsys, "workflows/uji.yaml", store); err != nil {
		t.Fatalf("SeedFS ulang: %v", err)
	}

	latest, err := store.Get("uji.alur.standar")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if latest.Version != 2 {
		t.Fatalf("versi terbaru = %d, ingin tetap 2 (kustomisasi tenant tergantikan baseline)", latest.Version)
	}
}
