package db

import (
	"context"

	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/infra/db"
)

var _ domain.TenantRegistry = (*TenantRepo)(nil)

// TenantRepo mengakses id.tenant_registry pada identity DB sentral.
type TenantRepo struct {
	conn db.Conn
}

func NewTenantRepo(conn db.Conn) *TenantRepo { return &TenantRepo{conn: conn} }

const tenantCols = `tenant_id, nama, tier, db_host, db_name, db_schema, migration_version, is_active, created_at, updated_at`

func (r *TenantRepo) Save(ctx context.Context, t *domain.Tenant) error {
	const q = `INSERT INTO id.tenant_registry
	    (tenant_id, nama, tier, db_host, db_name, db_schema, migration_version, is_active)
	    VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`
	_, err := r.conn.Exec(ctx, q, t.TenantID, t.Nama, t.Tier, t.DBHost, t.DBName,
		t.DBSchema, t.MigrationVersion, t.IsActive)
	if db.IsUniqueViolation(err) {
		return core.ErrConflict("tenant sudah terdaftar: " + t.TenantID)
	}
	return err
}

func (r *TenantRepo) FindByID(ctx context.Context, tenantID string) (*domain.Tenant, error) {
	row := r.conn.QueryRow(ctx, `SELECT `+tenantCols+` FROM id.tenant_registry WHERE tenant_id = $1`, tenantID)
	var t domain.Tenant
	if err := row.Scan(&t.TenantID, &t.Nama, &t.Tier, &t.DBHost, &t.DBName, &t.DBSchema,
		&t.MigrationVersion, &t.IsActive, &t.CreatedAt, &t.UpdatedAt); err != nil {
		if db.IsNoRows(err) {
			return nil, core.ErrNotFound("Tenant", tenantID)
		}
		return nil, err
	}
	return &t, nil
}

func (r *TenantRepo) List(ctx context.Context) ([]*domain.Tenant, error) {
	rows, err := r.conn.Query(ctx, `SELECT `+tenantCols+` FROM id.tenant_registry ORDER BY tenant_id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*domain.Tenant
	for rows.Next() {
		var t domain.Tenant
		if err := rows.Scan(&t.TenantID, &t.Nama, &t.Tier, &t.DBHost, &t.DBName, &t.DBSchema,
			&t.MigrationVersion, &t.IsActive, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, &t)
	}
	return out, rows.Err()
}

func (r *TenantRepo) SetActive(ctx context.Context, tenantID string, active bool) error {
	const q = `UPDATE id.tenant_registry SET is_active = $2, updated_at = now() WHERE tenant_id = $1`
	tag, err := r.conn.Exec(ctx, q, tenantID, active)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return core.ErrNotFound("Tenant", tenantID)
	}
	return nil
}
