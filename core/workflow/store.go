package workflow

import (
	"fmt"
	"sync"
)

// MemoryStore adalah implementasi DefinitionStore berbasis in-memory map.
// Dipakai untuk test unit dan bootstrap awal sebelum DB-backed store (PR-3.2.3)
// siap. Thread-safe lewat RWMutex.
type MemoryStore struct {
	mu   sync.RWMutex
	defs map[string]WorkflowDefinition
}

// NewMemoryStore membuat store kosong.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{defs: make(map[string]WorkflowDefinition)}
}

var _ DefinitionStore = (*MemoryStore)(nil)

// Register memvalidasi dan menyimpan definisi. Validasi dilakukan di sini (pintu masuk)
// agar error struktur ketahuan saat load, bukan saat runtime eksekusi transisi.
func (s *MemoryStore) Register(def WorkflowDefinition) error {
	if err := Validate(def); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.defs[def.ID] = def
	return nil
}

// Get mengembalikan definisi berdasarkan ID. Error bila tidak ada.
func (s *MemoryStore) Get(id string) (WorkflowDefinition, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	def, ok := s.defs[id]
	if !ok {
		return WorkflowDefinition{}, ErrDefinitionNotFound(id)
	}
	return def, nil
}

// Validate memeriksa invariant struktural definisi:
//   - ID, InitialState wajib terisi
//   - Tidak ada nama state duplikat
//   - InitialState harus ada di daftar States
//   - Setiap transisi From/To harus merujuk state yang ada
//   - Minimal satu state terminal ada (reachability penuh DEFERRED ke PR-3.2.2/loader)
//
// Dipanggil oleh MemoryStore.Register dan DBStore.Register — satu titik validasi
// untuk semua implementasi DefinitionStore.
func Validate(def WorkflowDefinition) error {
	if def.ID == "" {
		return ErrInvalidDefinition("id workflow wajib diisi")
	}
	if def.InitialState == "" {
		return ErrInvalidDefinition("initial_state wajib diisi")
	}

	// Bangun indeks state, deteksi duplikat.
	stateSet := make(map[string]struct{}, len(def.States))
	hasTerminal := false
	for _, s := range def.States {
		if s.Name == "" {
			return ErrInvalidDefinition("state tanpa nama ditemukan")
		}
		if _, dup := stateSet[s.Name]; dup {
			return ErrInvalidDefinition(fmt.Sprintf("duplikat state %q", s.Name))
		}
		stateSet[s.Name] = struct{}{}
		if s.IsTerminal {
			hasTerminal = true
		}
	}

	if _, ok := stateSet[def.InitialState]; !ok {
		return ErrInvalidDefinition(
			fmt.Sprintf("initial_state %q tidak ada di daftar states", def.InitialState))
	}
	if !hasTerminal {
		return ErrInvalidDefinition("definisi harus punya minimal satu state terminal")
	}

	// Validasi semua referensi state di transisi.
	for i, tr := range def.Transitions {
		if tr.From == "" || tr.To == "" || tr.On == "" {
			return ErrInvalidDefinition(
				fmt.Sprintf("transisi[%d]: from, to, dan on wajib diisi", i))
		}
		if _, ok := stateSet[tr.From]; !ok {
			return ErrInvalidDefinition(
				fmt.Sprintf("transisi[%d]: from state %q tidak ada", i, tr.From))
		}
		if _, ok := stateSet[tr.To]; !ok {
			return ErrInvalidDefinition(
				fmt.Sprintf("transisi[%d]: to state %q tidak ada", i, tr.To))
		}
	}
	return nil
}
