package strategy

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/huda-salam/pamong/port"
)

// Registry adalah implementasi port.StrategyRegistry: registry ber-key untuk varian
// strategy, dengan resolusi pilihan per-tenant lewat SelectionSource.
//
// Konvensi key (CLAUDE.md core/strategy): strategy key = {modul}.{titik}.{varian}
// (mis. "keuangan.persediaan.fifo"); decision point = prefiks {modul}.{titik}
// (mis. "keuangan.persediaan") — diturunkan dengan membuang segmen {varian} terakhir.
//
// Impl disimpan sebagai `any` (sesuai port): use case menegaskan tipe ke interface
// decision-point-nya sendiri, atau memakai helper generik ResolveAs[T].
type Registry struct {
	mu        sync.RWMutex
	impls     map[string]any                 // strategyKey -> impl
	byPoint   map[string]map[string]struct{} // decisionPoint -> set(strategyKey)
	defaults  map[string]string              // decisionPoint -> default key (opsional)
	selection SelectionSource
}

// New membuat Registry. selection menentukan key mana yang dipilih tenant untuk sebuah
// decision point (PR-3.3.1: MemorySelectionSource; PR-3.3.2: resolver core/config).
func New(selection SelectionSource) *Registry {
	return &Registry{
		impls:     make(map[string]any),
		byPoint:   make(map[string]map[string]struct{}),
		defaults:  make(map[string]string),
		selection: selection,
	}
}

var _ port.StrategyRegistry = (*Registry)(nil)

// Register mendaftarkan satu varian implementasi di bawah strategy key. Dipanggil saat
// bootstrap/registrasi modul. Key wajib berformat {modul}.{titik}.{varian}; key ganda
// ditolak (bug wiring). impl tidak boleh nil.
func (r *Registry) Register(key string, impl any) error {
	point, err := decisionPointOf(key)
	if err != nil {
		return err
	}
	if impl == nil {
		return ErrInvalidKey(key, "implementasi strategy tidak boleh nil")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.impls[key]; dup {
		return ErrKeyAlreadyRegistered(key)
	}
	r.impls[key] = impl
	keys, ok := r.byPoint[point]
	if !ok {
		keys = make(map[string]struct{})
		r.byPoint[point] = keys
	}
	keys[key] = struct{}{}
	return nil
}

// SetDefault menandai satu key sebagai default untuk decision point-nya — dipakai Resolve
// bila tenant belum menetapkan pilihan (PRD F2). Key wajib sudah terdaftar.
func (r *Registry) SetDefault(key string) error {
	point, err := decisionPointOf(key)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.impls[key]; !ok {
		return ErrKeyNotRegistered(key, point)
	}
	r.defaults[point] = key
	return nil
}

// Resolve mengembalikan implementasi varian yang berlaku untuk tenant pada decision point.
// Urutan: pilihan tenant (SelectionSource) → default developer → error bila keduanya tak
// ada. Pilihan yang merujuk key tak terdaftar untuk point ini ditolak (tanpa fallback diam).
func (r *Registry) Resolve(ctx context.Context, tenantID, point string) (any, error) {
	r.mu.RLock()
	registered := r.byPoint[point]
	def, hasDefault := r.defaults[point]
	r.mu.RUnlock()

	if len(registered) == 0 {
		return nil, ErrUnknownDecisionPoint(point)
	}

	key, ok, err := r.selection.SelectedKey(ctx, tenantID, point)
	if err != nil {
		return nil, err
	}
	if !ok {
		if !hasDefault {
			return nil, ErrNoSelection(tenantID, point)
		}
		key = def
	}

	if _, valid := registered[key]; !valid {
		return nil, ErrKeyNotRegistered(key, point)
	}

	r.mu.RLock()
	impl := r.impls[key]
	r.mu.RUnlock()
	return impl, nil
}

// AvailableOptions mengembalikan daftar strategy key yang boleh dipilih tenant untuk
// decision point, terurut. PR-3.3.1 mengembalikan semua varian terdaftar; irisan dengan
// rule tier nasional/provinsi (opsi yang dilarang regulasi disembunyikan) DEFERRED ke
// PR-3.3.4 (butuh core/rules). tenantID sudah di-parameter agar tanda tangan stabil.
func (r *Registry) AvailableOptions(_ context.Context, _ string, point string) ([]string, error) {
	r.mu.RLock()
	registered := r.byPoint[point]
	r.mu.RUnlock()
	if len(registered) == 0 {
		return nil, ErrUnknownDecisionPoint(point)
	}
	keys := make([]string, 0, len(registered))
	for k := range registered {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys, nil
}

// ResolveAs adalah helper generik: Resolve lalu tegaskan ke tipe T. Menghindari boilerplate
// type-assertion di setiap use case dan memberi error jelas bila impl tak sesuai interface.
func ResolveAs[T any](ctx context.Context, r port.StrategyRegistry, tenantID, point string) (T, error) {
	var zero T
	impl, err := r.Resolve(ctx, tenantID, point)
	if err != nil {
		return zero, err
	}
	typed, ok := impl.(T)
	if !ok {
		return zero, ErrInvalidKey(point,
			"implementasi terdaftar tidak memenuhi interface yang diharapkan use case")
	}
	return typed, nil
}

// decisionPointOf menurunkan decision point dari strategy key dengan membuang segmen
// {varian} terakhir. Memvalidasi format {modul}.{titik}.{varian}: minimal 3 segmen,
// semua non-kosong.
func decisionPointOf(key string) (string, error) {
	if key == "" {
		return "", ErrInvalidKey(key, "key kosong")
	}
	segs := strings.Split(key, ".")
	if len(segs) < 3 {
		return "", ErrInvalidKey(key, "format wajib {modul}.{titik}.{varian} (minimal 3 segmen)")
	}
	for _, s := range segs {
		if s == "" {
			return "", ErrInvalidKey(key, "segmen kosong tidak diperbolehkan")
		}
	}
	last := strings.LastIndex(key, ".")
	return key[:last], nil
}
