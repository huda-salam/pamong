package workflow

import (
	"maps"
	"sync"
	"time"

	"github.com/google/uuid"
)

// TenantWorkflowConfig menyimpan pilihan template workflow satu tenant untuk satu
// slot, beserta parameter binding peran. Slot mengidentifikasi tipe workflow
// (mis. "surat_masuk.disposisi"); TemplateID merujuk WorkflowDefinition yang dipilih
// (mis. "surat_masuk.disposisi.standar"). RoleBindings memetakan nama peran generik
// dalam template ke role konkret milik tenant.
//
// Contoh: template memakai peran "validator_tahap_1"; tenant A memetakannya ke
// "ppk_opd", tenant B ke "kepala_bagian". Engine memakai nama yang sudah di-bind
// saat resolusi peran ke orang (lewat core/permission + kepegawaian).
type TenantWorkflowConfig struct {
	TenantID     string
	Slot         string            // "surat_masuk.disposisi"
	TemplateID   string            // "surat_masuk.disposisi.standar"
	RoleBindings map[string]string // peran generik → role konkret tenant
	SetAt        time.Time
	SetBy        *uuid.UUID // nil = ditetapkan oleh seed/framework
}

// ApplyBindings mengembalikan salinan def dengan nama peran dalam EscalateToRole
// (tiap State) dan NotifySpec.ToRole (tiap Transition) diganti lewat tabel bindings.
// Peran yang tidak ada di bindings dibiarkan apa adanya. Bila bindings kosong,
// definisi dikembalikan tanpa perubahan.
func ApplyBindings(def WorkflowDefinition, bindings map[string]string) WorkflowDefinition {
	if len(bindings) == 0 {
		return def
	}
	resolve := func(role string) string {
		if bound, ok := bindings[role]; ok {
			return bound
		}
		return role
	}

	// Salin slice states agar tidak mutasi definisi asli.
	states := make([]State, len(def.States))
	copy(states, def.States)
	for i, s := range states {
		states[i].EscalateToRole = resolve(s.EscalateToRole)
	}

	// Salin slice transitions; NotifySpec di-clone bila non-nil.
	transitions := make([]Transition, len(def.Transitions))
	copy(transitions, def.Transitions)
	for i, tr := range transitions {
		if tr.Notify != nil {
			n := *tr.Notify
			n.ToRole = resolve(n.ToRole)
			transitions[i].Notify = &n
		}
	}

	bound := def
	bound.States = states
	bound.Transitions = transitions
	return bound
}

// MemoryTemplateStore adalah implementasi TemplateStore berbasis in-memory map.
// Dipakai untuk unit test dan bootstrap awal. Thread-safe lewat RWMutex.
// Membutuhkan DefinitionStore untuk mengambil definisi template saat GetForTenant.
type MemoryTemplateStore struct {
	mu      sync.RWMutex
	configs map[string]TenantWorkflowConfig // key: tenantID+":"+slot
	defs    DefinitionStore
}

// NewMemoryTemplateStore membuat store kosong yang mengambil definisi dari defs.
func NewMemoryTemplateStore(defs DefinitionStore) *MemoryTemplateStore {
	return &MemoryTemplateStore{
		configs: make(map[string]TenantWorkflowConfig),
		defs:    defs,
	}
}

var _ TemplateStore = (*MemoryTemplateStore)(nil)

func configKey(tenantID, slot string) string { return tenantID + ":" + slot }

// SetTenantTemplate menyimpan atau mengganti config tenant. Idempoten.
func (s *MemoryTemplateStore) SetTenantTemplate(cfg TenantWorkflowConfig) error {
	if cfg.TenantID == "" || cfg.Slot == "" || cfg.TemplateID == "" {
		return ErrInvalidTemplateConfig("tenant_id, slot, dan template_id wajib diisi")
	}
	if cfg.SetAt.IsZero() {
		cfg.SetAt = time.Now()
	}
	// Salin bindings agar caller tidak bisa memutasi isi store setelah Set.
	cfg.RoleBindings = maps.Clone(cfg.RoleBindings)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.configs[configKey(cfg.TenantID, cfg.Slot)] = cfg
	return nil
}

// GetTenantConfig mengembalikan config tersimpan. ErrTemplateNotConfigured bila belum ada.
func (s *MemoryTemplateStore) GetTenantConfig(tenantID, slot string) (TenantWorkflowConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cfg, ok := s.configs[configKey(tenantID, slot)]
	if !ok {
		return TenantWorkflowConfig{}, ErrTemplateNotConfigured(tenantID, slot)
	}
	// Salin bindings agar caller tidak memutasi state store lewat map yang dikembalikan.
	cfg.RoleBindings = maps.Clone(cfg.RoleBindings)
	return cfg, nil
}

// GetForTenant mengembalikan WorkflowDefinition yang dipilih tenant dengan role
// binding sudah diterapkan. ErrTemplateNotConfigured bila belum ada pilihan.
func (s *MemoryTemplateStore) GetForTenant(tenantID, slot string) (WorkflowDefinition, error) {
	cfg, err := s.GetTenantConfig(tenantID, slot)
	if err != nil {
		return WorkflowDefinition{}, err
	}
	def, err := s.defs.Get(cfg.TemplateID)
	if err != nil {
		return WorkflowDefinition{}, err
	}
	return ApplyBindings(def, cfg.RoleBindings), nil
}
