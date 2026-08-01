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

// FieldRef adalah koordinat lengkap satu nilai terenkripsi — dipakai saat MENULIS
// (ADR-016 §2). Ketiganya masuk ke AAD, sehingga ciphertext hanya bisa dibuka kembali di
// tempat yang sama persis: tenant yang sama, konteks kunci yang sama, BARIS yang sama.
//
// Struct bernama, bukan tiga parameter string berurutan: tertukarnya Purpose dengan
// RecordID menghasilkan satu DEK per baris — kesalahan yang tampak seperti "lambat",
// bukan seperti "salah".
type FieldRef struct {
	// TenantID menentukan hierarki kunci (ADR-010 §2): satu tenant bocor tak membuka
	// tenant lain. Wajib — konsekuensi DB-per-tenant (ADR-004) & Tier 3.
	TenantID string

	// Purpose memisahkan konteks kunci (mis. "nik" vs "no_rekening") tanpa mengubah port;
	// ia membatasi blast radius dan membuat rotasi bisa per-konteks. Default framework:
	// nama kolom. Wajib.
	Purpose string

	// RecordID adalah identitas BARIS pemilik nilai (ADR-016). Wajib, dan sengaja TIDAK
	// disimpan di dalam blob: ia harus disuplai lagi saat membaca, karena di situlah
	// penegakannya — nilai yang dipasang di baris lain akan diminta dibuka dengan id baris
	// itu dan ditolak AEAD. Bertipe string, bukan uuid.UUID, karena tak semua catatan
	// terenkripsi ber-kunci UUID (mis. cache idempotency ber-kunci string dari klien).
	RecordID string
}

// Row menurunkan koordinat baca dari koordinat tulis. Purpose tidak ikut: ia dibaca dari
// blob (ADR-009 §3).
func (r FieldRef) Row() RowRef {
	return RowRef{TenantID: r.TenantID, RecordID: r.RecordID}
}

// RowRef adalah bagian koordinat yang WAJIB disuplai pemanggil saat MEMBACA (ADR-016 §2).
//
// Purpose & versi kunci sengaja tidak ada di sini — keduanya dibaca dari blob itu sendiri.
// Menaruh Purpose di sini akan menggoda Decrypt untuk sekaligus menegakkan pengikatan
// KOLOM, alternatif yang sudah ditolak ADR-015: jalur baca audit tidak tahu kolom asal
// sebuah nilai. Pengikatan kolom tetap di repository (PurposeOf), pengikatan baris di AEAD.
type RowRef struct {
	TenantID string
	RecordID string
}

// CryptoPort adalah seam kripto framework (ADR-009 §4). Enkripsi field selektif dipanggil
// OTOMATIS di lapis repository berdasarkan FieldDef.Class — BUKAN dari use case (bila use
// case yang memanggil, developer modul pasti lupa). Domain & use case tetap nol-dependency
// kripto, mengikuti pola port.OTPCodec/PasswordVerifier (ADR-008).
type CryptoPort interface {
	// Encrypt membungkus plain dengan AEAD (nonce acak per-nilai) memakai DEK aktif untuk
	// (ref.TenantID, ref.Purpose). Output self-describing (membawa purpose + versi kunci)
	// sehingga Decrypt tak perlu diberi purpose dan rotasi kunci tak butuh migrasi format;
	// ref.RecordID TIDAK ikut ke dalam blob, hanya ke AAD (ADR-016 §1).
	// Dua panggilan atas plaintext sama WAJIB menghasilkan ciphertext berbeda — karena itu
	// kolom _enc tak bisa dipakai equality/UNIQUE; gunakan BlindIndex.
	// Ref tak lengkap (field kosong) = error, bukan nilai default: nilai yang terenkripsi
	// tanpa terikat baris bisa dipindah ke mana saja tanpa gejala.
	Encrypt(ctx context.Context, ref FieldRef, plain []byte) ([]byte, error)

	// Decrypt membaca metadata dari ct (purpose + versi kunci) untuk memilih DEK yang tepat,
	// sehingga ciphertext yang ditulis sebelum rotasi tetap terbaca (lazy re-encrypt bukan
	// tanggung jawab port ini). Mengembalikan ErrCiphertextInvalid bila blob rusak/asing,
	// milik tenant lain, ATAU milik baris lain (ADR-016) — ketiganya satu jawaban, karena
	// membedakannya hanya membantu penyerang.
	Decrypt(ctx context.Context, ref RowRef, ct []byte) ([]byte, error)

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
	//
	// Sengaja menerima (tenantID, purpose) telanjang, BUKAN FieldRef: blind index WAJIB
	// row-independent (ADR-016 §3), kalau tidak `WHERE nik_bidx = $1` tak akan pernah cocok
	// dan UNIQUE tak akan pernah menangkap duplikat. Tanda tangan tanpa FieldRef membuat
	// tak ada tempat untuk menyelipkan RecordID tanpa sadar.
	BlindIndex(ctx context.Context, tenantID, purpose string, plain []byte) ([]byte, error)
}
