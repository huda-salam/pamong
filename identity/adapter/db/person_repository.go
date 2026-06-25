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
)

var _ domain.PersonRepository = (*PersonRepo)(nil)

// PersonRepo mengakses id.persons pada identity DB.
type PersonRepo struct {
	conn db.Conn
}

func NewPersonRepo(conn db.Conn) *PersonRepo { return &PersonRepo{conn: conn} }

const personCols = `id, nik, nama_lengkap, tgl_lahir, no_hp, email, is_active, created_at, updated_at`

func (r *PersonRepo) Save(ctx context.Context, p *domain.Person) error {
	const q = `INSERT INTO id.persons
	    (id, nik, nama_lengkap, tgl_lahir, no_hp, email, is_active)
	    VALUES ($1,$2,$3,$4,$5,$6,$7)`
	_, err := r.conn.Exec(ctx, q, p.ID, p.NIK, p.NamaLengkap, p.TglLahir, p.NoHP, p.Email, p.IsActive)
	if db.IsUniqueViolation(err) {
		return core.ErrConflict("NIK sudah terdaftar: " + p.NIK)
	}
	return err
}

func (r *PersonRepo) FindByID(ctx context.Context, id uuid.UUID) (*domain.Person, error) {
	return r.scanOne(r.conn.QueryRow(ctx,
		`SELECT `+personCols+` FROM id.persons WHERE id = $1`, id), id.String())
}

func (r *PersonRepo) FindByNIK(ctx context.Context, nik string) (*domain.Person, error) {
	return r.scanOne(r.conn.QueryRow(ctx,
		`SELECT `+personCols+` FROM id.persons WHERE nik = $1`, nik), nik)
}

func (r *PersonRepo) scanOne(row interface{ Scan(...any) error }, ref string) (*domain.Person, error) {
	var p domain.Person
	if err := row.Scan(&p.ID, &p.NIK, &p.NamaLengkap, &p.TglLahir, &p.NoHP, &p.Email,
		&p.IsActive, &p.CreatedAt, &p.UpdatedAt); err != nil {
		if db.IsNoRows(err) {
			return nil, core.ErrNotFound("Person", ref)
		}
		return nil, err
	}
	return &p, nil
}
