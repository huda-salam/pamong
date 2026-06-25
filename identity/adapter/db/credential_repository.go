package db

import (
	"context"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/infra/db"
)

var _ domain.CredentialRepository = (*CredentialRepo)(nil)

// CredentialRepo mengakses id.credentials pada identity DB.
type CredentialRepo struct {
	conn db.Conn
}

func NewCredentialRepo(conn db.Conn) *CredentialRepo { return &CredentialRepo{conn: conn} }

const credentialCols = `id, person_id, cred_type, cred_value, secret_hash, is_primary, last_used_at, created_at`

func (r *CredentialRepo) Save(ctx context.Context, c *domain.Credential) error {
	var secret any
	if c.SecretHash != "" {
		secret = c.SecretHash
	}
	const q = `INSERT INTO id.credentials
	    (id, person_id, cred_type, cred_value, secret_hash, is_primary)
	    VALUES ($1,$2,$3,$4,$5,$6)`
	_, err := r.conn.Exec(ctx, q, c.ID, c.PersonID, string(c.CredType), c.CredValue, secret, c.IsPrimary)
	if db.IsUniqueViolation(err) {
		return core.ErrConflict("credential sudah terdaftar: " + string(c.CredType) + "/" + c.CredValue)
	}
	return err
}

func (r *CredentialRepo) FindByTypeValue(ctx context.Context, t domain.CredType, value string) (*domain.Credential, error) {
	row := r.conn.QueryRow(ctx,
		`SELECT `+credentialCols+` FROM id.credentials WHERE cred_type = $1 AND cred_value = $2`,
		string(t), value)
	c, err := scanCredential(row)
	if db.IsNoRows(err) {
		return nil, core.ErrNotFound("Credential", string(t)+"/"+value)
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
		c, err := scanCredential(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// scanCredential memetakan satu baris ke domain.Credential. secret_hash NULL → kosong.
func scanCredential(row interface{ Scan(...any) error }) (*domain.Credential, error) {
	var c domain.Credential
	var credType string
	var secret *string
	if err := row.Scan(&c.ID, &c.PersonID, &credType, &c.CredValue, &secret,
		&c.IsPrimary, &c.LastUsedAt, &c.CreatedAt); err != nil {
		return nil, err
	}
	c.CredType = domain.CredType(credType)
	if secret != nil {
		c.SecretHash = *secret
	}
	return &c, nil
}
