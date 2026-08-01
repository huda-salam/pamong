package db

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/infra/crypto"
	"github.com/huda-salam/pamong/port"
)

// Enkripsi field selektif untuk identity DB (ADR-009 §2, ADR-017). Satu field logis
// terenkripsi = dua kolom fisik: {f}_enc (AES-256-GCM, nonce acak) + {f}_bidx (HMAC
// deterministik) — yang kedua itulah yang menopang lookup equality & UNIQUE.
//
// Kenapa ditulis di sini alih-alih memakai infra/db.fieldCrypto: repo identity ditulis
// TANGAN (bukan SQLRepository di atas Mapper[T]/EntityDef), karena skema identity punya
// invariant yang tak bisa diungkapkan EntityDef — NIP NULL untuk non-ASN dengan CHECK
// silang ke kolom status, dan UNIQUE majemuk (cred_type, cred_value_bidx). Yang dipakai
// bersama adalah KEBIJAKANNYA (port.CryptoPort + purpose = nama kolom + pemeriksaan
// PurposeOf), bukan mesin repo generiknya.

// Purpose = konteks kunci, satu per kolom logis, mengikuti default framework (nama kolom).
// Kolom dan diff audit-nya SENGAJA berbagi purpose (ADR-017 §4): keduanya nilai yang sama
// pada baris yang sama, jadi memisahkannya hanya menggandakan kunci tanpa memperkecil blast
// radius — sekaligus membuat rotasi harus menyapu dua tempat alih-alih satu.
const (
	purposeNIK   = "nik"
	purposeNIP   = "nip"
	purposeNoHP  = "no_hp"
	purposeEmail = "email"
)

// purposeOfCredType menurunkan purpose kredensial dari cred_type-nya, BUKAN satu purpose
// "cred_value" untuk semua tipe (ADR-017 §4).
//
// Alasannya bukan granularitas kunci melainkan NORMALISASI: tabel kebijakan framework
// (crypto.caseFoldedPurposes) mengenali purpose "email" sebagai nilai yang setara tanpa
// memandang besar-kecil huruf. Dengan satu purpose "cred_value", kredensial email akan
// keluar dari kebijakan itu — dan "email yang sama" punya dua definisi: case-insensitive
// di id.persons.email_bidx, case-sensitive di id.credentials. Perbedaan itu tak bergejala
// sampai ada fitur yang menyilangkan keduanya, lalu salah diam-diam.
//
// Efek yang disadari & diterima: login lewat email menjadi case-insensitive (sebelumnya
// equality SQL, case-sensitive), dan UNIQUE mulai menangkap "Budi@x.id" vs "budi@x.id"
// sebagai duplikat. Gratis sekarang karena identity DB kosong. `oauth` tidak ikut di-fold —
// subject dari provider bersifat opaque dan boleh case-sensitive.
//
// Nilai purpose = nilai cred_type apa adanya; keduanya sudah dibatasi CHECK constraint
// (nip|nik|email|no_hp|oauth), jadi tak ada nilai bebas yang bisa menyelinap jadi purpose.
func purposeOfCredType(t domain.CredType) string { return string(t) }

// identityCrypto membungkus CryptoPort dengan realm sentral yang selalu sama, sehingga tak
// ada satu pun pemanggil di paket ini yang perlu (atau bisa) menyebut realm sendiri.
// Data identity tak punya tenant; lihat ADR-017 §1.
type identityCrypto struct {
	crypto port.CryptoPort
}

// newIdentityCrypto menolak CryptoPort nil. Gagal saat konstruksi, bukan saat baris pertama
// ditulis: repo identity tanpa kripto akan menyimpan NIK/NIP plaintext tanpa satu pun gejala
// sampai seseorang membuka dump — cermin penolakan yang sama di infra/db.NewRepository.
func newIdentityCrypto(c port.CryptoPort) (identityCrypto, error) {
	if c == nil {
		return identityCrypto{}, fmt.Errorf(
			"identity/db: repo pengenal butuh port.CryptoPort (nik/nip/cred_value terenkripsi, ADR-009)")
	}
	return identityCrypto{crypto: c}, nil
}

