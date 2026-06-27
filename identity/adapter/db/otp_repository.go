package db

import (
	"context"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/infra/db"
)

var _ domain.OTPRepository = (*OTPRepo)(nil)

// OTPRepo mengakses id.otps (OTP ephemeral) pada identity DB sentral.
type OTPRepo struct {
	conn db.Conn
}

func NewOTPRepo(conn db.Conn) *OTPRepo { return &OTPRepo{conn: conn} }

const otpCols = `id, credential_id, code_hash, expires_at, consumed_at, attempts, created_at`

func (r *OTPRepo) Create(ctx context.Context, o *domain.OTP) error {
	const q = `INSERT INTO id.otps (id, credential_id, code_hash, expires_at)
	    VALUES ($1, $2, $3, $4)`
	_, err := r.conn.Exec(ctx, q, o.ID, o.CredentialID, o.CodeHash, o.ExpiresAt)
	return err
}

func (r *OTPRepo) FindLatestByCredential(ctx context.Context, credentialID uuid.UUID) (*domain.OTP, error) {
	row := r.conn.QueryRow(ctx,
		`SELECT `+otpCols+` FROM id.otps WHERE credential_id = $1 ORDER BY created_at DESC LIMIT 1`,
		credentialID)
	o, err := scanOTP(row)
	if db.IsNoRows(err) {
		return nil, core.ErrNotFound("OTP", credentialID.String())
	}
	return o, err
}

// RecordAttempt mempersist jumlah percobaan terbaru. Set absolut (bukan increment di SQL) agar
// nilai berasal dari entity yang sudah divalidasi use case — sederhana & deterministik untuk
// single-instance.
func (r *OTPRepo) RecordAttempt(ctx context.Context, id uuid.UUID, attempts int) error {
	_, err := r.conn.Exec(ctx, `UPDATE id.otps SET attempts = $2 WHERE id = $1`, id, attempts)
	return err
}

// Consume menandai OTP dipakai. Idempoten: hanya set bila belum di-consume (consumed_at NULL).
func (r *OTPRepo) Consume(ctx context.Context, id uuid.UUID) error {
	_, err := r.conn.Exec(ctx,
		`UPDATE id.otps SET consumed_at = now() WHERE id = $1 AND consumed_at IS NULL`, id)
	return err
}

func scanOTP(row interface{ Scan(...any) error }) (*domain.OTP, error) {
	var o domain.OTP
	if err := row.Scan(&o.ID, &o.CredentialID, &o.CodeHash, &o.ExpiresAt,
		&o.ConsumedAt, &o.Attempts, &o.CreatedAt); err != nil {
		return nil, err
	}
	return &o, nil
}
