package db

import (
	"context"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/infra/db"
)

var _ domain.TenantAssignmentRepository = (*TenantAssignmentRepo)(nil)

// TenantAssignmentRepo mengakses id.tenant_assignments pada identity DB.
type TenantAssignmentRepo struct {
	conn db.Conn
}

func NewTenantAssignmentRepo(conn db.Conn) *TenantAssignmentRepo {
	return &TenantAssignmentRepo{conn: conn}
}

const tenantAssignmentCols = `id, employment_id, tenant_id, is_home_tenant, assigned_by, valid_from, valid_until, created_at`

func (r *TenantAssignmentRepo) Save(ctx context.Context, a *domain.TenantAssignment) error {
	const q = `INSERT INTO id.tenant_assignments
	    (id, employment_id, tenant_id, is_home_tenant, assigned_by, valid_from, valid_until)
	    VALUES ($1,$2,$3,$4,$5,$6,$7)`
	_, err := r.conn.Exec(ctx, q, a.ID, a.EmploymentID, a.TenantID, a.IsHomeTenant,
		a.AssignedBy, a.ValidFrom, a.ValidUntil)
	if db.IsUniqueViolation(err) {
		return core.ErrConflict("employment sudah ditugaskan ke tenant " + a.TenantID + " pada tanggal yang sama")
	}
	return err
}

func (r *TenantAssignmentRepo) ListByEmployment(ctx context.Context, employmentID uuid.UUID) ([]*domain.TenantAssignment, error) {
	rows, err := r.conn.Query(ctx,
		`SELECT `+tenantAssignmentCols+` FROM id.tenant_assignments WHERE employment_id = $1 ORDER BY valid_from ASC`,
		employmentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*domain.TenantAssignment
	for rows.Next() {
		var a domain.TenantAssignment
		if err := rows.Scan(&a.ID, &a.EmploymentID, &a.TenantID, &a.IsHomeTenant,
			&a.AssignedBy, &a.ValidFrom, &a.ValidUntil, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &a)
	}
	return out, rows.Err()
}
