package db

import (
	"context"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/infra/crypto"
	"github.com/huda-salam/pamong/port"
)

// Enkripsi field selektif untuk identity DB (ADR-009 §2, ADR-017). Satu field logis
// terenkripsi = dua kolom fisik: {f}_enc (AES-256-GCM, nonce acak) + {f}_bidx (HMAC
// deterministik) — yang kedua itulah yang menopang lookup equality & UNIQUE.
//
// Kenapa repo ini tidak memakai mesin infra/db.fieldCrypto: repo identity ditulis TANGAN
// (bukan SQLRepository di atas Mapper[T]/EntityDef), karena skema identity punya invariant
// yang tak bisa diungkapkan EntityDef — NIP NULL untuk non-ASN dengan CHECK silang ke kolom
// status, dan UNIQUE majemuk (cred_type, cred_value_bidx). Yang dipakai bersama adalah
// KEBIJAKANNYA — dan sejak PR-3.8.5 kebijakan itu punya SATU implementasi,
// crypto.FieldSealer, yang juga dipakai jalur clone tenant. Aturan "kosong → NULL",
// pengikatan baris, dan pemeriksaan PurposeOf tak lagi ditulis dua kali.

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

// identityCrypto mematri realm SENTRAL pada sealer bersama, sehingga tak ada satu pun
// pemanggil di paket ini yang perlu (atau bisa) menyebut realm sendiri. Data identity tak
// punya tenant; lihat ADR-017 §1.
type identityCrypto struct {
	sealer *crypto.FieldSealer
}

// newIdentityCrypto menolak CryptoPort nil (lewat NewFieldSealer). Gagal saat konstruksi,
// bukan saat baris pertama ditulis: repo identity tanpa kripto akan menyimpan NIK/NIP
// plaintext tanpa satu pun gejala sampai seseorang membuka dump.
func newIdentityCrypto(c port.CryptoPort) (identityCrypto, error) {
	s, err := crypto.NewFieldSealer(c, crypto.RealmCentral, "identity/db")
	if err != nil {
		return identityCrypto{}, err
	}
	return identityCrypto{sealer: s}, nil
}

func (c identityCrypto) seal(ctx context.Context, purpose string, recordID uuid.UUID, plain string) (enc, bidx []byte, err error) {
	return c.sealer.Seal(ctx, purpose, recordID, plain)
}

func (c identityCrypto) index(ctx context.Context, purpose, plain string) ([]byte, error) {
	return c.sealer.Index(ctx, purpose, plain)
}

func (c identityCrypto) open(ctx context.Context, purpose string, recordID uuid.UUID, ct []byte) (string, error) {
	return c.sealer.Open(ctx, purpose, recordID, ct)
}
