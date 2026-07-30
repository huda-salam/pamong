package crypto

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// namedProvider memasangkan KeyProvider dengan nama driver-nya. Nama disimpan ke kolom
// kek_driver agar baris DEK lama tetap bisa didiagnosa setelah KMS diganti.
type namedProvider struct {
	name     string
	provider KeyProvider
}

// keyManager adalah hierarki KEK→DEK (ADR-010 §2): ia meminta DEK ke store (ter-wrap),
// membukanya lewat KeyProvider sesuai custody tenant, lalu menyimpan DEK ter-decrypt di
// cache in-process ber-TTL.
//
// Cache adalah kebutuhan, bukan optimasi prematur: tanpa itu setiap baris yang dienkripsi
// memicu satu query ke DB sentral + satu operasi KMS (PRD non-fungsional: "hindari
// round-trip KMS per-baris"). Konsekuensi yang disadari: DEK ter-decrypt ada di memori
// proses selama TTL. Ia tak pernah ditulis ke DB tenant maupun di-log.
type keyManager struct {
	store     DEKStore
	custody   CustodyResolver
	providers map[Custody]namedProvider
	ttl       time.Duration
	now       func() time.Time

	mu     sync.Mutex
	deks   map[dekCacheKey][]byte     // kunci ter-decrypt per versi
	expiry map[dekCacheKey]time.Time  // umur entri deks
	active map[refCacheKey]activeInfo // versi aktif per (tenant, purpose, kind)
}

type dekCacheKey struct {
	tenantID string
	purpose  string
	kind     KeyKind
	version  int
}

type refCacheKey struct {
	tenantID string
	purpose  string
	kind     KeyKind
}

type activeInfo struct {
	version   int
	expiresAt time.Time
}

func newKeyManager(store DEKStore, custody CustodyResolver, providers map[Custody]namedProvider, ttl time.Duration) *keyManager {
	return &keyManager{
		store:     store,
		custody:   custody,
		providers: providers,
		ttl:       ttl,
		now:       time.Now,
		deks:      map[dekCacheKey][]byte{},
		expiry:    map[dekCacheKey]time.Time{},
		active:    map[refCacheKey]activeInfo{},
	}
}

// providerFor memilih KeyProvider untuk sebuah mode custody. Tak ada fallback ke platform:
// tenant yang memilih memegang kuncinya sendiri harus GAGAL LANTANG bila provider-nya belum
// terpasang (ErrCustodyUnsupported), bukan diam-diam dilayani kunci platform.
func (m *keyManager) providerFor(custody Custody) (namedProvider, error) {
	np, ok := m.providers[custody]
	if !ok {
		return namedProvider{}, fmt.Errorf("%w: custody=%q (terpasang: %s)", ErrCustodyUnsupported, custody, m.custodyModes())
	}
	return np, nil
}

func (m *keyManager) custodyModes() string {
	modes := make([]string, 0, len(m.providers))
	for c := range m.providers {
		modes = append(modes, string(c))
	}
	if len(modes) == 0 {
		return "-"
	}
	return joinSorted(modes)
}

// ActiveKey mengembalikan versi kunci aktif beserta DEK ter-decrypt untuk MENULIS. Bila tenant
// belum punya kunci untuk (purpose, kind), kunci pertama dibuat di sini — provisioning kunci
// terjadi otomatis pada penggunaan pertama, bukan lewat langkah manual yang bisa terlewat.
func (m *keyManager) ActiveKey(ctx context.Context, tenantID, purpose string, kind KeyKind) (int, []byte, error) {
	rk := refCacheKey{tenantID, purpose, kind}
	if version, ok := m.cachedActive(rk); ok {
		if dek, ok := m.cachedDEK(dekCacheKey{tenantID, purpose, kind, version}); ok {
			return version, dek, nil
		}
	}

	custody, err := m.custody.Custody(ctx, tenantID)
	if err != nil {
		return 0, nil, err
	}
	np, err := m.providerFor(custody)
	if err != nil {
		return 0, nil, err
	}
	ref := KeyRef{TenantID: tenantID, Purpose: purpose, Kind: kind, Custody: custody}

	rec, found, err := m.store.Active(ctx, ref)
	if err != nil {
		return 0, nil, err
	}
	if !found {
		// Kunci pertama untuk (tenant, purpose, kind).
		_, wrapped, err := np.provider.GenerateDEK(ctx, ref)
		if err != nil {
			return 0, nil, err
		}
		rec, err = m.store.InsertActive(ctx, ref, DEKRecord{
			Version: 1, Wrapped: wrapped, Custody: custody, KEKDriver: np.name,
		})
		if err != nil {
			return 0, nil, err
		}
		// Sengaja TIDAK memakai DEK hasil GenerateDEK: bila proses lain menang balapan,
		// baris pemenang-lah yang otoritatif. Unwrap di bawah memakai rec — satu jalur,
		// tanpa cabang "menang/kalah" yang bisa menulis dengan kunci berbeda.
	}

	dek, err := m.unwrap(ctx, ref, rec)
	if err != nil {
		return 0, nil, err
	}
	m.cache(dekCacheKey{tenantID, purpose, kind, rec.Version}, dek, rk, rec.Version)
	return rec.Version, dek, nil
}

