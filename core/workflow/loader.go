package workflow

import (
	"fmt"
	"os"
	"sort"
	"time"

	"gopkg.in/yaml.v3"
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

// SeedYAML mem-parsing bytes YAML dan mendaftarkannya ke store bila belum ada.
// Bila ID sudah ada di store (DB aktif telah punya definisi ini), operasi di-skip
// tanpa error — DB adalah sumber kebenaran aktif, YAML hanya baseline developer.
func SeedYAML(data []byte, store DefinitionStore) error {
	def, err := ParseYAML(data)
	if err != nil {
		return err
	}
	// Cek apakah sudah ada di store — jika ya, tidak timpa.
	if _, err := store.Get(def.ID); err == nil {
		return nil
	}
	return store.Register(def)
}

// LoadYAML membaca file YAML dari path dan memanggil SeedYAML.
// Dipakai modul saat bootstrap untuk mendaftarkan seed workflow mereka.
//
//	app.Workflow().Load("modules/surat_masuk/workflows/disposisi.yaml")
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

	// Delegasikan validasi struktural ke validateDefinition — aturan sama dengan Register manual.
	if err := validateDefinition(def); err != nil {
		return WorkflowDefinition{}, fmt.Errorf("YAML workflow %q tidak valid: %w", raw.ID, err)
	}
	return def, nil
}
