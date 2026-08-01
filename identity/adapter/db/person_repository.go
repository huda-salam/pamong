// Package db adalah driven adapter persistensi identity (Postgres/pgx) terhadap
// identity DB sentral (schema id). Mengimplementasi port repository di identity/domain.
// Domain tidak tahu file ini ada.
package db

import (
	"context"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/infra/db"
	"github.com/huda-salam/pamong/port"
)

var _ domain.PersonRepository = (*PersonRepo)(nil)

// PersonRepo mengakses id.persons pada identity DB.
//
// NIK, no HP, dan email tersimpan TERENKRIPSI (kolom _enc) dengan blind index pendamping
// (_bidx) — lihat field_crypto.go. Tak ada kolom plaintext-nya di tabel, jadi tak ada jalur
// baca yang bisa "kebetulan" mengembalikan pengenal mentah dari dump.
type PersonRepo struct {
	conn db.Conn
	fc   identityCrypto
}

// NewPersonRepo merakit repo person. CryptoPort WAJIB: tanpa itu NIK tersimpan plaintext,
// dan kegagalan semacam itu tak punya gejala sampai seseorang membuka dump.
func NewPersonRepo(conn db.Conn, c port.CryptoPort) (*PersonRepo, error) {
	fc, err := newIdentityCrypto(c)
	if err != nil {
		return nil, err
	}
	return &PersonRepo{conn: conn, fc: fc}, nil
}

// _bidx TIDAK ikut dibaca: ia alat pencarian, bukan sumber nilai.
const personCols = `id, nik_enc, nama_lengkap, tgl_lahir, no_hp_enc, email_enc, is_active, created_at, updated_at`

func (r *PersonRepo) Save(ctx context.Context, p *domain.Person) error {
	nikEnc, nikBidx, err := r.fc.seal(ctx, purposeNIK, p.ID, p.NIK)
	if err != nil {
		return err
	}
	noHPEnc, noHPBidx, err := r.fc.seal(ctx, purposeNoHP, p.ID, p.NoHP)
	if err != nil {
		return err
	}
	emailEnc, emailBidx, err := r.fc.seal(ctx, purposeEmail, p.ID, p.Email)
	if err != nil {
		return err
	}

	const q = `INSERT INTO id.persons
	    (id, nik_enc, nik_bidx, nama_lengkap, tgl_lahir,
	     no_hp_enc, no_hp_bidx, email_enc, email_bidx, is_active)
	    VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`
	_, err = r.conn.Exec(ctx, q, p.ID, nikEnc, nikBidx, p.NamaLengkap, p.TglLahir,
		noHPEnc, noHPBidx, emailEnc, emailBidx, p.IsActive)
	if db.IsUniqueViolation(err) {
		// Keunikan kini ditegakkan uq_persons_nik_bidx, bukan kolom nik plaintext — arti
		// bagi pemanggil tak berubah. NIK-nya sendiri tak lagi diikutkan ke pesan error:
		// pemanggil sudah tahu nilai yang ia kirim, sedangkan error mengalir ke log
		// (ADR-009 §6 jalur log/trace).
		return core.ErrConflict("NIK sudah terdaftar")
	}
	return err
}

func (r *PersonRepo) FindByID(ctx context.Context, id uuid.UUID) (*domain.Person, error) {
	return r.scanOne(ctx, r.conn.QueryRow(ctx,
		`SELECT `+personCols+` FROM id.persons WHERE id = $1`, id), id.String())
}

// FindByNIK mencari lewat blind index. Kolom _enc tak bisa dipakai di sini: nonce acak
// membuat NIK yang sama menghasilkan ciphertext berbeda tiap penulisan.
func (r *PersonRepo) FindByNIK(ctx context.Context, nik string) (*domain.Person, error) {
	bidx, err := r.fc.index(ctx, purposeNIK, nik)
	if err != nil {
		return nil, err
	}
	// Referensi error menyebut JENIS pencarian, bukan NIK-nya: pesan error mengalir ke log dan
	// body HTTP, dan itu jalur samping yang sama yang ditutup ADR-009 §6. Pemanggil sudah tahu
	// nilai yang ia kirim, jadi tak ada informasi yang hilang.
	return r.scanOne(ctx, r.conn.QueryRow(ctx,
		`SELECT `+personCols+` FROM id.persons WHERE nik_bidx = $1`, bidx), purposeNIK)
}

// scanOne memetakan baris lalu membuka kolom terenkripsi. Identitas baris untuk AAD
// (ADR-016) diambil dari BARIS ITU SENDIRI (p.ID hasil scan), tak pernah dari parameter
// pemanggil — sehingga baris yang ciphertext-nya dipindahkan lewat SQL gagal dibuka di sini.
//
// Nilai yang sudah terbuka TIDAK dibandingkan ulang dengan argumen pencarian. Menukar
// _bidx SAJA antar baris memang masih bisa mengembalikan baris yang salah, dan itu sisa
// risiko yang DIPUTUSKAN diterima ADR-016 §Konsekuensi (integritas indeks, bukan
// kebocoran). Perbandingan ulang menuntut normalisasi yang sama persis dengan yang dipakai
// blind index (trim + case-fold per purpose) — kebijakan framework yang hidup di
// infra/crypto; menyalinnya ke sini justru menciptakan dua sumber kebenaran yang bisa
// menyimpang diam-diam.
func (r *PersonRepo) scanOne(ctx context.Context, row interface{ Scan(...any) error }, ref string) (*domain.Person, error) {
	var (
		p                         domain.Person
		nikEnc, noHPEnc, emailEnc []byte
	)
	if err := row.Scan(&p.ID, &nikEnc, &p.NamaLengkap, &p.TglLahir, &noHPEnc, &emailEnc,
		&p.IsActive, &p.CreatedAt, &p.UpdatedAt); err != nil {
		if db.IsNoRows(err) {
			return nil, core.ErrNotFound("Person", ref)
		}
		return nil, err
	}

	var err error
	if p.NIK, err = r.fc.open(ctx, purposeNIK, p.ID, nikEnc); err != nil {
		return nil, err
	}
	if p.NoHP, err = r.fc.open(ctx, purposeNoHP, p.ID, noHPEnc); err != nil {
		return nil, err
	}
	if p.Email, err = r.fc.open(ctx, purposeEmail, p.ID, emailEnc); err != nil {
		return nil, err
	}
	return &p, nil
}
