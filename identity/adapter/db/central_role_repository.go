package db

import (
	"context"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/infra/db"
)

var (
	_ domain.CentralRoleRepository           = (*CentralRoleRepo)(nil)
	_ domain.CentralRoleAssignmentRepository = (*CentralRoleAssignmentRepo)(nil)
)

// CentralRoleRepo mengakses id.central_roles + id.central_role_permissions pada identity DB.
// Memegang *db.Pool (bukan db.Conn) karena Save menulis role + grant permission-nya secara
// atomik dalam satu transaksi.
type CentralRoleRepo struct {
	pool *db.Pool
}

func NewCentralRoleRepo(pool *db.Pool) *CentralRoleRepo { return &CentralRoleRepo{pool: pool} }

// Save menulis role + seluruh permission-nya atomik. Nama duplikat → core.ErrConflict.
func (r *CentralRoleRepo) Save(ctx context.Context, role *domain.CentralRole) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op setelah commit

	const insRole = `INSERT INTO id.central_roles (id, name, label, scope_type, description)
	    VALUES ($1,$2,$3,$4,$5)`
	if _, err := tx.Exec(ctx, insRole, role.ID, role.Name, role.Label, string(role.ScopeType), role.Description); err != nil {
		if db.IsUniqueViolation(err) {
			return core.ErrConflict("role sentral sudah ada: " + role.Name)
		}
		return err
	}

	// ON CONFLICT DO NOTHING: grant role→permission idempoten. Permission yang sama diberikan
	// dua kali (mis. UI menggabung beberapa permission group) tidak menggandakan baris maupun
	// menggagalkan transaksi (perbaikan review PR-2.3.2: sebelumnya pelanggaran PK
	// (role_id,permission) lolos tanpa dipetakan ke core.ErrConflict → error pgx mentah/500 &
	// role batal dibuat). Ini batas pertahanan untuk SEMUA caller; use case juga men-dedup.
	const insPerm = `INSERT INTO id.central_role_permissions (role_id, permission)
	    VALUES ($1,$2) ON CONFLICT (role_id, permission) DO NOTHING`
	for _, p := range role.Permissions {
		if _, err := tx.Exec(ctx, insPerm, role.ID, p); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

const centralRoleCols = `id, name, label, scope_type, description, created_at`

func (r *CentralRoleRepo) FindByID(ctx context.Context, id uuid.UUID) (*domain.CentralRole, error) {
	role, err := r.scanOne(r.pool.QueryRow(ctx,
		`SELECT `+centralRoleCols+` FROM id.central_roles WHERE id = $1`, id), id.String())
	if err != nil {
		return nil, err
	}
	if role.Permissions, err = r.permissionsOf(ctx, role.ID); err != nil {
		return nil, err
	}
	return role, nil
}

func (r *CentralRoleRepo) FindByName(ctx context.Context, name string) (*domain.CentralRole, error) {
	role, err := r.scanOne(r.pool.QueryRow(ctx,
		`SELECT `+centralRoleCols+` FROM id.central_roles WHERE name = $1`, name), name)
	if err != nil {
		return nil, err
	}
	if role.Permissions, err = r.permissionsOf(ctx, role.ID); err != nil {
		return nil, err
	}
	return role, nil
}

// List mengembalikan seluruh role sentral dengan permission-nya terisi. Menghindari N+1:
// satu query role + satu query permission, lalu di-stitch per role_id.
func (r *CentralRoleRepo) List(ctx context.Context) ([]*domain.CentralRole, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+centralRoleCols+` FROM id.central_roles ORDER BY name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byID := make(map[uuid.UUID]*domain.CentralRole)
	var out []*domain.CentralRole
	for rows.Next() {
		role, err := scanCentralRole(rows)
		if err != nil {
			return nil, err
		}
		byID[role.ID] = role
		out = append(out, role)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	prows, err := r.pool.Query(ctx, `SELECT role_id, permission FROM id.central_role_permissions`)
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

func (r *CentralRoleRepo) permissionsOf(ctx context.Context, roleID uuid.UUID) ([]string, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT permission FROM id.central_role_permissions WHERE role_id = $1 ORDER BY permission ASC`, roleID)
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

func (r *CentralRoleRepo) scanOne(row interface{ Scan(...any) error }, ref string) (*domain.CentralRole, error) {
	role, err := scanCentralRole(row)
	if err != nil {
		if db.IsNoRows(err) {
			return nil, core.ErrNotFound("CentralRole", ref)
		}
		return nil, err
	}
	return role, nil
}

func scanCentralRole(row interface{ Scan(...any) error }) (*domain.CentralRole, error) {
	var role domain.CentralRole
	var scope string
	if err := row.Scan(&role.ID, &role.Name, &role.Label, &scope, &role.Description, &role.CreatedAt); err != nil {
		return nil, err
	}
	role.ScopeType = domain.ScopeType(scope)
	return &role, nil
}

// CentralRoleAssignmentRepo mengakses id.central_role_assignments pada identity DB.
type CentralRoleAssignmentRepo struct {
	conn db.Conn
}

func NewCentralRoleAssignmentRepo(conn db.Conn) *CentralRoleAssignmentRepo {
	return &CentralRoleAssignmentRepo{conn: conn}
}

const centralAssignmentCols = `id, person_id, role_id, tenant_scope, assigned_by, valid_from, valid_until, created_at`

func (r *CentralRoleAssignmentRepo) Save(ctx context.Context, a *domain.CentralRoleAssignment) error {
	const q = `INSERT INTO id.central_role_assignments
	    (id, person_id, role_id, tenant_scope, assigned_by, valid_from, valid_until)
	    VALUES ($1,$2,$3,$4,$5,$6,$7)`
	_, err := r.conn.Exec(ctx, q, a.ID, a.PersonID, a.RoleID, a.TenantScope, a.AssignedBy, a.ValidFrom, a.ValidUntil)
	return err
}

func (r *CentralRoleAssignmentRepo) ListByPerson(ctx context.Context, personID uuid.UUID) ([]*domain.CentralRoleAssignment, error) {
	rows, err := r.conn.Query(ctx,
		`SELECT `+centralAssignmentCols+` FROM id.central_role_assignments WHERE person_id = $1 ORDER BY valid_from ASC`,
		personID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*domain.CentralRoleAssignment
	for rows.Next() {
		var a domain.CentralRoleAssignment
		if err := rows.Scan(&a.ID, &a.PersonID, &a.RoleID, &a.TenantScope, &a.AssignedBy,
			&a.ValidFrom, &a.ValidUntil, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &a)
	}
	return out, rows.Err()
}
