package workflow_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/huda-salam/pamong/core/workflow"
)

// yamlDisposisi adalah konten disposisi.yaml yang valid — mencerminkan format
// sesungguhnya di modules/surat_masuk/workflows/disposisi.yaml.
const yamlDisposisi = `
id: surat_masuk.disposisi.standar
entity: surat_masuk.SuratMasuk
version: 1
initial_state: diterima

states:
  diterima:
    label: Diterima agendaris
    actions: [disposisi]
  didisposisi:
    label: Menunggu tindak lanjut
    sla_hours: 72
    escalate_to_role: sekretaris_daerah
    actions: [disposisi_lanjut, selesai]
  selesai:
    label: Selesai
    is_terminal: true

transitions:
  - from: diterima
    to: didisposisi
    on: disposisi
    guards:
      - "actor.has_permission('surat_masuk:surat:disposisi')"
    action: DisposisiSurat
  - from: didisposisi
    to: didisposisi
    on: disposisi_lanjut
    guards:
      - "actor.has_permission('surat_masuk:surat:disposisi')"
    action: DisposisiSurat
  - from: didisposisi
    to: selesai
    on: selesai
    guards:
      - "actor.has_permission('surat_masuk:surat:disposisi')"
    notify:
      to_role: agendaris
      template: surat_selesai
`

// ===== ParseYAML =====

func TestParseYAML_DisposisiValid_TermuatSempurna(t *testing.T) {
	def, err := workflow.ParseYAML([]byte(yamlDisposisi))
	if err != nil {
		t.Fatalf("ParseYAML: %v", err)
	}

	if def.ID != "surat_masuk.disposisi.standar" {
		t.Errorf("ID: mau %q, dapat %q", "surat_masuk.disposisi.standar", def.ID)
	}
	if def.InitialState != "diterima" {
		t.Errorf("InitialState: mau %q, dapat %q", "diterima", def.InitialState)
	}
	if def.Version != 1 {
		t.Errorf("Version: mau 1, dapat %d", def.Version)
	}
	if def.AuthoringSource != "developer" {
		t.Errorf("AuthoringSource: mau %q, dapat %q", "developer", def.AuthoringSource)
	}

	// 3 states tersedia.
	if len(def.States) != 3 {
		t.Fatalf("mau 3 states, dapat %d", len(def.States))
	}

	// State terminal ada.
	hasTerminal := false
	for _, s := range def.States {
		if s.IsTerminal {
			hasTerminal = true
		}
	}
	if !hasTerminal {
		t.Error("harus ada state terminal")
	}

	// 3 transisi tersedia.
	if len(def.Transitions) != 3 {
		t.Fatalf("mau 3 transisi, dapat %d", len(def.Transitions))
	}

	// Notifikasi transisi terakhir terbaca.
	last := def.Transitions[2]
	if last.Notify == nil || last.Notify.ToRole != "agendaris" {
		t.Errorf("notify transisi terakhir tidak terbaca: %+v", last.Notify)
	}

	// SLA state didisposisi terbaca.
	for _, s := range def.States {
		if s.Name == "didisposisi" && s.SLAHours != 72 {
			t.Errorf("sla_hours didisposisi: mau 72, dapat %d", s.SLAHours)
		}
	}
}

func TestParseYAML_IDKosong_Ditolak(t *testing.T) {
	data := []byte(`
initial_state: a
states:
  a:
    is_terminal: true
`)
	_, err := workflow.ParseYAML(data)
	if err == nil {
		t.Error("YAML tanpa id harus ditolak")
	}
}

func TestParseYAML_InitialStateTakAda_Ditolak(t *testing.T) {
	data := []byte(`
id: test.bad
initial_state: TIDAK_ADA
states:
  a:
    is_terminal: true
transitions:
  - from: a
    to: a
    on: x
`)
	_, err := workflow.ParseYAML(data)
	if err == nil {
		t.Error("initial_state tidak ada di states harus ditolak")
	}
}

func TestParseYAML_TransisiKeStateTakAda_Ditolak(t *testing.T) {
	data := []byte(`
id: test.badtransition
initial_state: a
states:
  a:
    label: A
  selesai:
    is_terminal: true
transitions:
  - from: a
    to: TIDAK_ADA
    on: aksi
`)
	_, err := workflow.ParseYAML(data)
	if err == nil {
		t.Error("transisi ke state tak ada harus ditolak")
	}
}

func TestParseYAML_TanpaTerminalState_Ditolak(t *testing.T) {
	data := []byte(`
id: test.noterminal
initial_state: a
states:
  a:
    label: A tanpa terminal
transitions:
  - from: a
    to: a
    on: loop
`)
	_, err := workflow.ParseYAML(data)
	if err == nil {
		t.Error("YAML tanpa terminal state harus ditolak")
	}
}

func TestParseYAML_YAMLMalformed_Ditolak(t *testing.T) {
	data := []byte(`bukan: yaml: valid: [`)
	_, err := workflow.ParseYAML(data)
	if err == nil {
		t.Error("YAML malformed harus ditolak")
	}
}

// ===== SeedYAML =====

func TestSeedYAML_ValidDiRegister(t *testing.T) {
	store := workflow.NewMemoryStore()
	if err := workflow.SeedYAML([]byte(yamlDisposisi), store); err != nil {
		t.Fatalf("SeedYAML: %v", err)
	}
	// Verifikasi bisa di-Get.
	def, err := store.Get("surat_masuk.disposisi.standar")
	if err != nil {
		t.Fatalf("Get setelah seed: %v", err)
	}
	if def.ID != "surat_masuk.disposisi.standar" {
		t.Errorf("definisi tersimpan salah ID: %q", def.ID)
	}
}

func TestSeedYAML_SudahAda_TidakMenutup(t *testing.T) {
	store := workflow.NewMemoryStore()
	// Seed pertama kali.
	if err := workflow.SeedYAML([]byte(yamlDisposisi), store); err != nil {
		t.Fatalf("seed pertama: %v", err)
	}
	// Seed kedua tidak boleh error (idempoten — DB aktif tidak ditimpa).
	if err := workflow.SeedYAML([]byte(yamlDisposisi), store); err != nil {
		t.Errorf("seed ulang harus no-op, dapat error: %v", err)
	}
}

// ===== LoadYAML =====

func TestLoadYAML_FileValid_DiRegister(t *testing.T) {
	// Tulis file YAML ke direktori temp untuk test.
	dir := t.TempDir()
	path := filepath.Join(dir, "disposisi.yaml")
	if err := os.WriteFile(path, []byte(yamlDisposisi), 0600); err != nil {
		t.Fatalf("buat file temp: %v", err)
	}

	store := workflow.NewMemoryStore()
	if err := workflow.LoadYAML(path, store); err != nil {
		t.Fatalf("LoadYAML: %v", err)
	}

	if _, err := store.Get("surat_masuk.disposisi.standar"); err != nil {
		t.Errorf("definisi tidak tersimpan setelah LoadYAML: %v", err)
	}
}

func TestLoadYAML_FileTidakAda_Ditolak(t *testing.T) {
	store := workflow.NewMemoryStore()
	err := workflow.LoadYAML("/tidak/ada/file.yaml", store)
	if err == nil {
		t.Error("LoadYAML file tidak ada harus ditolak")
	}
}