// KeyByVersion mengambil DEK versi tertentu untuk MEMBACA ciphertext (versinya dibawa
// ciphertext itu sendiri) — inilah yang membuat rotasi kunci enkripsi bisa lazy.
func (m *keyManager) KeyByVersion(ctx context.Context, tenantID, purpose string, kind KeyKind, version int) ([]byte, error) {
	ck := dekCacheKey{tenantID, purpose, kind, version}
	if dek, ok := m.cachedDEK(ck); ok {
		return dek, nil
	}

	custody, err := m.custody.Custody(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	ref := KeyRef{TenantID: tenantID, Purpose: purpose, Kind: kind, Custody: custody}
	rec, found, err := m.store.ByVersion(ctx, ref, version)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("%w: %s/%s/%s versi %d", ErrDEKMissing, tenantID, purpose, kind, version)
	}

	dek, err := m.unwrap(ctx, ref, rec)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.deks[ck] = dek
	m.expiry[ck] = m.now().Add(m.ttl)
	m.mu.Unlock()
	return dek, nil
}

// unwrap membuka DEK memakai provider yang MEMBUNGKUS baris itu (rec.Custody), bukan custody
// tenant saat ini. Ini yang membuat data lama tetap terbaca setelah custody sebuah tenant
// berpindah.
//
// Yang TIDAK dilakukan (sengaja, agar tak ada janji palsu): mengubah key_custody TIDAK
// membuat tulisan baru otomatis memakai KEK custody baru. `ActiveKey` memakai baris DEK aktif
// yang sudah ada apa adanya — ia tak tahu baris itu dibungkus custody lain. Perpindahan
// custody yang sesungguhnya butuh operasi RE-WRAP eksplisit (unwrap dengan provider lama,
// wrap dengan provider baru, simpan sebagai versi baru + nonaktifkan versi lama) yang belum
// ada; itu bagian PR-3.8.8 bersama driver custody `tenant`. Sampai saat itu hanya mode
// `platform` yang punya provider, jadi situasi ini belum bisa terjadi di produksi.
func (m *keyManager) unwrap(ctx context.Context, ref KeyRef, rec DEKRecord) ([]byte, error) {
	np, err := m.providerFor(rec.Custody)
	if err != nil {
		return nil, err
	}
	wrapRef := ref
	wrapRef.Custody = rec.Custody
	dek, err := np.provider.UnwrapDEK(ctx, wrapRef, rec.Wrapped)
	if err != nil {
		return nil, err
	}
	if len(dek) != dekLen {
		return nil, fmt.Errorf("crypto: DEK %s/%s/%s versi %d berukuran %d byte, bukan %d",
			ref.TenantID, ref.Purpose, ref.Kind, rec.Version, len(dek), dekLen)
	}
	return dek, nil
}

func (m *keyManager) cache(ck dekCacheKey, dek []byte, rk refCacheKey, version int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	m.deks[ck] = dek
	m.expiry[ck] = now.Add(m.ttl)
	m.active[rk] = activeInfo{version: version, expiresAt: now.Add(m.ttl)}
}

func (m *keyManager) cachedDEK(ck dekCacheKey) ([]byte, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	dek, ok := m.deks[ck]
	if !ok || !m.now().Before(m.expiry[ck]) {
		return nil, false
	}
	return dek, true
}

func (m *keyManager) cachedActive(rk refCacheKey) (int, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	info, ok := m.active[rk]
	if !ok || !m.now().Before(info.expiresAt) {
		return 0, false
	}
	return info.version, true
}
