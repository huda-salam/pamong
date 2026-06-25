package db

import (
	"context"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/infra/db"
)

var _ domain.EmploymentRepository = (*EmploymentRepo)(nil)

// EmploymentRepo mengakses id.employments pada identity DB.
type EmploymentRepo struct {
	conn db.Conn
}

func NewEmploymentRepo(conn db.Conn) *EmploymentRepo { return &EmploymentRepo{conn: conn} }

const employmentCols = `id, person_id, status, nip, instansi_asal, is_active, valid_from, valid_until, created_at`

func (r *EmploymentRepo) Save(ctx context.Context, e *domain.Employment) error {
	// NIP kosong (non-ASN) disimpan NULL agar UNIQUE constraint tidak menabrak antar
	// non-ASN (banyak baris NULL diizinkan Postgres).
	var nip any
	if e.NIP != "" {
		nip = e.NIP
	}
	const q = `INSERT INTO id.employments
	    (id, person_id, status, nip, instansi_asal, is_active, valid_from, valid_until)
	    VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`
	_, err := r.conn.Exec(ctx, q, e.ID, e.PersonID, string(e.Status), nip,
		e.InstansiAsal, e.IsActive, e.ValidFrom, e.ValidUntil)
	if db.IsUniqueViolation(err) {
		return core.ErrConflict("NIP sudah terdaftar: " + e.NIP)
	}
	return err
}

func (r *EmploymentRepo) FindByID(ctx context.Context, id uuid.UUID) (*domain.Employment, error) {
	return r.scanOne(r.conn.QueryRow(ctx,
		`SELECT `+employmentCols+` FROM id.employments WHERE id = $1`, id), id.String())
}

func (r *EmploymentRepo) FindByNIP(ctx context.Context, nip string) (*domain.Employment, error) {
	return r.scanOne(r.conn.QueryRow(ctx,
		`SELECT `+employmentCols+` FROM id.employments WHERE nip = $1`, nip), nip)
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
		e, err := scanEmployment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r *EmploymentRepo) scanOne(row interface{ Scan(...any) error }, ref string) (*domain.Employment, error) {
	e, err := scanEmployment(row)
	if db.IsNoRows(err) {
		return nil, core.ErrNotFound("Employment", ref)
	}
	return e, err
}

// scanEmployment memetakan satu baris ke domain.Employment. NIP NULL → string kosong.
func scanEmployment(row interface{ Scan(...any) error }) (*domain.Employment, error) {
	var e domain.Employment
	var status string
	var nip *string
	if err := row.Scan(&e.ID, &e.PersonID, &status, &nip, &e.InstansiAsal,
		&e.IsActive, &e.ValidFrom, &e.ValidUntil, &e.CreatedAt); err != nil {
		return nil, err
	}
	e.Status = domain.EmploymentStatus(status)
	if nip != nil {
		e.NIP = *nip
	}
	return &e, nil
}
