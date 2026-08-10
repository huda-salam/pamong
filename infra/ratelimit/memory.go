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

// Ruang key limiter dikendalikan PEMANGGIL ANONIM. Sejak PR-W1 rute /auth/* dilayani tanpa
// otentikasi dan key lapis-1-nya diturunkan dari nilai kredensial yang dikirim klien, jadi siapa
// pun bisa mencetak key baru sebanyak request yang ia kirim. Instance ini DIBAGI dengan middleware
// RateLimit rute bisnis, sehingga apa pun yang menahan mutex-nya menahan seluruh lalu lintas —
// biaya per-operasi karena itu harus O(1), bukan sekadar "biasanya murah".
//
// Penyimpanan memakai DUA GENERASI map yang dirotasi, bukan satu map yang disapu. Sapuan (dan
// eviksi satu-per-satu) berbiaya O(n) tepat pada keadaan yang paling mudah dipaksakan penyerang:
// map penuh. Rotasi membuang satu generasi sekaligus — O(1) — dan batas memori ditegakkan oleh
// syarat rotasinya sendiri, bukan oleh pemindaian.
const (
	// maxEntries membatasi total jendela tersimpan (cur + prev). Rotasi dipicu saat generasi
	// berjalan mencapai separuhnya.
	maxEntries = 100_000

	// rotateInterval merotasi generasi meski sepi, agar memori jendela menganggur kembali
	// dilepas. Dipilih lebih panjang dari jendela terpanjang yang dipakai (15 menit untuk
	// login/OTP) supaya penghitung nyata tidak dibuang lebih cepat dari masa berlakunya.
	rotateInterval = 20 * time.Minute
)

// Memory adalah RateLimiter fixed-window per key, aman untuk akses konkuren. Setiap key punya
// jendela hitung yang reset setelah lewat; di dalam jendela, percobaan melebihi limit ditolak.
type Memory struct {
	mu        sync.Mutex
	cur       map[string]*window // generasi berjalan
	prev      map[string]*window // generasi sebelumnya; masih dibaca, tak lagi ditulisi
	rotatedAt time.Time
	rotations int // diamati test: rotasi = satu-satunya mekanisme pembuangan
	now       func() time.Time
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
	return &Memory{
		cur:       map[string]*window{},
		prev:      map[string]*window{},
		rotatedAt: now(),
		now:       now,
	}
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

	m.rotateIfNeeded(now)

	w := m.lookup(key, now)
	if w == nil {
		m.cur[key] = &window{count: 1, resetAt: now.Add(dur)}
		return true, nil
	}
	if w.count >= limit {
		return false, nil
	}
	w.count++
	return true, nil
}

// lookup mencari jendela yang masih berlaku di generasi berjalan lalu generasi sebelumnya.
// Jendela dari generasi lama dipromosikan ke generasi berjalan supaya penghitungnya tak hilang
// pada rotasi berikutnya. Pemanggil sudah memegang m.mu.
func (m *Memory) lookup(key string, now time.Time) *window {
	if w, ok := m.cur[key]; ok {
		if now.Before(w.resetAt) {
			return w
		}
		delete(m.cur, key) // kedaluwarsa → biar dibuat ulang oleh pemanggil
		return nil
	}
	w, ok := m.prev[key]
	if !ok || !now.Before(w.resetAt) {
		return nil
	}
	m.cur[key] = w
	return w
}

// rotateIfNeeded membuang generasi terlama saat generasi berjalan menyentuh separuh batas, atau
// saat rotateInterval terlampaui. O(1): satu map dilepas utuh, tak ada pemindaian.
//
// Konsekuensi yang disadari: banjir key unik memaksa rotasi lebih cepat, dan penghitung yang
// hanya ada di generasi lama ikut terbuang — penyerang bisa MELEMAHKAN limiter, meski tak bisa
// menghabiskan memori atau CPU-nya. Menukarnya dengan menolak key baru justru lebih buruk
// (penyerang mematikan login bagi semua orang); yang benar-benar menutupnya adalah store bersama
// ber-TTL (Redis) — titik ekstensi #1, tinggal ganti adapter.
func (m *Memory) rotateIfNeeded(now time.Time) {
	if len(m.cur) < maxEntries/2 && now.Sub(m.rotatedAt) < rotateInterval {
		return
	}
	m.prev = m.cur
	m.cur = make(map[string]*window, len(m.prev)/2+1)
	m.rotatedAt = now
	m.rotations++
}
