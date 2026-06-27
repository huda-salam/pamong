// Package ratelimit menyediakan driven adapter port.RateLimiter. Implementasi awal in-memory
// (single-instance, cukup untuk Tier 1). Swap ke Redis untuk multi-instance bersifat additive di
// balik port.RateLimiter — use case tak berubah (ADR-008 §4, titik ekstensi #1).
package ratelimit

import (
	"context"
	"sync"
	"time"

	"github.com/huda-salam/pamong/port"
)

// Memory adalah RateLimiter fixed-window per key, aman untuk akses konkuren. Setiap key punya
// jendela hitung yang reset setelah lewat; di dalam jendela, percobaan melebihi limit ditolak.
type Memory struct {
	mu      sync.Mutex
	windows map[string]*window
	now     func() time.Time
}

type window struct {
	count   int
	resetAt time.Time
}

var _ port.RateLimiter = (*Memory)(nil)

// NewMemory membuat limiter in-memory. now opsional (nil → time.Now) untuk uji deterministik.
func NewMemory(now func() time.Time) *Memory {
	if now == nil {
		now = time.Now
	}
	return &Memory{windows: map[string]*window{}, now: now}
}

// Allow mencatat satu percobaan untuk key dan melaporkan apakah masih dalam batas. limit ≤ 0
// selalu menolak (fail-closed terhadap konfigurasi tak masuk akal).
func (m *Memory) Allow(_ context.Context, key string, limit int, dur time.Duration) (bool, error) {
	if limit <= 0 {
		return false, nil
	}
	now := m.now()

	m.mu.Lock()
	defer m.mu.Unlock()

	w := m.windows[key]
	if w == nil || !now.Before(w.resetAt) {
		// Jendela baru / kedaluwarsa → mulai hitung ulang.
		m.windows[key] = &window{count: 1, resetAt: now.Add(dur)}
		m.purgeExpired(now)
		return true, nil
	}
	if w.count >= limit {
		return false, nil
	}
	w.count++
	return true, nil
}

// purgeExpired membuang jendela yang sudah lewat agar map tak tumbuh tak terbatas. Dipanggil
// oportunistik saat membuat jendela baru; O(n) tapi jarang relatif terhadap jumlah key aktif.
// Pemanggil sudah memegang m.mu.
func (m *Memory) purgeExpired(now time.Time) {
	for k, w := range m.windows {
		if !now.Before(w.resetAt) {
			delete(m.windows, k)
		}
	}
}
