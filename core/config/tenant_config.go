package config

import (
	"context"
	"sync"

	"github.com/google/uuid"
)

// ConfigScope menyatakan pada konteks mana sebuah nilai config berlaku. Scope bertingkat:
// tenant → unit kerja → resource. Field yang lebih dalam bernilai nil berarti "berlaku di
// level yang lebih umum". Skema ini sengaja dibuat kaya sejak awal (titik ekstensi #2
// CLAUDE.md) agar scope bisa diperdalam tanpa migrasi — saat ini hampir semua pemakaian
// hanya mengisi TenantID.
//
// Invarian: ResourceID hanya bermakna bila UnitKerjaID juga terisi (resource ber-nested di
// bawah unit kerja). Store menegakkan invarian ini lewat CHECK; di sisi Go, scope dengan
// ResourceID != nil && UnitKerjaID == nil diperlakukan sebagai level resource yang tidak
// akan pernah cocok dengan query manapun yang taat invarian.
type ConfigScope struct {
	TenantID    string
	UnitKerjaID *uuid.UUID // nil = level tenant
	ResourceID  *uuid.UUID // nil = level unit/tenant
}

// specificity mengembalikan kedalaman scope: 0 = tenant, 1 = unit kerja, 2 = resource.
// Dipakai resolver untuk memilih "paling spesifik menang".
func (s ConfigScope) specificity() int {
	n := 0
	if s.UnitKerjaID != nil {
		n++
	}
	if s.ResourceID != nil {
		n++
	}
	return n
}

// appliesTo melaporkan apakah entry ber-scope s berlaku untuk query ber-scope q. Sebuah
// entry berlaku bila scope-nya adalah "prefiks" dari query: level yang di-set pada entry
// harus sama persis dengan query, level yang nil pada entry berarti "berlaku untuk apapun".
func (s ConfigScope) appliesTo(q ConfigScope) bool {
	if s.TenantID != q.TenantID {
		return false
	}
	if s.UnitKerjaID != nil && (q.UnitKerjaID == nil || *s.UnitKerjaID != *q.UnitKerjaID) {
		return false
	}
	if s.ResourceID != nil && (q.ResourceID == nil || *s.ResourceID != *q.ResourceID) {
		return false
	}
	return true
}

// ConfigEntry adalah satu nilai config ter-scope. Value disimpan sebagai string; makna nilai
// adalah tanggung jawab pemakai (mis. core/strategy menyimpan strategy key). SetBy nil bila
// ditetapkan oleh seed/framework, bukan aktor manusia.
type ConfigEntry struct {
	Scope ConfigScope
	Key   string
	Value string
	SetBy *uuid.UUID
}

// TenantConfigStore adalah driven port penyimpanan tenant config ber-scope. Resolver
// bergantung padanya; core/config menyediakan MemoryTenantConfigStore untuk test & bootstrap
// awal, infra/config menyediakan implementasi Postgres (gov.tenant_configs).
type TenantConfigStore interface {
	// Candidates mengembalikan SEMUA entry untuk pasangan tenant+key lintas level scope,
	// tanpa urutan tertentu. Resolver yang memilih mana paling spesifik. Slice kosong bila
	// tidak ada; error hanya untuk kegagalan sumber (mis. DB).
	Candidates(ctx context.Context, tenantID, key string) ([]ConfigEntry, error)

	// Set menyimpan (upsert) satu entry pada scope persisnya. Idempoten: entry dengan scope
	// & key yang sama menimpa nilai sebelumnya.
	Set(ctx context.Context, entry ConfigEntry) error
}

// MemoryTenantConfigStore adalah TenantConfigStore in-memory & thread-safe untuk test dan
// bootstrap awal. BUKAN jalur produksi ber-audit/ber-versi — itu Postgres store + PR-3.3.3.
type MemoryTenantConfigStore struct {
	mu      sync.RWMutex
	entries map[string][]ConfigEntry // (tenant "\x00" key) -> entries per scope
}

// NewMemoryTenantConfigStore membuat store kosong.
func NewMemoryTenantConfigStore() *MemoryTenantConfigStore {
	return &MemoryTenantConfigStore{entries: make(map[string][]ConfigEntry)}
}

var _ TenantConfigStore = (*MemoryTenantConfigStore)(nil)

// Candidates mengimplementasi TenantConfigStore.
func (m *MemoryTenantConfigStore) Candidates(_ context.Context, tenantID, key string) ([]ConfigEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	src := m.entries[bucketKey(tenantID, key)]
	if len(src) == 0 {
		return nil, nil
	}
	out := make([]ConfigEntry, len(src))
	copy(out, src)
	return out, nil
}

// Set mengimplementasi TenantConfigStore. Upsert berdasarkan scope penuh.
func (m *MemoryTenantConfigStore) Set(_ context.Context, entry ConfigEntry) error {
	if err := ValidateEntry(entry); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	bk := bucketKey(entry.Scope.TenantID, entry.Key)
	list := m.entries[bk]
	for i, e := range list {
		if sameScope(e.Scope, entry.Scope) {
			list[i] = entry
			m.entries[bk] = list
			return nil
		}
	}
	m.entries[bk] = append(list, entry)
	return nil
}

func bucketKey(tenantID, key string) string { return tenantID + "\x00" + key }

// sameScope membandingkan dua scope (identik bila tenant sama & pointer level bernilai sama).
func sameScope(a, b ConfigScope) bool {
	return a.TenantID == b.TenantID &&
		samePtr(a.UnitKerjaID, b.UnitKerjaID) &&
		samePtr(a.ResourceID, b.ResourceID)
}

func samePtr(a, b *uuid.UUID) bool {
	switch {
	case a == nil && b == nil:
		return true
	case a == nil || b == nil:
		return false
	default:
		return *a == *b
	}
}
