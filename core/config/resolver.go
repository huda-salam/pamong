package config

import "context"

// Resolver menyelesaikan tenant config ber-scope dengan aturan "paling spesifik menang":
// untuk sebuah (scope query, key), ia mengambil semua entry kandidat dari store lalu memilih
// entry ber-scope terdalam yang masih menjadi prefiks dari scope query. Resource-level
// mengalahkan unit-level, unit-level mengalahkan tenant-level.
//
// Logika pemilihan murni & deterministik (tidak menyentuh DB) sehingga bisa diuji tanpa
// koneksi — store hanya menyediakan kandidat. Ini pemakai utama titik ekstensi #2 CLAUDE.md.
type Resolver struct {
	store TenantConfigStore
}

// NewResolver membuat resolver di atas store tertentu.
func NewResolver(store TenantConfigStore) *Resolver {
	return &Resolver{store: store}
}

// Resolve mengembalikan nilai config yang berlaku untuk scope pada key tertentu.
// ok=false bila tidak ada entry yang cocok (pemakai lalu memakai default-nya sendiri).
// Error hanya untuk kegagalan store.
func (r *Resolver) Resolve(ctx context.Context, scope ConfigScope, key string) (value string, ok bool, err error) {
	cands, err := r.store.Candidates(ctx, scope.TenantID, key)
	if err != nil {
		return "", false, err
	}
	best, found := mostSpecific(cands, scope)
	if !found {
		return "", false, nil
	}
	return best.Value, true, nil
}

// Set menyimpan satu nilai config ter-scope lewat store.
func (r *Resolver) Set(ctx context.Context, entry ConfigEntry) error {
	return r.store.Set(ctx, entry)
}

// mostSpecific memilih entry ber-scope terdalam di antara kandidat yang berlaku untuk query.
// Kandidat yang scope-nya bukan prefiks query diabaikan. Bila tak ada yang cocok → found=false.
func mostSpecific(cands []ConfigEntry, query ConfigScope) (ConfigEntry, bool) {
	var best ConfigEntry
	bestScore := -1
	for _, e := range cands {
		if !e.Scope.appliesTo(query) {
			continue
		}
		if s := e.Scope.specificity(); s > bestScore {
			best, bestScore = e, s
		}
	}
	return best, bestScore >= 0
}
