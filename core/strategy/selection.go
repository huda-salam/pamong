package strategy

import (
	"context"
	"sync"
)

// SelectionSource memberi tahu registry KEY varian mana yang dipilih tenant untuk sebuah
// decision point. Ini adalah seam: PR-3.3.1 memakai MemorySelectionSource (in-memory);
// PR-3.3.2 menggantinya dengan resolver core/config ber-scope (tenant[/unit/resource])
// tanpa mengubah Registry.
//
// SelectedKey mengembalikan (key, true, nil) bila tenant punya pilihan; (·, false, nil)
// bila belum memilih (registry lalu memakai default developer bila ada). Error hanya untuk
// kegagalan sumber (mis. DB), bukan untuk "belum memilih".
type SelectionSource interface {
	SelectedKey(ctx context.Context, tenantID, decisionPoint string) (key string, ok bool, err error)
}

// MemorySelectionSource adalah SelectionSource in-memory untuk test & bootstrap awal.
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
