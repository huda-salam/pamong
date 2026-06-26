package db

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/delegation/domain"
	"github.com/huda-salam/pamong/infra/db"
)

var _ domain.DelegationRepository = (*DelegationRepo)(nil)

// DelegationRepo mengakses gov.delegations pada TENANT DB.
type DelegationRepo struct {
	conn db.Conn
}

func NewDelegationRepo(conn db.Conn) *DelegationRepo { return &DelegationRepo{conn: conn} }

const delegationCols = `id, from_user_id, to_user_id, permissions, unit_kerja_id, include_subtree, reason, valid_from, valid_until, assigned_by, created_at`

func (r *DelegationRepo) Save(ctx context.Context, d *domain.Delegation) error {
	if err := ensureDelegationSchema(ctx, r.conn); err != nil {
		return err
	}
	const q = `INSERT INTO gov.delegations
	    (id, from_user_id, to_user_id, permissions, unit_kerja_id, include_subtree, reason, valid_from, valid_until, assigned_by)
	    VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`
	_, err := r.conn.Exec(ctx, q, d.ID, d.FromUserID, d.ToUserID, d.Permissions, d.UnitKerjaID,
		d.IncludeSubtree, d.Reason, d.ValidFrom, d.ValidUntil, d.AssignedBy)
	return err
}

// ListActiveByDelegatee mengembalikan delegasi yang AKTIF pada now untuk delegatee. Kedaluwarsa
// difilter di SQL (lazy): hanya delegasi dengan now ∈ [valid_from, valid_until) — delegasi lewat
// masa berlaku tak pernah ikut ter-resolve (DoD PR-2.3.5b).
func (r *DelegationRepo) ListActiveByDelegatee(ctx context.Context, toUserID uuid.UUID, now time.Time) ([]*domain.Delegation, error) {
	if err := ensureDelegationSchema(ctx, r.conn); err != nil {
		return nil, err
	}
	const q = `SELECT ` + delegationCols + ` FROM gov.delegations
	    WHERE to_user_id = $1 AND valid_from <= $2 AND valid_until > $2`
	rows, err := r.conn.Query(ctx, q, toUserID, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*domain.Delegation
	for rows.Next() {
		var d domain.Delegation
		if err := rows.Scan(&d.ID, &d.FromUserID, &d.ToUserID, &d.Permissions, &d.UnitKerjaID,
			&d.IncludeSubtree, &d.Reason, &d.ValidFrom, &d.ValidUntil, &d.AssignedBy, &d.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &d)
	}
	return out, rows.Err()
}
