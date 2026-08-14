package workflow

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/huda-salam/pamong/core"
)

// ===== YAML intermediate structs =====
//
// Format file YAML workflow (lihat modules/surat_masuk/workflows/disposisi.yaml):
//   - id, entity, version, initial_state: field datar
//   - states: MAP nama→yamlState (bukan slice) — dikonversi ke []State dengan sort nama
//   - transitions: SLICE yamlTransition
//
// Pemisahan struct YAML ↔ domain (WorkflowDefinition) dilakukan sengaja agar format file
// bisa berevolusi tanpa menyentuh domain struct.

type yamlDefinition struct {
	ID           string               `yaml:"id"`
	Entity       string               `yaml:"entity"`
	Version      int                  `yaml:"version"`
	InitialState string               `yaml:"initial_state"`
	States       map[string]yamlState `yaml:"states"`
	Transitions  []yamlTransition     `yaml:"transitions"`
}

type yamlState struct {
	Label          string   `yaml:"label"`
	SLAHours       int      `yaml:"sla_hours"`
	EscalateToRole string   `yaml:"escalate_to_role"`
	IsTerminal     bool     `yaml:"is_terminal"`
	Actions        []string `yaml:"actions"`
}

type yamlTransition struct {
	From   string      `yaml:"from"`
	To     string      `yaml:"to"`
	On     string      `yaml:"on"`
	Guards []string    `yaml:"guards"`
	Action string      `yaml:"action"`
	Notify *yamlNotify `yaml:"notify"`
}

type yamlNotify struct {
	ToRole   string `yaml:"to_role"`
	Template string `yaml:"template"`
}

// ===== Public API =====

// ParseYAML mem-parsing bytes YAML menjadi WorkflowDefinition yang divalidasi.
// Validasi mencakup: field wajib terisi, referensi state konsisten, ada state terminal.
// Error dikembalikan dengan pesan jelas agar bootstrap gagal cepat dengan diagnosa tepat.
func ParseYAML(data []byte) (WorkflowDefinition, error) {
	var raw yamlDefinition
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return WorkflowDefinition{}, fmt.Errorf("workflow YAML tidak valid: %w", err)
	}
	return convertAndValidate(raw)
}

// DefinitionSeeder adalah store yang bisa men-seed definisi baseline secara ATOMIK ("daftarkan
// bila workflow_id ini belum punya versi apa pun"). Dipenuhi MemoryStore & infra/workflow.DBStore.
//
// Ia ada karena "Get lalu Register" adalah check-then-act yang tidak aman begitu seed pindah ke
// JALUR REQUEST (cold-start tumpukan workflow per tenant, PR-W4a) dan berjalan di lebih dari satu
// replika: dua pemanggil bisa sama-sama melihat "belum ada" lalu sama-sama menulis — yang satu
// gagal di PRIMARY KEY (workflow_id, version), atau yang lebih buruk, keduanya lolos dan versi
// baseline kedua menjadi versi TERBARU yang menggantikan definisi hasil kustomisasi tenant.
type DefinitionSeeder interface {
	SeedIfAbsent(def WorkflowDefinition) error
}

// SeedYAML mem-parsing bytes YAML dan mendaftarkannya ke store bila belum ada.
// Bila ID sudah ada di store (DB aktif telah punya definisi ini), operasi di-skip
// tanpa error — DB adalah sumber kebenaran aktif, YAML hanya baseline developer.
//
// Store yang memenuhi DefinitionSeeder memakai jalur atomiknya. Jalur cadangan (Get→Register)
// membedakan "belum ada" dari "gagal membaca": kegagalan infra dikembalikan apa adanya, TIDAK
// diperlakukan sebagai "belum ada" — kalau tidak, satu error koneksi sesaat akan mendaftarkan
// ulang baseline developer di atas definisi tenant yang sudah dikustomisasi.
func SeedYAML(data []byte, store DefinitionStore) error {
	def, err := ParseYAML(data)
	if err != nil {
		return err
	}
	if seeder, ok := store.(DefinitionSeeder); ok {
		return seeder.SeedIfAbsent(def)
	}
	_, err = store.Get(def.ID)
	switch {
	case err == nil:
		return nil // sudah ada — jangan timpa
	case !isNotFound(err):
		return err // gagal baca ≠ belum ada
	}
	return store.Register(def)
}

