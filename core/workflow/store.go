package workflow

import (
	"fmt"
	"sync"
)

// MemoryStore adalah implementasi DefinitionStore berbasis in-memory map.
// Dipakai untuk test unit dan bootstrap awal sebelum DB-backed store (PR-3.2.3)
// siap. Thread-safe lewat RWMutex.
//
// Menyimpan SEMUA versi per ID (id → version → def) — Register versi baru tidak
// menghapus yang lama, sehingga instance yang terkunci ke versi tertentu tetap bisa
// mengambil definisinya lewat GetVersion (PR-3.2.7).
type MemoryStore struct {
	mu   sync.RWMutex
	defs map[string]map[int]WorkflowDefinition
}

// NewMemoryStore membuat store kosong.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{defs: make(map[string]map[int]WorkflowDefinition)}
}

var _ DefinitionStore = (*MemoryStore)(nil)

// Register memvalidasi dan menyimpan definisi sebagai versi tersendiri. Validasi
// dilakukan di sini (pintu masuk) agar error struktur ketahuan saat load, bukan saat
// runtime eksekusi transisi. Version ≤ 0 dinormalkan ke 1 (baseline). Register dengan
// (id, version) yang sama menimpa entri versi tersebut; version berbeda menambah versi.
func (s *MemoryStore) Register(def WorkflowDefinition) error {
	if err := Validate(def); err != nil {
		return err
	}
	if def.Version <= 0 {
		def.Version = 1
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	versions, ok := s.defs[def.ID]
	if !ok {
		versions = make(map[int]WorkflowDefinition)
		s.defs[def.ID] = versions
	}
	versions[def.Version] = def
	return nil
}

// Get mengembalikan versi TERBARU (version tertinggi) dari definisi. Error bila ID
// tidak ada sama sekali.
func (s *MemoryStore) Get(id string) (WorkflowDefinition, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	versions, ok := s.defs[id]
	if !ok || len(versions) == 0 {
		return WorkflowDefinition{}, ErrDefinitionNotFound(id)
	}
	maxVer := 0
	for v := range versions {
		if v > maxVer {
			maxVer = v
		}
	}
	return versions[maxVer], nil
}

// GetVersion mengembalikan versi spesifik dari definisi. Error bila ID atau versi
// tersebut tidak ada — instance yang terkunci ke versi yang sudah dihapus akan gagal
// eksplisit, bukan diam-diam memakai versi lain.
func (s *MemoryStore) GetVersion(id string, version int) (WorkflowDefinition, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	versions, ok := s.defs[id]
	if !ok {
		return WorkflowDefinition{}, ErrDefinitionNotFound(id)
	}
	def, ok := versions[version]
	if !ok {
		return WorkflowDefinition{}, ErrDefinitionVersionNotFound(id, version)
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

	// Compile semua guard di pintu masuk — syntax error / tipe non-boolean ditolak
	// saat load, bukan runtime (PR-3.2.5, PRD F5).
	if err := validateGuards(def); err != nil {
		return err
	}
	return nil
}
