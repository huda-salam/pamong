package db

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/infra/db"
)

// CentralRoleResolver menentukan nama role sentral yang AKTIF untuk seorang person pada satu
// tenant. Inilah lapis tempat scope di-resolve: global selalu masuk, scoped hanya bila tenant
// ada di tenant_scope, dan assignment di luar masa berlaku diabaikan (semua lewat
// domain.CentralRoleAssignment.AppliesTo). Hasilnya — nama role — diumpankan ke
// permission.Engine bersama role tenant; Engine sendiri tetap tenant-agnostic (keputusan
// PR-2.3.2). Saat auth flow (2.4) aktif, effective-role inilah yang dibake ke token per tenant.
type CentralRoleResolver struct {
	conn db.Conn
	now  func() time.Time
}

func NewCentralRoleResolver(conn db.Conn) *CentralRoleResolver {
	return &CentralRoleResolver{conn: conn, now: time.Now}
}

// EffectiveRoles mengembalikan nama role sentral yang berlaku untuk person di tenantID saat ini.
//
// Mengambil cr.scope_type (OTORITATIF) untuk keputusan global vs scoped — bukan menyimpulkan
// global dari tenant_scope kosong (perbaikan review PR-2.3.2: cegah scoped-tanpa-tenant
// berlaku di mana-mana / fail-open). Lihat domain.CentralRoleAssignment.AppliesTo.
func (r *CentralRoleResolver) EffectiveRoles(ctx context.Context, personID uuid.UUID, tenantID string) ([]string, error) {
	const q = `SELECT cr.name, cr.scope_type, cra.tenant_scope, cra.valid_from, cra.valid_until
	    FROM id.central_role_assignments cra
	    JOIN id.central_roles cr ON cr.id = cra.role_id
	    WHERE cra.person_id = $1`
	rows, err := r.conn.Query(ctx, q, personID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	now := r.now()
	var out []string
	for rows.Next() {
		var name, scope string
		var a domain.CentralRoleAssignment
		if err := rows.Scan(&name, &scope, &a.TenantScope, &a.ValidFrom, &a.ValidUntil); err != nil {
			return nil, err
		}
		if a.AppliesTo(domain.ScopeType(scope), tenantID, now) {
			out = append(out, name)
		}
	}
	return out, rows.Err()
}
