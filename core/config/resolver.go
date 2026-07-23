package config

import (
	"context"
	"time"
)

// Resolver menyelesaikan tenant config ber-scope dengan dua aturan sekaligus:
//  1. "paling spesifik menang" antar level scope (resource > unit > tenant), dan
//  2. "versi efektif terbaru pada tanggal" untuk pilihan ber-versi (append-only + effective
//     date, PR-3.3.3) — pilihan lama tetap berlaku untuk tanggal lama (non-retroaktif).
//
// Logika pemilihan murni & deterministik (tidak menyentuh DB) sehingga bisa diuji tanpa
// koneksi — store hanya menyediakan seluruh versi kandidat.
type Resolver struct {
	store TenantConfigStore
	now   func() time.Time
}

// NewResolver membuat resolver di atas store tertentu.
func NewResolver(store TenantConfigStore) *Resolver {
	return &Resolver{store: store, now: time.Now}
}

// Resolve mengembalikan nilai config yang berlaku SEKARANG untuk scope pada key tertentu.
// Setara ResolveAsOf(now). ok=false bila tidak ada versi yang cocok.
func (r *Resolver) Resolve(ctx context.Context, scope ConfigScope, key string) (value string, ok bool, err error) {
	return r.ResolveAsOf(ctx, scope, key, r.now())
}

// ResolveAsOf mengembalikan nilai config yang berlaku pada tanggal asOf. Ini inti
// non-retroaktivitas: mengganti pilihan (versi baru dgn effective_from di masa depan) tidak
// mengubah hasil untuk tanggal yang sudah lewat. Error hanya untuk kegagalan store.
func (r *Resolver) ResolveAsOf(ctx context.Context, scope ConfigScope, key string, asOf time.Time) (string, bool, error) {
	cands, err := r.store.Candidates(ctx, scope.TenantID, key)
	if err != nil {
		return "", false, err
	}
	best, found := mostSpecificAsOf(cands, scope, asOf)
	if !found {
		return "", false, nil
	}
	return best.Value, true, nil
}

// Set menambah satu versi config lewat store — jalur SEED/framework (set_by nil, tanpa gerbang
// fiskal). Untuk perubahan pilihan oleh aktor manusia yang ber-audit & non-retroaktif ber-gerbang
// periode, pakai ChoiceManager.SetChoice.
func (r *Resolver) Set(ctx context.Context, entry ConfigEntry) error {
	return r.store.Set(ctx, entry)
}

// mostSpecificAsOf memilih versi yang menang di antara kandidat: hanya yang scope-nya prefiks
// query DAN sudah berlaku pada asOf (effective_from <= asOf); di antara itu, scope terdalam
// menang, lalu effective_from terbaru, lalu versi tertinggi (tie-break deterministik).
//
// Urutan (spesifisitas dulu, baru tanggal) disengaja: override unit-kerja tetap menang atas
// tenant meski pilihan tenant lebih baru — kekhususan scope lebih kuat daripada kebaruan.
func mostSpecificAsOf(cands []ConfigEntry, query ConfigScope, asOf time.Time) (ConfigEntry, bool) {
	var best ConfigEntry
	found := false
	for _, e := range cands {
		if !e.Scope.appliesTo(query) || e.EffectiveFrom.After(asOf) {
			continue
		}
		if !found || moreWinning(e, best) {
			best, found = e, true
		}
	}
	return best, found
}

// moreWinning melaporkan apakah a mengalahkan b: spesifisitas lebih tinggi, atau sama tapi
// effective_from lebih baru, atau sama lagi tapi versi lebih tinggi.
func moreWinning(a, b ConfigEntry) bool {
	sa, sb := a.Scope.specificity(), b.Scope.specificity()
	if sa != sb {
		return sa > sb
	}
	if !a.EffectiveFrom.Equal(b.EffectiveFrom) {
		return a.EffectiveFrom.After(b.EffectiveFrom)
	}
	return a.Version > b.Version
}
