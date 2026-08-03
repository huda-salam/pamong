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
	"github.com/huda-salam/pamong/infra/crypto"
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

// purposeResponse adalah purpose kunci untuk kolom `response` (badan respons tersimpan).
const purposeResponse = "idempotency_response"

// DBStore mengimplementasi port.IdempotencyStore di atas tenant DB (tabel gov.idempotency_keys).
// Pool tenant diresolusi per-request dari TenantConnManager (DB-per-tenant); skema dipastikan
// sekali per tenant per proses.
//
// Kolom `response` disimpan TERENKRIPSI (ADR-009 §6 butir 3): ia badan respons API yang
// utuh, jadi ia memuat apa pun yang di-echo endpoint mutasi — termasuk NIK/NIP/email pada
// respons use case identity. Mengenkripsi kolomnya sendiri sambil membiarkan cache replay
// menyimpan salinan plaintext-nya selama 24 jam adalah teater keamanan.
//
// Kolom `fingerprint` TIDAK disegel: ia SHA-256 atas (method+path+body), bukan nilai mentah.
// Ia tetap oracle kesamaan atas request utuh — diterima, karena menyegelnya akan mematikan
// satu-satunya gunanya (dibandingkan saat Reserve, sebelum baris apa pun dibuka).
type DBStore struct {
	connMgr      *db.TenantConnManager
	crypto       port.CryptoPort
	pendingTTL   time.Duration
	completedTTL time.Duration

	ensuredMu sync.Mutex
	ensured   map[string]bool // tenantID → skema gov.idempotency_keys sudah dipastikan

	sealers sync.Map // tenantID → *crypto.FieldSealer (realm = tenant, seperti clone)
}

// NewDBStore membuat store dengan TTL default. CryptoPort nil DITOLAK: store tanpa kripto
// menyimpan badan respons plaintext tanpa satu pun gejala sampai seseorang membuka dump —
// penolakan yang sama dengan NewTenantDBWriter & infra/db.NewRepository, dan di titik yang
// sama (konstruksi, bukan penulisan pertama).
func NewDBStore(connMgr *db.TenantConnManager, c port.CryptoPort) (*DBStore, error) {
	if c == nil {
		return nil, fmt.Errorf("infra/idempotency: cache respons butuh port.CryptoPort (ADR-009 §6)")
	}
	return &DBStore{
		connMgr:      connMgr,
		crypto:       c,
		pendingTTL:   defaultPendingTTL,
		completedTTL: defaultCompletedTTL,
		ensured:      make(map[string]bool),
	}, nil
}

var _ port.IdempotencyStore = (*DBStore)(nil)

// sealer mengembalikan sealer ber-realm TENANT. Alasannya sama dengan clone (ADR-017):
// gov.idempotency_keys hidup DI DALAM tenant DB, jadi ia dilindungi kunci yang sama dengan
// sisa DB itu — realm sentral di sini berarti satu kunci membuka cache respons seluruh pemda.
func (s *DBStore) sealer(tenantID string) (*crypto.FieldSealer, error) {
	if v, ok := s.sealers.Load(tenantID); ok {
		return v.(*crypto.FieldSealer), nil
	}
	sl, err := crypto.NewFieldSealer(s.crypto, tenantID, "infra/idempotency")
	if err != nil {
		return nil, err
	}
	actual, _ := s.sealers.LoadOrStore(tenantID, sl)
	return actual.(*crypto.FieldSealer), nil
}

// recordRef menurunkan identitas BARIS untuk AAD (ADR-016). Tabel ini ber-PK gabungan
// (person_id, key) tanpa kolom UUID, jadi koordinatnya dibangun deterministik dari kedua
// bagian PK itu — bukan dari person_id saja: dengan person_id saja, respons boleh dipindah
// antar key MILIK ORANG YANG SAMA dan tetap terbuka, dan `fingerprint` tak menolong karena
// ia ikut berpindah dalam baris yang sama. Namespace URL + separator eksplisit menjaga
// (person, "a|b") tak bertabrakan dengan (person, "a", "b").
func recordRef(personID uuid.UUID, key string) uuid.UUID {
	return uuid.NewSHA1(personID, []byte("gov.idempotency_keys\x00"+key))
}

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

	// Badan respons hanya dipakai pada SATU cabang caller: replay (entri selesai DAN
	// fingerprint-nya sama). Dua cabang lain — key dipakai-ulang untuk request BERBEDA, dan
	// kembar yang masih in-flight — lahir dari Fingerprint & Completed saja. Membuka blob di
	// luar cabang replay hanya memindahkan kegagalan kripto (baris pra-3.8.5b yang masih
	// plaintext, versi kunci yang hilang, blob rusak) ke dua verdict yang tak butuh isinya:
	// keduanya berubah menjadi 503 retryable selama sisa TTL, padahal 422 adalah jawaban
	// FINAL yang benar dan retry tak akan pernah menolong.
	if !rec.Completed || rec.Fingerprint != fingerprint {
		return rec, false, nil
	}

	// Di cabang replay, gagal membuka TIDAK boleh berubah jadi "respons kosong": klien akan
	// menerima 200 berbadan kosong sebagai jawaban final yang sah, dan request mutasinya tak
	// akan pernah dijalankan ulang. Gagal lantang → middleware fail-closed 503, retry aman.
	sl, err := s.sealer(tenantID)
	if err != nil {
		return nil, false, err
	}
	plain, err := sl.Open(ctx, purposeResponse, recordRef(personID, key), body)
	if err != nil {
		return nil, false, err
	}
	if plain != "" {
		rec.Body = []byte(plain)
	}
	return rec, false, nil
}

// Complete menyimpan respons final (TERENKRIPSI) & memperpanjang masa simpan ke replay window.
func (s *DBStore) Complete(ctx context.Context, tenantID string, personID uuid.UUID, key string, status int, body []byte) error {
	pool, err := s.pool(ctx, tenantID)
	if err != nil {
		return err
	}
	sl, err := s.sealer(tenantID)
	if err != nil {
		return err
	}
	// Disegel SEBELUM menyentuh DB: kegagalan kripto harus berarti "tak ada yang tersimpan",
	// bukan "tersimpan plaintext".
	sealed, err := sl.SealOpaque(ctx, purposeResponse, recordRef(personID, key), string(body))
	if err != nil {
		return err
	}
	// gov:raw-ok reason=idempotency-complete query=idempotency-complete
	_, err = pool.Exec(ctx, `
		UPDATE gov.idempotency_keys
		SET status = $3, response = $4, completed = true,
		    expires_at = now() + make_interval(secs => $5)
		WHERE person_id = $1 AND key = $2`, personID, key, status, sealed, s.completedTTL.Seconds())
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
