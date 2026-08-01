package crypto

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/port"
)

// FieldSealer adalah kebijakan "satu field logis = dua kolom fisik" (ADR-009 §2) yang dipakai
// oleh repo yang ditulis TANGAN — yaitu yang tak berjalan di atas Mapper[T]/EntityDef sehingga
// tak bisa memakai mesin generik infra/db.fieldCrypto:
//
//	identity/adapter/db  — id.persons/employments/credentials, realm RealmCentral (ADR-017)
//	identity/sync        — clone gov.user_profiles di tenant DB, realm = tenant
//	infra/user           — pembaca clone yang sama
//	infra/notification   — pembaca kontak pada clone yang sama
//
// Yang dipusatkan di sini KEBIJAKANNYA, bukan mesin repo: nilai kosong → NULL (bukan
// ciphertext dari string kosong), pengikatan baris wajib (ADR-016), dan pemeriksaan purpose
// sebelum Decrypt (ADR-015). Empat penulis dengan empat salinan aturan ini akan menyimpang
// diam-diam — dan menyimpangnya tak bergejala sampai seseorang membuka dump.
//
// Realm dipatri saat konstruksi sehingga tak satu pun pemanggil perlu (atau bisa) menyebut
// realm per-panggilan: realm yang salah tidak gagal, ia hanya menghasilkan bidx yang tak
// pernah cocok dan ciphertext yang tak bisa dibuka lagi oleh pembacanya.
type FieldSealer struct {
	crypto port.CryptoPort
	realm  string
	owner  string // prefiks pesan error: paket pemanggil, bukan paket ini
}

// NewFieldSealer menolak CryptoPort nil dan realm kosong.
//
// Gagalnya saat KONSTRUKSI, bukan saat baris pertama ditulis: repo tanpa kripto menyimpan
// pengenal plaintext tanpa satu pun gejala sampai seseorang membuka dump — cermin penolakan
// yang sama di infra/db.NewRepository. Realm kosong ditolak karena ia diam-diam menyatukan
// ruang kunci semua tenant menjadi satu.
//
// who dipakai untuk prefiks pesan error (mis. "identity/db", "identity/sync") supaya kegagalan
// kripto menunjuk pemanggilnya, bukan paket ini.
func NewFieldSealer(c port.CryptoPort, realm, who string) (*FieldSealer, error) {
	if c == nil {
		return nil, fmt.Errorf("%s: field terenkripsi butuh port.CryptoPort (ADR-009)", who)
	}
	if realm == "" {
		return nil, fmt.Errorf("%s: field terenkripsi butuh realm kunci (ADR-017)", who)
	}
	return &FieldSealer{crypto: c, realm: realm, owner: who}, nil
}

// Realm melaporkan realm kunci yang dipatri sealer ini.
func (f *FieldSealer) Realm() string { return f.realm }

// Seal menghasilkan pasangan kolom fisik ({f}_enc, {f}_bidx) untuk satu nilai.
//
// Nilai kosong menghasilkan (nil, nil) yang tersimpan sebagai NULL, BUKAN ciphertext dari
// string kosong: nilai yang absen tak punya apa pun untuk dirahasiakan, dan bidx dari ""
// menjadi satu nilai konstan yang dibagi semua baris tanpa nilai — menumpuk di satu bucket
// index tanpa manfaat, sekaligus membocorkan "baris-baris ini sama-sama kosong".
func (f *FieldSealer) Seal(ctx context.Context, purpose string, recordID uuid.UUID, plain string) (enc, bidx []byte, err error) {
	if plain == "" {
		return nil, nil, nil
	}
	if recordID == uuid.Nil {
		// Tanpa identitas baris, ciphertext tak terikat ke mana pun dan bisa dipindah
		// diam-diam (ADR-016 §6). Gagal keras, jangan pakai nilai default.
		return nil, nil, fmt.Errorf("%s: seal %q butuh id baris (pengikatan baris, ADR-016)", f.who(), purpose)
	}
	ref := port.FieldRef{TenantID: f.realm, Purpose: purpose, RecordID: recordID.String()}
	enc, err = f.crypto.Encrypt(ctx, ref, []byte(plain))
	if err != nil {
		return nil, nil, fmt.Errorf("%s: enkripsi %q: %w", f.who(), purpose, err)
	}
	bidx, err = f.Index(ctx, purpose, plain)
	if err != nil {
		return nil, nil, err
	}
	return enc, bidx, nil
}

// Index menghitung blind index untuk lookup equality & UNIQUE. Sengaja TIDAK menerima
// recordID: ia wajib row-independent (ADR-016 §3), kalau tidak `WHERE {f}_bidx = $1` tak akan
// pernah cocok dan UNIQUE tak akan pernah menangkap duplikat.
func (f *FieldSealer) Index(ctx context.Context, purpose, plain string) ([]byte, error) {
	bidx, err := f.crypto.BlindIndex(ctx, f.realm, purpose, []byte(plain))
	if err != nil {
		return nil, fmt.Errorf("%s: blind index %q: %w", f.who(), purpose, err)
	}
	return bidx, nil
}

// Open membuka satu kolom terenkripsi. NULL/kosong → string kosong (kebalikan Seal).
//
// Purpose blob diperiksa SEBELUM Decrypt (ADR-015): AAD hanya mengikat realm & baris, dan
// purpose dibaca dari blob itu sendiri (format self-describing), sehingga tanpa pemeriksaan
// ini ciphertext bisa dipindah antar KOLOM pada baris yang sama — mis. `no_hp_enc` disalin ke
// `email_enc` — dan tetap terbuka. Pengikatan kolom hanya bisa ditegakkan di lapis yang tahu
// kolomnya, yaitu di pemanggil sealer ini.
func (f *FieldSealer) Open(ctx context.Context, purpose string, recordID uuid.UUID, ct []byte) (string, error) {
	if len(ct) == 0 {
		return "", nil
	}
	got, err := f.crypto.PurposeOf(ct)
	if err != nil {
		return "", fmt.Errorf("%s: baca purpose kolom %q: %w", f.who(), purpose, err)
	}
	if got != purpose {
		return "", fmt.Errorf("%s: ciphertext kolom %q ternyata ber-purpose %q — ditolak (ADR-015)", f.who(), purpose, got)
	}
	plain, err := f.crypto.Decrypt(ctx,
		port.RowRef{TenantID: f.realm, RecordID: recordID.String()}, ct)
	if err != nil {
		// Baris disebut supaya kegagalan bisa ditelusuri; NILAI-nya tak pernah ikut (ADR-009 §6).
		return "", fmt.Errorf("%s: dekripsi %q baris %s: %w", f.who(), purpose, recordID, err)
	}
	return string(plain), nil
}

func (f *FieldSealer) who() string { return f.owner }
