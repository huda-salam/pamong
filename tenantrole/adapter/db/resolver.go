package db

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/infra/db"
	"github.com/huda-salam/pamong/tenantrole/domain"
)

// TenantRoleResolver menentukan nama role tenant yang AKTIF untuk seorang user di TENANT DB
// tempat resolver ini terhubung. Inilah jaminan struktural DoD "role tenant hanya berlaku di
// tenant-nya": resolver hanya melihat gov.* milik tenant yang sedang dikoneksikan — tidak ada
// parameter tenantID & tidak ada pencocokan scope tenant seperti role sentral.
//
// Hasilnya — nama role — diumpankan ke permission.Engine bersama nama role sentral; Engine
// membaca Layer tiap nama dari CompositeCatalog (central + tenant). Saat auth flow (2.4) aktif,
// effective-role inilah yang dibake ke token per tenant.
type TenantRoleResolver struct {
	conn db.Conn
	now  func() time.Time
}

func NewTenantRoleResolver(conn db.Conn) *TenantRoleResolver {
	return &TenantRoleResolver{conn: conn, now: time.Now}
}

// EffectiveRoles mengembalikan nama role tenant yang berlaku untuk user saat ini (assignment
// dalam masa berlaku; lihat domain.TenantRoleAssignment.AppliesTo). JOIN dibatasi schema gov
// (no-cross-schema-join). Scope unit kerja tidak difilter di sini (DEFERRED 2.3.5).
func (r *TenantRoleResolver) EffectiveRoles(ctx context.Context, userID uuid.UUID) ([]string, error) {
	if err := ensureTenantRoleSchema(ctx, r.conn); err != nil {
		return nil, err
	}
	const q = `SELECT tr.name, ura.valid_from, ura.valid_until
	    FROM gov.user_role_assignments ura
	    JOIN gov.tenant_roles tr ON tr.id = ura.role_id
	    WHERE ura.user_id = $1`
	rows, err := r.conn.Query(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	now := r.now()
	var out []string
	for rows.Next() {
		var name string
		var a domain.TenantRoleAssignment
		if err := rows.Scan(&name, &a.ValidFrom, &a.ValidUntil); err != nil {
			return nil, err
		}
		if a.AppliesTo(now) {
			out = append(out, name)
		}
	}
	return out, rows.Err()
}
