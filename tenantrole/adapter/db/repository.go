package db

import (
	"context"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/infra/db"
	"github.com/huda-salam/pamong/tenantrole/domain"
)

var (
	_ domain.TenantRoleRepository           = (*TenantRoleRepo)(nil)
	_ domain.TenantRoleAssignmentRepository = (*TenantRoleAssignmentRepo)(nil)
)

// TenantRoleRepo mengakses gov.tenant_roles + gov.tenant_role_permissions pada TENANT DB.
// Memegang *db.Pool (bukan db.Conn) karena Save menulis role + grant permission-nya secara
// atomik dalam satu transaksi. Pola RBAC kanonik yang sama dengan CentralRoleRepo (id.*).
type TenantRoleRepo struct {
	pool *db.Pool
}

func NewTenantRoleRepo(pool *db.Pool) *TenantRoleRepo { return &TenantRoleRepo{pool: pool} }

// Save menulis role + seluruh permission-nya atomik. Nama duplikat → core.ErrConflict.
// Schema dipastikan ada lebih dulu (ensure-on-write) di luar transaksi role.
func (r *TenantRoleRepo) Save(ctx context.Context, role *domain.TenantRole) error {
	if err := ensureTenantRoleSchema(ctx, r.pool); err != nil {
		return err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op setelah commit

	const insRole = `INSERT INTO gov.tenant_roles (id, name, label, description)
	    VALUES ($1,$2,$3,$4)`
	if _, err := tx.Exec(ctx, insRole, role.ID, role.Name, role.Label, role.Description); err != nil {
		if db.IsUniqueViolation(err) {
			return core.ErrConflict("role tenant sudah ada: " + role.Name)
		}
		return err
	}

	// ON CONFLICT DO NOTHING: grant role→permission idempoten (himpunan). Permission yang
	// sama dua kali tidak menggandakan baris maupun menggagalkan transaksi — batas
	// pertahanan untuk SEMUA caller; use case juga men-dedup.
	const insPerm = `INSERT INTO gov.tenant_role_permissions (role_id, permission)
	    VALUES ($1,$2) ON CONFLICT (role_id, permission) DO NOTHING`
	for _, p := range role.Permissions {
		if _, err := tx.Exec(ctx, insPerm, role.ID, p); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

const tenantRoleCols = `id, name, label, description, created_at`

func (r *TenantRoleRepo) FindByID(ctx context.Context, id uuid.UUID) (*domain.TenantRole, error) {
	if err := ensureTenantRoleSchema(ctx, r.pool); err != nil {
		return nil, err
	}
	role, err := r.scanOne(r.pool.QueryRow(ctx,
		`SELECT `+tenantRoleCols+` FROM gov.tenant_roles WHERE id = $1`, id), id.String())
	if err != nil {
		return nil, err
	}
	if role.Permissions, err = r.permissionsOf(ctx, role.ID); err != nil {
		return nil, err
	}
	return role, nil
}

func (r *TenantRoleRepo) FindByName(ctx context.Context, name string) (*domain.TenantRole, error) {
	if err := ensureTenantRoleSchema(ctx, r.pool); err != nil {
		return nil, err
	}
	role, err := r.scanOne(r.pool.QueryRow(ctx,
		`SELECT `+tenantRoleCols+` FROM gov.tenant_roles WHERE name = $1`, name), name)
	if err != nil {
		return nil, err
	}
	if role.Permissions, err = r.permissionsOf(ctx, role.ID); err != nil {
		return nil, err
	}
	return role, nil
}

// List mengembalikan seluruh role tenant dengan permission-nya terisi. Menghindari N+1:
// satu query role + satu query permission, lalu di-stitch per role_id.
func (r *TenantRoleRepo) List(ctx context.Context) ([]*domain.TenantRole, error) {
	if err := ensureTenantRoleSchema(ctx, r.pool); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `SELECT `+tenantRoleCols+` FROM gov.tenant_roles ORDER BY name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byID := make(map[uuid.UUID]*domain.TenantRole)
	var out []*domain.TenantRole
	for rows.Next() {
		role, err := scanTenantRole(rows)
		if err != nil {
			return nil, err
		}
		byID[role.ID] = role
		out = append(out, role)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	prows, err := r.pool.Query(ctx, `SELECT role_id, permission FROM gov.tenant_role_permissions`)
	if err != nil {
		return nil, err
	}
	defer prows.Close()
	for prows.Next() {
		var roleID uuid.UUID
		var perm string
		if err := prows.Scan(&roleID, &perm); err != nil {
			return nil, err
		}
		if role, ok := byID[roleID]; ok {
			role.Permissions = append(role.Permissions, perm)
		}
	}
	return out, prows.Err()
}

func (r *TenantRoleRepo) permissionsOf(ctx context.Context, roleID uuid.UUID) ([]string, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT permission FROM gov.tenant_role_permissions WHERE role_id = $1 ORDER BY permission ASC`, roleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *TenantRoleRepo) scanOne(row interface{ Scan(...any) error }, ref string) (*domain.TenantRole, error) {
	role, err := scanTenantRole(row)
	if err != nil {
		if db.IsNoRows(err) {
			return nil, core.ErrNotFound("TenantRole", ref)
		}
		return nil, err
	}
	return role, nil
}

func scanTenantRole(row interface{ Scan(...any) error }) (*domain.TenantRole, error) {
	var role domain.TenantRole
	if err := row.Scan(&role.ID, &role.Name, &role.Label, &role.Description, &role.CreatedAt); err != nil {
		return nil, err
	}
	return &role, nil
}

// TenantRoleAssignmentRepo mengakses gov.user_role_assignments pada TENANT DB.
type TenantRoleAssignmentRepo struct {
	conn db.Conn
}

func NewTenantRoleAssignmentRepo(conn db.Conn) *TenantRoleAssignmentRepo {
	return &TenantRoleAssignmentRepo{conn: conn}
}

const tenantAssignmentCols = `id, user_id, role_id, unit_kerja_id, include_subtree, assigned_by, valid_from, valid_until, created_at`

func (r *TenantRoleAssignmentRepo) Save(ctx context.Context, a *domain.TenantRoleAssignment) error {
	if err := ensureTenantRoleSchema(ctx, r.conn); err != nil {
		return err
	}
	const q = `INSERT INTO gov.user_role_assignments
	    (id, user_id, role_id, unit_kerja_id, include_subtree, assigned_by, valid_from, valid_until)
	    VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`
	_, err := r.conn.Exec(ctx, q, a.ID, a.UserID, a.RoleID, a.UnitKerjaID, a.IncludeSubtree,
		a.AssignedBy, a.ValidFrom, a.ValidUntil)
	return err
}

func (r *TenantRoleAssignmentRepo) ListByUser(ctx context.Context, userID uuid.UUID) ([]*domain.TenantRoleAssignment, error) {
	if err := ensureTenantRoleSchema(ctx, r.conn); err != nil {
		return nil, err
	}
	rows, err := r.conn.Query(ctx,
		`SELECT `+tenantAssignmentCols+` FROM gov.user_role_assignments WHERE user_id = $1 ORDER BY valid_from ASC`,
		userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*domain.TenantRoleAssignment
	for rows.Next() {
		var a domain.TenantRoleAssignment
		if err := rows.Scan(&a.ID, &a.UserID, &a.RoleID, &a.UnitKerjaID, &a.IncludeSubtree,
			&a.AssignedBy, &a.ValidFrom, &a.ValidUntil, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &a)
	}
	return out, rows.Err()
}
