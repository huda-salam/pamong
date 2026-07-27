// Package idempotency menyediakan driven adapter Postgres untuk port.IdempotencyStore.
// Seluruh kode yang menyentuh pgx/tenant-pool HANYA ada di infra — gateway/middleware
// bergantung pada port.IdempotencyStore, bukan paket ini.
package idempotency

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	coreIdem "github.com/huda-salam/pamong/core/idempotency"
	"github.com/huda-salam/pamong/infra/db"
	"github.com/huda-salam/pamong/port"
)

// TTL default. Reservasi PENDING sengaja berumur pendek: bila request crash sebelum
// Complete/Release, baris pending tak menyandera key seumur replay window — retry pulih
// setelah pendingTTL. Entri COMPLETED diperpanjang ke replay window (CLAUDE.md: 24 jam).
const (
	defaultPendingTTL   = 2 * time.Minute
	defaultCompletedTTL = 24 * time.Hour
)

// DBStore mengimplementasi port.IdempotencyStore di atas tenant DB (tabel gov.idempotency_keys).
// Pool tenant diresolusi per-request dari TenantConnManager (DB-per-tenant); skema dipastikan
// sekali per tenant per proses.
type DBStore struct {
	connMgr      *db.TenantConnManager
	pendingTTL   time.Duration
	completedTTL time.Duration

	ensuredMu sync.Mutex
	ensured   map[string]bool // tenantID → skema gov.idempotency_keys sudah dipastikan
}

// NewDBStore membuat store dengan TTL default.
func NewDBStore(connMgr *db.TenantConnManager) *DBStore {
	return &DBStore{
		connMgr:      connMgr,
		pendingTTL:   defaultPendingTTL,
		completedTTL: defaultCompletedTTL,
		ensured:      make(map[string]bool),
	}
}

var _ port.IdempotencyStore = (*DBStore)(nil)

// pool meresolusi pool tenant & memastikan skema tabel ada (sekali per tenant).
func (s *DBStore) pool(ctx context.Context, tenantID string) (*db.Pool, error) {
	pool, err := s.connMgr.Tenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if err := s.ensureSchema(ctx, tenantID, pool); err != nil {
		return nil, err
	}
	return pool, nil
}

// ensureSchema menerapkan migrasi tabel idempotency ke tenant DB sekali per proses. Aman bila
// dua request pertama untuk tenant sama menerapkan bersamaan: ApplyEmbeddedSchema idempoten &
// ber-advisory-lock (paling banter redundan).
func (s *DBStore) ensureSchema(ctx context.Context, tenantID string, pool *db.Pool) error {
	s.ensuredMu.Lock()
	done := s.ensured[tenantID]
	s.ensuredMu.Unlock()
	if done {
		return nil
	}
	if err := db.ApplyEmbeddedSchema(ctx, pool, coreIdem.MigrationModule, coreIdem.MigrationsFS); err != nil {
		return err
	}
	s.ensuredMu.Lock()
	s.ensured[tenantID] = true
	s.ensuredMu.Unlock()
	return nil
}

// Reserve mengklaim (personID, key) secara atomik. INSERT langsung berhasil bila belum ada;
// bila bentrok dengan baris KEDALUWARSA, DO UPDATE mengambil-alih (reset ke pending) karena
// WHERE ik.expires_at <= now() terpenuhi; bila bentrok dengan baris VALID, WHERE gagal →
// tidak ada baris di-RETURNING → kita ambil entri yang ada untuk replay / deteksi in-flight.
func (s *DBStore) Reserve(ctx context.Context, tenantID string, personID uuid.UUID, key, fingerprint string) (*port.IdempotencyRecord, bool, error) {
	pool, err := s.pool(ctx, tenantID)
	if err != nil {
		return nil, false, err
	}

	// gov:raw-ok reason=idempotency-atomic-claim query=idempotency-reserve
	row := pool.QueryRow(ctx, `
		INSERT INTO gov.idempotency_keys AS ik (person_id, key, fingerprint, expires_at)
		VALUES ($1, $2, $3, now() + make_interval(secs => $4))
		ON CONFLICT (person_id, key) DO UPDATE
			SET fingerprint = EXCLUDED.fingerprint,
			    status      = NULL,
			    response    = NULL,
			    completed   = false,
			    created_at  = now(),
			    expires_at  = EXCLUDED.expires_at
			WHERE ik.expires_at <= now()
		RETURNING completed`, personID, key, fingerprint, s.pendingTTL.Seconds())

	var completed bool
	if err := row.Scan(&completed); err == nil {
		// Ada baris di-RETURNING → reservasi berhasil (insert baru atau ambil-alih kedaluwarsa).
		return nil, true, nil
	} else if !db.IsNoRows(err) {
		return nil, false, err
	}

	// Tidak ada baris → sudah ada entri VALID (belum kedaluwarsa). Ambil untuk replay/in-flight.
	// gov:raw-ok reason=idempotency-fetch-existing query=idempotency-select
	existing := pool.QueryRow(ctx, `
		SELECT fingerprint, status, response, completed
		FROM gov.idempotency_keys
		WHERE person_id = $1 AND key = $2`, personID, key)

	rec := &port.IdempotencyRecord{}
	var status *int
	var body []byte
	if err := existing.Scan(&rec.Fingerprint, &status, &body, &rec.Completed); err != nil {
		if db.IsNoRows(err) {
			// Balapan langka: baris kedaluwarsa diambil/dihapus proses lain antara INSERT & SELECT.
			// Fail-closed transient → caller (middleware) menolak agar bisa di-retry dengan aman.
			return nil, false, fmt.Errorf("idempotency: reservasi berbenturan, coba lagi")
		}
		return nil, false, err
	}
	if status != nil {
		rec.Status = *status
	}
	rec.Body = body
	return rec, false, nil
}

// Complete menyimpan respons final & memperpanjang masa simpan ke replay window.
func (s *DBStore) Complete(ctx context.Context, tenantID string, personID uuid.UUID, key string, status int, body []byte) error {
	pool, err := s.pool(ctx, tenantID)
	if err != nil {
		return err
	}
	// gov:raw-ok reason=idempotency-complete query=idempotency-complete
	_, err = pool.Exec(ctx, `
		UPDATE gov.idempotency_keys
		SET status = $3, response = $4, completed = true,
		    expires_at = now() + make_interval(secs => $5)
		WHERE person_id = $1 AND key = $2`, personID, key, status, body, s.completedTTL.Seconds())
	return err
}

// Release menghapus reservasi yang belum selesai (respons gagal/panic) agar key bisa dipakai
// ulang untuk retry. completed=true dijaga agar tak menghapus entri yang sudah bisa di-replay.
func (s *DBStore) Release(ctx context.Context, tenantID string, personID uuid.UUID, key string) error {
	pool, err := s.pool(ctx, tenantID)
	if err != nil {
		return err
	}
	// gov:raw-ok reason=idempotency-release query=idempotency-release
	_, err = pool.Exec(ctx, `
		DELETE FROM gov.idempotency_keys
		WHERE person_id = $1 AND key = $2 AND completed = false`, personID, key)
	return err
}