// isNotFound melaporkan apakah err adalah core.ErrNotFound (definisi memang belum ada).
func isNotFound(err error) bool {
	var fe *core.FrameworkError
	return errors.As(err, &fe) && fe.Code == "NOT_FOUND"
}

// LoadYAML membaca file YAML dari path di DISK dan memanggil SeedYAML. Dipakai tooling
// (pamongctl) dan test yang bekerja dari checkout repo.
//
// Jalur PRODUKSI adalah SeedFS: binary yang ter-deploy tak punya direktori repo, jadi seed
// modul dibaca dari FS ter-embed (domain.WorkflowRef.FS).
func LoadYAML(path string, store DefinitionStore) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("baca file workflow %q: %w", path, err)
	}
	if err := SeedYAML(data, store); err != nil {
		return fmt.Errorf("seed workflow dari %q: %w", path, err)
	}
	return nil
}

// SeedFS membaca satu definisi dari filesystem ter-embed modul lalu men-seed-nya ke store.
// Ini jalur yang dipakai composition root saat menyiapkan tumpukan workflow sebuah tenant
// (definisi hidup di tenant DB, jadi seed terjadi per tenant — idempoten lewat SeedYAML).
func SeedFS(fsys fs.FS, path string, store DefinitionStore) error {
	if fsys == nil {
		return fmt.Errorf("seed workflow %q: filesystem ter-embed nil (isi WorkflowRef.FS)", path)
	}
	data, err := fs.ReadFile(fsys, path)
	if err != nil {
		return fmt.Errorf("baca workflow ter-embed %q: %w", path, err)
	}
	if err := SeedYAML(data, store); err != nil {
		return fmt.Errorf("seed workflow dari %q: %w", path, err)
	}
	return nil
}

// ===== Konversi & validasi internal =====

// convertAndValidate mengonversi yamlDefinition ke WorkflowDefinition dan memvalidasinya.
// States dari map di-sort by name agar urutan deterministik (map Go tidak berurutan).
// Validasi struktural didelegasikan ke validateDefinition (store.go) agar aturan
// konsisten antara Register langsung dan Register via loader.
func convertAndValidate(raw yamlDefinition) (WorkflowDefinition, error) {
	if raw.ID == "" {
		return WorkflowDefinition{}, ErrInvalidDefinition("field 'id' wajib diisi di YAML workflow")
	}

	// Konversi states dari map ke slice, sort by name untuk determinisme.
	names := make([]string, 0, len(raw.States))
	for name := range raw.States {
		names = append(names, name)
	}
	sort.Strings(names)

	states := make([]State, 0, len(names))
	for _, name := range names {
		ys := raw.States[name]
		states = append(states, State{
			Name:           name,
			Label:          ys.Label,
			SLAHours:       ys.SLAHours,
			EscalateToRole: ys.EscalateToRole,
			IsTerminal:     ys.IsTerminal,
			Actions:        ys.Actions,
		})
	}

	// Konversi transitions.
	transitions := make([]Transition, 0, len(raw.Transitions))
	for _, yt := range raw.Transitions {
		tr := Transition{
			From:   yt.From,
			To:     yt.To,
			On:     yt.On,
			Guards: yt.Guards,
			Action: yt.Action,
		}
		if yt.Notify != nil {
			tr.Notify = &NotifySpec{
				ToRole:   yt.Notify.ToRole,
				Template: yt.Notify.Template,
			}
		}
		transitions = append(transitions, tr)
	}

	version := raw.Version
	if version <= 0 {
		version = 1 // default aman bila tidak diset di YAML
	}

	def := WorkflowDefinition{
		ID:              raw.ID,
		Entity:          raw.Entity,
		Version:         version,
		EffectiveFrom:   time.Now(),
		InitialState:    raw.InitialState,
		States:          states,
		Transitions:     transitions,
		AuthoringSource: "developer",
	}

	// Delegasikan validasi struktural ke Validate — aturan sama dengan Register manual.
	if err := Validate(def); err != nil {
		return WorkflowDefinition{}, fmt.Errorf("YAML workflow %q tidak valid: %w", raw.ID, err)
	}
	return def, nil
}
