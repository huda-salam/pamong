package db

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core/permission"
	"github.com/huda-salam/pamong/infra/db"
	"github.com/huda-salam/pamong/tenantrole/domain"
)

// TenantScopedGrantResolver membangun scoped-grant lapis tenant untuk satu user: tiap assignment
// role tenant yang AKTIF dikalikan permission role-nya, membawa jangkauan unit (UnitKerjaID nil →
// TenantWide; IncludeSubtree → keturunan unit pada hierarki OPD). Inilah masukan Tahap 2 ABAC
// (core/permission.Authority.RoleGrants), pelengkap nama role dari TenantRoleResolver (Tahap 1).
//
// Isolasi per-tenant bersifat STRUKTURAL: resolver hanya melihat gov.* milik tenant DB yang
// dikoneksikan — tanpa parameter tenantID, sama seperti TenantRoleResolver.
type TenantScopedGrantResolver struct {
	conn db.Conn
	now  func() time.Time
}

func NewTenantScopedGrantResolver(conn db.Conn) *TenantScopedGrantResolver {
	return &TenantScopedGrantResolver{conn: conn, now: time.Now}
}

// Grants mengembalikan scoped-grant lapis tenant yang berlaku untuk user saat ini. JOIN dibatasi
// schema gov (no-cross-schema-join). Assignment di luar masa berlaku diabaikan (AppliesTo).
func (r *TenantScopedGrantResolver) Grants(ctx context.Context, userID uuid.UUID) ([]permission.Grant, error) {
	if err := ensureTenantRoleSchema(ctx, r.conn); err != nil {
		return nil, err
	}
	const q = `SELECT trp.permission, ura.unit_kerja_id, ura.include_subtree, ura.valid_from, ura.valid_until
	    FROM gov.user_role_assignments ura
	    JOIN gov.tenant_role_permissions trp ON trp.role_id = ura.role_id
	    WHERE ura.user_id = $1`
	rows, err := r.conn.Query(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	now := r.now()
	var out []permission.Grant
	for rows.Next() {
		var perm string
		var unit *uuid.UUID
		var subtree bool
		var a domain.TenantRoleAssignment
		if err := rows.Scan(&perm, &unit, &subtree, &a.ValidFrom, &a.ValidUntil); err != nil {
			return nil, err
		}
		if !a.AppliesTo(now) {
			continue
		}
		g := permission.Grant{Permission: perm}
		if unit == nil {
			g.TenantWide = true
		} else {
			g.UnitKerjaID = *unit
			g.Subtree = subtree
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
