package db

import (
	"context"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/infra/db"
	"github.com/huda-salam/pamong/port"
)

var _ domain.CredentialRepository = (*CredentialRepo)(nil)

// CredentialRepo mengakses id.credentials pada identity DB.
//
// cred_value (NIP/NIK/email/no HP yang dipakai login) tersimpan terenkripsi; cred_type
// TETAP plaintext — ia jenis kredensial, bukan pengenal orang. Justru itulah yang membuat
// UNIQUE (cred_type, cred_value_bidx) tetap menegakkan keunikan per-tipe seperti sebelumnya
// dan FindByTypeValue tetap satu query (ADR-017 §4).
type CredentialRepo struct {
	conn db.Conn
	fc   identityCrypto
}

// NewCredentialRepo merakit repo credential. CryptoPort WAJIB — lihat NewPersonRepo.
func NewCredentialRepo(conn db.Conn, c port.CryptoPort) (*CredentialRepo, error) {
	fc, err := newIdentityCrypto(c)
	if err != nil {
		return nil, err
	}
	return &CredentialRepo{conn: conn, fc: fc}, nil
}

const credentialCols = `id, person_id, cred_type, cred_value_enc, secret_hash, is_primary, last_used_at, created_at`

func (r *CredentialRepo) Save(ctx context.Context, c *domain.Credential) error {
	// Invariant domain ditegakkan DI PINTU TULIS, bukan diserahkan ke use case. Alasannya
	// sama dengan alasan enkripsi & audit dipasang di lapis repo: kalau penegakannya
	// bergantung pada tiap penulis use case baru mengingat memanggil Validate, ia akan
	// terlewat — dan yang terlewat di sini bukan kosmetik. cred_value yang lolos dengan
	// CR/LF tetap me-resolve lewat blind index (normalize() mem-*trim*-nya sebelum HMAC),
	// lalu ditolak transport di hilir → beda respons antara kredensial terdaftar dan tak
	// terdaftar. Itu orakel keberadaan akun yang persis sama dengan yang ditutup di jalur
	// baca (REVIEW_BACKLOG A7); menutupnya di jalur tulis hanya berarti kalau tak ada
	// jalan memutar. Saat ini belum ada use case penulis credential — justru itu waktu
	// termurah memasangnya.
	if err := c.Validate(); err != nil {
		return err
	}

	var secret any
	if c.SecretHash != "" {
		secret = c.SecretHash
	}
	valEnc, valBidx, err := r.fc.seal(ctx, purposeOfCredType(c.CredType), c.ID, c.CredValue)
	if err != nil {
		return err
	}

	const q = `INSERT INTO id.credentials
	    (id, person_id, cred_type, cred_value_enc, cred_value_bidx, secret_hash, is_primary)
	    VALUES ($1,$2,$3,$4,$5,$6,$7)`
	_, err = r.conn.Exec(ctx, q, c.ID, c.PersonID, string(c.CredType), valEnc, valBidx, secret, c.IsPrimary)
	if db.IsUniqueViolation(err) {
		// Nilai kredensialnya tak diikutkan ke pesan (ADR-009 §6 jalur log/trace); tipenya
		// aman disebut dan itulah yang menjelaskan konfliknya.
		return core.ErrConflict("credential sudah terdaftar untuk tipe " + string(c.CredType))
	}
	return err
}

// FindByTypeValue adalah jalur resolusi login (NIP/NIK/email/no HP → person). Equality atas
// nilai kredensial berjalan di blind index; cred_type tetap dibandingkan langsung.
func (r *CredentialRepo) FindByTypeValue(ctx context.Context, t domain.CredType, value string) (*domain.Credential, error) {
	bidx, err := r.fc.index(ctx, purposeOfCredType(t), value)
	if err != nil {
		return nil, err
	}
	row := r.conn.QueryRow(ctx,
		`SELECT `+credentialCols+` FROM id.credentials WHERE cred_type = $1 AND cred_value_bidx = $2`,
		string(t), bidx)
	c, err := r.scanCredential(ctx, row)
	if db.IsNoRows(err) {
		// Referensi error hanya menyebut TIPE, bukan nilainya — pesan not-found ikut
		// mengalir ke log & respons.
		return nil, core.ErrNotFound("Credential", string(t))
	}
	return c, err
}

func (r *CredentialRepo) ListByPerson(ctx context.Context, personID uuid.UUID) ([]*domain.Credential, error) {
	rows, err := r.conn.Query(ctx,
		`SELECT `+credentialCols+` FROM id.credentials WHERE person_id = $1 ORDER BY created_at ASC`, personID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*domain.Credential
	for rows.Next() {
		c, err := r.scanCredential(ctx, rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// scanCredential memetakan satu baris ke domain.Credential lalu membuka nilai kredensial.
// secret_hash NULL → kosong. Identitas baris untuk AAD diambil dari BARIS ITU SENDIRI
// (c.ID hasil scan), tak pernah dari parameter pemanggil (ADR-016).
func (r *CredentialRepo) scanCredential(ctx context.Context, row interface{ Scan(...any) error }) (*domain.Credential, error) {
	var (
		c        domain.Credential
		credType string
		secret   *string
		valEnc   []byte
	)
	if err := row.Scan(&c.ID, &c.PersonID, &credType, &valEnc, &secret,
		&c.IsPrimary, &c.LastUsedAt, &c.CreatedAt); err != nil {
		return nil, err
	}
	c.CredType = domain.CredType(credType)
	if secret != nil {
		c.SecretHash = *secret
	}

	// Purpose diturunkan dari cred_type BARIS ITU (hasil scan), bukan dari argumen pemanggil —
	// sehingga ciphertext yang dipindah antar tipe kredensial pun tertangkap PurposeOf.
	value, err := r.fc.open(ctx, purposeOfCredType(c.CredType), c.ID, valEnc)
	if err != nil {
		return nil, err
	}
	c.CredValue = value
	return &c, nil
}