// seal menghasilkan pasangan kolom fisik untuk satu nilai.
//
// Nilai kosong menghasilkan (nil, nil) yang tersimpan sebagai NULL, BUKAN ciphertext dari
// string kosong: nilai yang absen tak punya apa pun untuk dirahasiakan, dan bidx dari ""
// akan menjadi satu nilai konstan yang dibagi semua baris tanpa nilai — menumpuk di satu
// bucket index tanpa manfaat. NULL juga yang dituntut CHECK silang nip↔status.
func (c identityCrypto) seal(ctx context.Context, purpose string, recordID uuid.UUID, plain string) (enc, bidx []byte, err error) {
	if plain == "" {
		return nil, nil, nil
	}
	if recordID == uuid.Nil {
		// Tanpa identitas baris, ciphertext tak terikat ke mana pun dan bisa dipindah
		// diam-diam (ADR-016 §6). Gagal keras, jangan pakai nilai default.
		return nil, nil, fmt.Errorf("identity/db: seal %q butuh id baris (pengikatan baris, ADR-016)", purpose)
	}
	ref := port.FieldRef{TenantID: crypto.RealmCentral, Purpose: purpose, RecordID: recordID.String()}
	enc, err = c.crypto.Encrypt(ctx, ref, []byte(plain))
	if err != nil {
		return nil, nil, fmt.Errorf("identity/db: enkripsi %q: %w", purpose, err)
	}
	bidx, err = c.index(ctx, purpose, plain)
	if err != nil {
		return nil, nil, err
	}
	return enc, bidx, nil
}

// index menghitung blind index untuk lookup & UNIQUE. Sengaja TIDAK menerima recordID: ia
// wajib row-independent (ADR-016 §3), kalau tidak `WHERE nik_bidx = $1` tak akan pernah
// cocok dan UNIQUE tak akan pernah menangkap duplikat.
func (c identityCrypto) index(ctx context.Context, purpose, plain string) ([]byte, error) {
	bidx, err := c.crypto.BlindIndex(ctx, crypto.RealmCentral, purpose, []byte(plain))
	if err != nil {
		return nil, fmt.Errorf("identity/db: blind index %q: %w", purpose, err)
	}
	return bidx, nil
}

// open membuka satu kolom terenkripsi. NULL → string kosong (kebalikan seal).
//
// Purpose blob diperiksa SEBELUM Decrypt (ADR-015): AAD hanya mengikat realm & baris, dan
// purpose dibaca dari blob itu sendiri (format self-describing), sehingga tanpa pemeriksaan
// ini ciphertext bisa dipindah antar KOLOM pada baris yang sama — mis. `no_hp_enc` disalin
// ke `email_enc` — dan tetap terbuka. Pengikatan kolom hanya bisa ditegakkan di lapis yang
// tahu kolomnya, yaitu di sini.
func (c identityCrypto) open(ctx context.Context, purpose string, recordID uuid.UUID, ct []byte) (string, error) {
	if len(ct) == 0 {
		return "", nil
	}
	got, err := c.crypto.PurposeOf(ct)
	if err != nil {
		return "", fmt.Errorf("identity/db: baca purpose kolom %q: %w", purpose, err)
	}
	if got != purpose {
		return "", fmt.Errorf("identity/db: ciphertext kolom %q ternyata ber-purpose %q — ditolak (ADR-015)", purpose, got)
	}
	plain, err := c.crypto.Decrypt(ctx,
		port.RowRef{TenantID: crypto.RealmCentral, RecordID: recordID.String()}, ct)
	if err != nil {
		return "", fmt.Errorf("identity/db: dekripsi %q baris %s: %w", purpose, recordID, err)
	}
	return string(plain), nil
}
