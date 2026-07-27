package port

import (
	"context"

	"github.com/google/uuid"
)

// IdempotencyRecord adalah entri idempotency tersimpan untuk satu (principal, key).
// Status & Body hanya bermakna bila Completed = true (respons sudah final & bisa di-replay).
type IdempotencyRecord struct {
	Fingerprint string // hash(method+path+body) — deteksi key dipakai untuk request BERBEDA
	Status      int    // HTTP status respons tersimpan (0 bila belum Completed)
	Body        []byte // body respons tersimpan (nil bila belum Completed)
	Completed   bool   // true bila handler selesai & respons tersimpan (siap di-replay)
}

// IdempotencyStore menyimpan hasil request mutasi ber-Idempotency-Key sehingga request
// duplikat (key sama) mengembalikan respons yang sama tanpa efek ganda (CLAUDE.md §Data
// integrity — framework yang handle di middleware level, bukan use case).
//
// Store bersifat per-tenant (tabel gov.idempotency_keys di tenant DB). Key di-scope ke
// PRINCIPAL (person_id), bukan hanya nilai key: satu user tak boleh bisa membaca atau menimpa
// respons user lain dengan menebak/menggunakan-ulang nilai key yang sama.
type IdempotencyStore interface {
	// Reserve mengklaim (personID, key) untuk request ini secara ATOMIK:
	//   reserved=true  → klaim baru (atau ambil-alih reservasi yang sudah kedaluwarsa). Caller
	//                    menjalankan handler lalu memanggil Complete (sukses) / Release (gagal).
	//                    record = nil.
	//   reserved=false → sudah ada entri VALID. record berisi entri tsb: bila Completed → replay
	//                    (Status/Body); bila belum → request kembar masih in-flight. Caller TIDAK
	//                    menjalankan handler.
	// Fingerprint dipakai caller untuk mendeteksi key yang dipakai-ulang untuk request berbeda.
	Reserve(ctx context.Context, tenantID string, personID uuid.UUID, key, fingerprint string) (record *IdempotencyRecord, reserved bool, err error)

	// Complete menyimpan respons final untuk reservasi & memperpanjang masa simpan ke replay
	// window (mis. 24 jam). Dipanggil setelah handler sukses (status 2xx).
	Complete(ctx context.Context, tenantID string, personID uuid.UUID, key string, status int, body []byte) error

	// Release menghapus reservasi yang BELUM selesai (respons non-2xx / panic) agar request
	// berikutnya dengan key sama bisa mencoba lagi. Aman dipanggil untuk entri yang sudah
	// terhapus/kedaluwarsa (no-op).
	Release(ctx context.Context, tenantID string, personID uuid.UUID, key string) error
}
