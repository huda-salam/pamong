// Package customization adalah layer kustomisasi tenant yang TERPISAH dari definisi modul
// inti: custom field, label override, dan capability flags (core/customization/PRD.md).
// Upgrade framework tidak menimpa kustomisasi tenant; kustomisasi tenant tidak mengotori
// modul inti.
//
// PR-3.4.2: capability flags (F3) — gate fitur dormant per-tenant tanpa rilis terpisah atau
// percabangan kode yang menyebar (CLAUDE.md titik ekstensi #6). Custom field (F1) & label
// override (F2) menyusul PR-3.4.1.
package customization

import (
	"context"
	"sort"
	"strings"
	"sync"
)

// Capability adalah fitur ber-gate yang dapat diaktif/dinonaktifkan per-tenant tanpa rilis
// terpisah (CLAUDE.md titik ekstensi #6, PRD F3). Fitur DIDEKLARASIKAN di kode; yang tersimpan
// per-tenant hanya override boolean — BUKAN definisi fitur (sejalan prinsip "tidak ada logika
// tereksekusi tersimpan di DB", CLAUDE.md §Fleksibilitas). Fitur dormant lahir dengan
// DefaultEnabled=false dan diaktifkan per-tenant saat matang, mencegah long-lived feature branch.
type Capability struct {
	Name           string // "{modul}.{fitur}", mis. "keuangan.approval_paralel"
	Label          string
	Description    string
	DefaultEnabled bool // perilaku saat tenant belum meng-override
}

// CapabilityRegistry menyimpan deklarasi capability yang dikenal sistem (titik ekstensi #1:
// interface + registry ber-key + Register). Diisi saat bootstrap/registrasi modul; menambah
// fitur baru = mendaftarkan satu Capability, bukan mengubah kode gate.
type CapabilityRegistry struct {
	mu   sync.RWMutex
	caps map[string]Capability
}

// NewCapabilityRegistry membuat registry kosong.
func NewCapabilityRegistry() *CapabilityRegistry {
	return &CapabilityRegistry{caps: make(map[string]Capability)}
}

// Register mendaftarkan satu capability. Nama wajib berformat {modul}.{fitur} (≥2 segmen
// non-kosong); nama ganda ditolak — registrasi ulang menandakan bug wiring modul.
func (r *CapabilityRegistry) Register(c Capability) error {
	if err := validateCapabilityName(c.Name); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.caps[c.Name]; dup {
		return ErrCapabilityExists(c.Name)
	}
	r.caps[c.Name] = c
	return nil
}

// Get mengembalikan deklarasi capability terdaftar.
func (r *CapabilityRegistry) Get(name string) (Capability, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.caps[name]
	return c, ok
}

// List mengembalikan semua capability terdaftar, terurut nama (deterministik untuk UI admin).
func (r *CapabilityRegistry) List() []Capability {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Capability, 0, len(r.caps))
	for _, c := range r.caps {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// TenantCapabilityStore menyimpan override per-tenant. Pluggable (Memory untuk test/bootstrap;
// adapter Postgres menyusul). Hanya menyimpan override EKSPLISIT — ketiadaan override berarti
// "pakai DefaultEnabled", bukan "nonaktif".
type TenantCapabilityStore interface {
	// Override mengembalikan (enabled, ok, err). ok=false bila tenant tak menetapkan override
	// untuk capability tsb (caller memakai DefaultEnabled).
	Override(ctx context.Context, tenantID, capability string) (enabled, ok bool, err error)
	// Set menetapkan/menimpa override per-tenant.
	Set(ctx context.Context, tenantID, capability string, enabled bool) error
}

// CapabilityResolver menjawab "apakah fitur X aktif untuk tenant T": override tenant menang;
// bila tak ada override → DefaultEnabled dari deklarasi. Capability tak terdaftar → error
// (FAIL-CLOSED: fitur tak dikenal tak boleh dianggap aktif diam-diam).
type CapabilityResolver struct {
	reg   *CapabilityRegistry
	store TenantCapabilityStore
}

// NewCapabilityResolver merakit resolver dari registry deklarasi + store override.
func NewCapabilityResolver(reg *CapabilityRegistry, store TenantCapabilityStore) *CapabilityResolver {
	return &CapabilityResolver{reg: reg, store: store}
}

// IsEnabled mengevaluasi gate untuk (tenant, capability). Capability wajib terdaftar.
func (r *CapabilityResolver) IsEnabled(ctx context.Context, tenantID, capability string) (bool, error) {
	decl, ok := r.reg.Get(capability)
	if !ok {
		return false, ErrUnknownCapability(capability)
	}
	enabled, hasOverride, err := r.store.Override(ctx, tenantID, capability)
	if err != nil {
		return false, err
	}
	if !hasOverride {
		return decl.DefaultEnabled, nil
	}
	return enabled, nil
}

// MemoryTenantCapabilityStore adalah TenantCapabilityStore in-memory untuk test/bootstrap.
type MemoryTenantCapabilityStore struct {
	mu        sync.RWMutex
	overrides map[string]bool // key: capKey(tenantID, capability)
}

// NewMemoryTenantCapabilityStore membuat store kosong.
func NewMemoryTenantCapabilityStore() *MemoryTenantCapabilityStore {
	return &MemoryTenantCapabilityStore{overrides: make(map[string]bool)}
}

// Override membaca override eksplisit; ok=false bila belum pernah di-Set untuk tenant tsb.
func (s *MemoryTenantCapabilityStore) Override(_ context.Context, tenantID, capability string) (bool, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.overrides[capKey(tenantID, capability)]
	return v, ok, nil
}

// Set menetapkan override per-tenant.
func (s *MemoryTenantCapabilityStore) Set(_ context.Context, tenantID, capability string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.overrides[capKey(tenantID, capability)] = enabled
	return nil
}

var _ TenantCapabilityStore = (*MemoryTenantCapabilityStore)(nil)

// capKey menggabungkan tenant + capability dengan pemisah NUL (tak mungkin muncul di nama).
func capKey(tenantID, capability string) string {
	return tenantID + "\x00" + capability
}

// validateCapabilityName menegakkan format {modul}.{fitur} (≥2 segmen non-kosong).
func validateCapabilityName(name string) error {
	segs := strings.Split(name, ".")
	if len(segs) < 2 {
		return ErrInvalidCapabilityName(name, "wajib berformat {modul}.{fitur}")
	}
	for _, s := range segs {
		if strings.TrimSpace(s) == "" {
			return ErrInvalidCapabilityName(name, "segmen nama tidak boleh kosong")
		}
	}
	return nil
}
