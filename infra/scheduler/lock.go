package scheduler

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	coreSched "github.com/huda-salam/pamong/core/scheduler"
	"github.com/huda-salam/pamong/infra/db"
)

// jobLockDDL membuat tabel sewa lock. Satu baris per key; locked_until adalah batas sewa.
// Baris kedaluwarsa (locked_until < now) dianggap bebas dan bisa diambil alih — ini yang
// mencegah deadlock permanen bila instance pemegang mati (PRD F3, non-fungsional TTL).
const jobLockDDL = `
CREATE SCHEMA IF NOT EXISTS gov;
CREATE TABLE IF NOT EXISTS gov.job_locks (
    lock_key     TEXT PRIMARY KEY,
    token        TEXT        NOT NULL,
    locked_until TIMESTAMPTZ NOT NULL
);`

// DBLocker mengimplementasi coreSched.Locker di atas Postgres — lock terdistribusi ber-sewa
// untuk lingkungan multi-instance. Acquire bersifat atomik lewat INSERT .. ON CONFLICT dengan
// guard kedaluwarsa, sehingga tepat satu instance menang saat balapan.
type DBLocker struct {
	pool *db.Pool
	now  func() time.Time
}

var _ coreSched.Locker = (*DBLocker)(nil)

// NewDBLocker membuat locker. Panggil EnsureSchema sebelum dipakai.
func NewDBLocker(pool *db.Pool) *DBLocker { return &DBLocker{pool: pool, now: time.Now} }

// EnsureSchema membuat tabel gov.job_locks bila belum ada. Idempoten.
func (l *DBLocker) EnsureSchema(ctx context.Context) error {
	_, err := l.pool.Exec(ctx, jobLockDDL)
	return err
}

// Acquire mengambil lock secara atomik. Menang bila belum ada baris (INSERT) atau baris
// yang ada sudah kedaluwarsa (DO UPDATE dengan guard locked_until < now). Bila baris masih
// aktif, ON CONFLICT DO UPDATE tidak mengubah apa pun dan tidak RETURNING — dianggap gagal.
func (l *DBLocker) Acquire(ctx context.Context, key string, ttl time.Duration) (coreSched.Lock, bool, error) {
	token := uuid.NewString()
	until := l.now().Add(ttl)
	// gov:raw-ok reason=atomic-lease-acquire query=scheduler-lock-acquire
	row := l.pool.QueryRow(ctx, `
		INSERT INTO gov.job_locks (lock_key, token, locked_until)
		VALUES ($1, $2, $3)
		ON CONFLICT (lock_key) DO UPDATE
			SET token = EXCLUDED.token, locked_until = EXCLUDED.locked_until
			WHERE gov.job_locks.locked_until < $4
		RETURNING token`, key, token, until, l.now())
	var got string
	err := row.Scan(&got)
	if errors.Is(err, pgx.ErrNoRows) {
		return coreSched.Lock{}, false, nil // masih dipegang & belum kedaluwarsa
	}
	if err != nil {
		return coreSched.Lock{}, false, fmt.Errorf("acquire lock %q: %w", key, err)
	}
	return coreSched.Lock{Key: key, Token: got}, true, nil
}

// Release melepas lock hanya bila token cocok (pemegang saat ini). Token tak cocok di-abaikan.
func (l *DBLocker) Release(ctx context.Context, lock coreSched.Lock) error {
	// gov:raw-ok reason=guarded-release query=scheduler-lock-release
	_, err := l.pool.Exec(ctx,
		`DELETE FROM gov.job_locks WHERE lock_key = $1 AND token = $2`, lock.Key, lock.Token)
	if err != nil {
		return fmt.Errorf("release lock %q: %w", lock.Key, err)
	}
	return nil
}
