package port

import (
	"context"
	"errors"
)

// ErrCiphertextInvalid dikembalikan saat blob yang diberikan ke Decrypt bukan ciphertext
// berformat Pamong (versi format tak dikenal, terpotong, atau tag GCM tak cocok). Seragam
// lintas driver sehingga pemanggil bisa membedakan "bukan/rusak ciphertext" dari kegagalan
// transport ke KMS lewat errors.Is.
var ErrCiphertextInvalid = errors.New("crypto: ciphertext tidak valid")

// CryptoPort adalah seam kripto framework (ADR-009 §4). Enkripsi field selektif dipanggil
// OTOMATIS di lapis repository berdasarkan FieldDef.Class — BUKAN dari use case (bila use
// case yang memanggil, developer modul pasti lupa). Domain & use case tetap nol-dependency
// kripto, mengikuti pola port.OTPCodec/PasswordVerifier (ADR-008).
//
// tenantID wajib di semua operasi: hierarki kunci per-tenant (ADR-010 §2) menjaga satu
// tenant bocor tak membuka tenant lain — konsekuensi DB-per-tenant (ADR-004) & Tier 3.
// purpose memisahkan konteks kunci (mis. "nik" vs "no_rekening") tanpa mengubah port;
// ia membatasi blast radius dan membuat rotasi bisa per-konteks.
type CryptoPort interface {
	// Encrypt membungkus plain dengan AEAD (nonce acak per-nilai) memakai DEK aktif untuk
	// (tenantID, purpose). Output self-describing (membawa purpose + versi kunci) sehingga
	// Decrypt tak perlu diberi purpose dan rotasi kunci tak butuh migrasi format.
	// Dua panggilan atas plaintext sama WAJIB menghasilkan ciphertext berbeda — karena itu
	// kolom _enc tak bisa dipakai equality/UNIQUE; gunakan BlindIndex.
	Encrypt(ctx context.Context, tenantID, purpose string, plain []byte) ([]byte, error)

	// Decrypt membaca metadata dari ct (purpose + versi kunci) untuk memilih DEK yang tepat,
	// sehingga ciphertext yang ditulis sebelum rotasi tetap terbaca (lazy re-encrypt bukan
	// tanggung jawab port ini). Mengembalikan ErrCiphertextInvalid bila blob rusak/asing,
	// termasuk saat ct milik tenant lain.
	Decrypt(ctx context.Context, tenantID string, ct []byte) ([]byte, error)

	// PurposeOf membaca purpose (konteks kunci) dari ciphertext TANPA mendekripsinya — tanpa
	// kunci, tanpa I/O. Ini bagian dari kontrak port, bukan detail satu implementasi, karena
	// lapis repository membutuhkannya untuk menegakkan pengikatan KOLOM: AAD hanya mengikat
	// tenant (purpose & versi kunci dibaca dari blob itu sendiri, konsekuensi format
	// self-describing), sehingga ciphertext yang dipindah antar kolom dalam SATU tenant tetap
	// terbuka. Repo membandingkan hasil PurposeOf dengan purpose kolom yang sedang dibaca dan
	// menolak bila berbeda. Mengembalikan ErrCiphertextInvalid untuk blob asing/rusak.
	PurposeOf(ct []byte) (string, error)

	// BlindIndex menghasilkan nilai DETERMINISTIK atas plain ternormalisasi memakai kunci
	// blind index yang TERPISAH dari kunci enkripsi. Inilah yang menopang lookup equality
	// dan UNIQUE pada kolom _bidx (ADR-009 §2). Konsekuensi yang disadari: rotasi kunci
	// blind index memaksa reindex seluruh baris — operasi kompromi, bukan rutin.
	BlindIndex(ctx context.Context, tenantID, purpose string, plain []byte) ([]byte, error)
}
