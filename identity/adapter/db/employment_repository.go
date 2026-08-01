package db

import (
	"context"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/infra/db"
	"github.com/huda-salam/pamong/port"
)

var _ domain.EmploymentRepository = (*EmploymentRepo)(nil)

// EmploymentRepo mengakses id.employments pada identity DB. NIP tersimpan terenkripsi
// (nip_enc) dengan blind index pendamping (nip_bidx) yang memikul UNIQUE-nya.
type EmploymentRepo struct {
	conn db.Conn
	fc   identityCrypto
}

// NewEmploymentRepo merakit repo employment. CryptoPort WAJIB — lihat NewPersonRepo.
func NewEmploymentRepo(conn db.Conn, c port.CryptoPort) (*EmploymentRepo, error) {
	fc, err := newIdentityCrypto(c)
	if err != nil {
		return nil, err
	}
	return &EmploymentRepo{conn: conn, fc: fc}, nil
}

const employmentCols = `id, person_id, status, nip_enc, instansi_asal, is_active, valid_from, valid_until, created_at`

func (r *EmploymentRepo) Save(ctx context.Context, e *domain.Employment) error {
	// Lihat CredentialRepo.Save. Untuk employment yang dijaga bukan bentuk alamat melainkan
	// pasangan status↔NIP: CHECK employments_nip_status_check kini berdiri di atas nip_bidx,
	// jadi ia tak lagi bisa "melihat" NIP yang cacat bentuknya — hanya ada/tidaknya.
	if err := e.Validate(); err != nil {
		return err
	}

	// NIP kosong (non-ASN) disimpan NULL pada KEDUA kolom: banyak baris NULL diizinkan
	// UNIQUE Postgres, dan CHECK employments_nip_status_check menuntutnya. seal()
	// mengembalikan (nil, nil) untuk nilai kosong, jadi tak perlu percabangan di sini.
	nipEnc, nipBidx, err := r.fc.seal(ctx, purposeNIP, e.ID, e.NIP)
	if err != nil {
		return err
	}

	const q = `INSERT INTO id.employments
	    (id, person_id, status, nip_enc, nip_bidx, instansi_asal, is_active, valid_from, valid_until)
	    VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`
	_, err = r.conn.Exec(ctx, q, e.ID, e.PersonID, string(e.Status), nipEnc, nipBidx,
		e.InstansiAsal, e.IsActive, e.ValidFrom, e.ValidUntil)
	if db.IsUniqueViolation(err) {
		// NIP tak diikutkan ke pesan: error mengalir ke log (ADR-009 §6 jalur log/trace).
		return core.ErrConflict("NIP sudah terdaftar")
	}
	return err
}

func (r *EmploymentRepo) FindByID(ctx context.Context, id uuid.UUID) (*domain.Employment, error) {
	return r.scanOne(ctx, r.conn.QueryRow(ctx,
		`SELECT `+employmentCols+` FROM id.employments WHERE id = $1`, id), id.String())
}

// FindByNIP mencari lewat blind index — nip_enc tak bisa dipakai equality (nonce acak
// membuat NIP yang sama menghasilkan ciphertext berbeda tiap penulisan).
func (r *EmploymentRepo) FindByNIP(ctx context.Context, nip string) (*domain.Employment, error) {
	bidx, err := r.fc.index(ctx, purposeNIP, nip)
	if err != nil {
		return nil, err
	}
	// Referensi error menyebut JENIS pencarian, bukan NIP-nya — lihat catatan di PersonRepo.FindByNIK.
	return r.scanOne(ctx, r.conn.QueryRow(ctx,
		`SELECT `+employmentCols+` FROM id.employments WHERE nip_bidx = $1`, bidx), purposeNIP)
}

func (r *EmploymentRepo) ListByPerson(ctx context.Context, personID uuid.UUID) ([]*domain.Employment, error) {
	rows, err := r.conn.Query(ctx,
		`SELECT `+employmentCols+` FROM id.employments WHERE person_id = $1 ORDER BY valid_from ASC`, personID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*domain.Employment
	for rows.Next() {
		e, err := r.scanEmployment(ctx, rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r *EmploymentRepo) scanOne(ctx context.Context, row interface{ Scan(...any) error }, ref string) (*domain.Employment, error) {
	e, err := r.scanEmployment(ctx, row)
	if db.IsNoRows(err) {
		return nil, core.ErrNotFound("Employment", ref)
	}
	return e, err
}

// scanEmployment memetakan satu baris ke domain.Employment lalu membuka NIP. NIP NULL
// (non-ASN) → string kosong. Identitas baris untuk AAD diambil dari BARIS ITU SENDIRI
// (e.ID hasil scan), tak pernah dari parameter pemanggil (ADR-016).
func (r *EmploymentRepo) scanEmployment(ctx context.Context, row interface{ Scan(...any) error }) (*domain.Employment, error) {
	var (
		e      domain.Employment
		status string
		nipEnc []byte
	)
	if err := row.Scan(&e.ID, &e.PersonID, &status, &nipEnc, &e.InstansiAsal,
		&e.IsActive, &e.ValidFrom, &e.ValidUntil, &e.CreatedAt); err != nil {
		return nil, err
	}
	e.Status = domain.EmploymentStatus(status)

	nip, err := r.fc.open(ctx, purposeNIP, e.ID, nipEnc)
	if err != nil {
		return nil, err
	}
	e.NIP = nip
	return &e, nil
}
