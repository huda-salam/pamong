package crypto

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/huda-salam/pamong/port"
)

// CustodyResolver menjawab "siapa pegang KEK untuk tenant ini" (ADR-010 §3). Ia disengaja
// menjadi seam terpisah: menambah mode custody baru = mendaftarkan KeyProvider + mengisi
// kolom key_custody, TANPA menyentuh kode enkripsi.
type CustodyResolver interface {
	Custody(ctx context.Context, tenantID string) (Custody, error)
}

// FixedCustody memberi custody yang sama untuk semua tenant. Dipakai dev/test dan deployment
// single-custody; produksi multi-tenant memakai DBCustodyResolver agar kebijakan per-tenant
// benar-benar per-tenant.
type FixedCustody Custody

func (f FixedCustody) Custody(context.Context, string) (Custody, error) { return Custody(f), nil }

// DBCustodyResolver membaca id.tenant_registry.key_custody (identity DB, sentral).
//
// Cache ber-TTL: custody nyaris tak pernah berubah (keputusan kontraktual), tapi ia dibaca
// pada setiap operasi kripto — tanpa cache, tiap baris yang dienkripsi menambah satu query
// ke DB sentral. TTL menjaga perubahan tetap terlihat tanpa restart.
type DBCustodyResolver struct {
	conn port.DBConn
	ttl  time.Duration
	now  func() time.Time

	mu    sync.Mutex
	cache map[string]custodyEntry
}

type custodyEntry struct {
	custody   Custody
	expiresAt time.Time
}

func NewDBCustodyResolver(identityConn port.DBConn, ttl time.Duration) *DBCustodyResolver {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &DBCustodyResolver{conn: identityConn, ttl: ttl, now: time.Now, cache: map[string]custodyEntry{}}
}

func (r *DBCustodyResolver) Custody(ctx context.Context, tenantID string) (Custody, error) {
	if c, ok := r.cached(tenantID); ok {
		return c, nil
	}

	var custody string
	err := r.conn.QueryRow(ctx,
		`SELECT key_custody FROM id.tenant_registry WHERE tenant_id = $1`, tenantID).Scan(&custody)
	if err != nil {
		if isNoRows(err) {
			// Fail-closed: tenant tak terdaftar tidak boleh mendapat kunci apa pun.
			return "", fmt.Errorf("crypto: tenant %q tidak ada di registry — custody tak bisa ditentukan", tenantID)
		}
		return "", fmt.Errorf("crypto: baca key_custody tenant %q: %w", tenantID, err)
	}

	r.mu.Lock()
	r.cache[tenantID] = custodyEntry{custody: Custody(custody), expiresAt: r.now().Add(r.ttl)}
	r.mu.Unlock()
	return Custody(custody), nil
}

func (r *DBCustodyResolver) cached(tenantID string) (Custody, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.cache[tenantID]
	if !ok || !r.now().Before(e.expiresAt) {
		return "", false
	}
	return e.custody, true
}
