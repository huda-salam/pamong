package db

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/infra/db"
)

var _ domain.RevokedTokenStore = (*RevokedTokenStore)(nil)

// RevokedTokenStore mengakses id.revoked_tokens (denylist jti) pada identity DB sentral.
type RevokedTokenStore struct {
	conn db.Conn
}

func NewRevokedTokenStore(conn db.Conn) *RevokedTokenStore {
	return &RevokedTokenStore{conn: conn}
}

// Revoke menandai jti dicabut hingga expiresAt. Idempoten via ON CONFLICT DO NOTHING:
// mencabut jti yang sama dua kali bukan error (retry / event ganda aman).
func (s *RevokedTokenStore) Revoke(ctx context.Context, jti, personID uuid.UUID, expiresAt time.Time, reason string) error {
	const q = `INSERT INTO id.revoked_tokens (jti, person_id, expires_at, reason)
	    VALUES ($1, $2, $3, $4) ON CONFLICT (jti) DO NOTHING`
	_, err := s.conn.Exec(ctx, q, jti, personID, expiresAt, reason)
	return err
}

// IsRevoked true bila jti ada di denylist. Lookup PK → cepat.
func (s *RevokedTokenStore) IsRevoked(ctx context.Context, jti uuid.UUID) (bool, error) {
	var revoked bool
	err := s.conn.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM id.revoked_tokens WHERE jti = $1)`, jti).Scan(&revoked)
	if err != nil {
		return false, err
	}
	return revoked, nil
}
