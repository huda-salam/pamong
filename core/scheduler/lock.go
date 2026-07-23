package scheduler

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Lock adalah bukti kepemilikan sewa (lease) atas sebuah key. Token unik per-pengambilan:
// hanya pemegang token yang boleh melepas — mencegah instance A melepas lock yang sudah
// kedaluwarsa dan kini dipegang instance B.
type Lock struct {
	Key   string
	Token string
}

// Locker adalah driven port lock terdistribusi ber-sewa (lease). Dipakai Runner agar satu
// job tidak jalan ganda di multi-instance (PRD F3). Sewa punya TTL: bila instance pemegang
// mati, lock kedaluwarsa dan instance lain bisa mengambil — tidak ada deadlock permanen.
//
// Implementasi: MemoryLocker (single-proses, test) & infra/scheduler.DBLocker (Postgres,
// produksi multi-instance). Core tak tahu detail — hexagonal.
type Locker interface {
	// Acquire mencoba mengambil lock atas key dengan masa sewa ttl. Mengembalikan
	// (lock, true, nil) bila berhasil; (_, false, nil) bila key sedang dipegang & belum
	// kedaluwarsa. Acquire pada lock yang sudah kedaluwarsa berhasil (mengambil alih).
	Acquire(ctx context.Context, key string, ttl time.Duration) (Lock, bool, error)

	// Release melepas lock. Hanya berlaku bila token cocok dengan pemegang saat ini;
	// token yang tak cocok (mis. sudah diambil alih) di-abaikan tanpa error.
	Release(ctx context.Context, lock Lock) error
}

// jobLockKey menurunkan key lock dari ID jadwal — satu lock per jadwal.
func jobLockKey(id uuid.UUID) string { return "scheduler:job:" + id.String() }

// MemoryLocker adalah Locker in-memory untuk test & dev single-proses. Bukan untuk
// produksi multi-instance (state tak dibagi lintas proses) — gunakan infra/scheduler.DBLocker.
type MemoryLocker struct {
	mu    sync.Mutex
	locks map[string]memLease
	now   func() time.Time
}

type memLease struct {
	token string
	until time.Time
}

// NewMemoryLocker membuat locker kosong.
func NewMemoryLocker() *MemoryLocker {
	return &MemoryLocker{locks: make(map[string]memLease), now: time.Now}
}

var _ Locker = (*MemoryLocker)(nil)

func (l *MemoryLocker) Acquire(_ context.Context, key string, ttl time.Duration) (Lock, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if cur, ok := l.locks[key]; ok && cur.until.After(now) {
		return Lock{}, false, nil // masih dipegang & belum kedaluwarsa
	}
	token := uuid.NewString()
	l.locks[key] = memLease{token: token, until: now.Add(ttl)}
	return Lock{Key: key, Token: token}, true, nil
}

func (l *MemoryLocker) Release(_ context.Context, lock Lock) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if cur, ok := l.locks[lock.Key]; ok && cur.token == lock.Token {
		delete(l.locks, lock.Key)
	}
	return nil
}
