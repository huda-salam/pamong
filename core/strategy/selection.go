package strategy

import (
	"context"
	"sync"

	"github.com/huda-salam/pamong/core/config"
)

// SelectionSource memberi tahu registry KEY varian mana yang dipilih tenant untuk sebuah
// decision point. Ini adalah seam: jalur produksi memakai ConfigSelectionSource (di atas
// resolver core/config ber-scope tenant[/unit/resource], PR-3.3.2); MemorySelectionSource
// tetap tersedia untuk test. Registry tidak berubah antar sumber.
//
// SelectedKey mengembalikan (key, true, nil) bila tenant punya pilihan; (·, false, nil)
// bila belum memilih (registry lalu memakai default developer bila ada). Error hanya untuk
// kegagalan sumber (mis. DB), bukan untuk "belum memilih".
type SelectionSource interface {
	SelectedKey(ctx context.Context, tenantID, decisionPoint string) (key string, ok bool, err error)
}

// ConfigSelectionSource adalah SelectionSource jalur produksi: ia menanyakan pilihan key
// tenant ke resolver tenant config ber-scope (core/config). Ini menggantikan
// MemorySelectionSource sebagai sumber pilihan sesungguhnya (PR-3.3.2). Decision point
// dipetakan langsung ke config key; scope saat ini selalu level tenant, tapi resolver sudah
// siap memperdalam ke unit kerja/resource tanpa mengubah Registry (titik ekstensi #2).
type ConfigSelectionSource struct {
	resolver *config.Resolver
}

// NewConfigSelectionSource membungkus resolver tenant config sebagai SelectionSource.
func NewConfigSelectionSource(r *config.Resolver) *ConfigSelectionSource {
	return &ConfigSelectionSource{resolver: r}
}

var _ SelectionSource = (*ConfigSelectionSource)(nil)

// SelectedKey mengimplementasi SelectionSource: pilihan tenant untuk decision point =
// nilai config pada key = decisionPoint, di-resolve pada level tenant.
func (s *ConfigSelectionSource) SelectedKey(ctx context.Context, tenantID, decisionPoint string) (string, bool, error) {
	return s.resolver.Resolve(ctx, config.ConfigScope{TenantID: tenantID}, decisionPoint)
}

// MemorySelectionSource adalah SelectionSource in-memory untuk test & bootstrap awal.
// Sejak PR-3.3.2, jalur produksi memakai ConfigSelectionSource; MemorySelectionSource
// dipertahankan untuk test unit yang tidak butuh resolver/DB.
// Thread-safe. Kunci internal: tenantID + "\x00" + decisionPoint.
type MemorySelectionSource struct {
	mu      sync.RWMutex
	choices map[string]string
}

// NewMemorySelectionSource membuat sumber pilihan kosong.
func NewMemorySelectionSource() *MemorySelectionSource {
	return &MemorySelectionSource{choices: make(map[string]string)}
}

var _ SelectionSource = (*MemorySelectionSource)(nil)

// Set menetapkan pilihan tenant untuk sebuah decision point. Idempoten (menimpa).
// Catatan: ini BUKAN jalur produksi ber-audit/ber-versi — itu PR-3.3.3 lewat core/config.
func (m *MemorySelectionSource) Set(tenantID, decisionPoint, key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.choices[selKey(tenantID, decisionPoint)] = key
}

// SelectedKey mengimplementasi SelectionSource.
func (m *MemorySelectionSource) SelectedKey(_ context.Context, tenantID, decisionPoint string) (string, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	key, ok := m.choices[selKey(tenantID, decisionPoint)]
	return key, ok, nil
}

func selKey(tenantID, decisionPoint string) string {
	return tenantID + "\x00" + decisionPoint
}
