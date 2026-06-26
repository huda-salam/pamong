package db

import (
	"context"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core/permission"
	"github.com/huda-salam/pamong/infra/db"
)

// orgUnitDDL membuat tabel hierarki OPD minimal (adjacency list: parent_id) bila belum ada.
// Ini PLACEHOLDER untuk modul OPD penuh (entitas kaya: jabatan/eselon, CRUD, UI) yang menyusul —
// saat itu modul tsb menjadi pemilik/penulis gov.org_units lewat port Hierarchy yang SAMA
// (non-breaking). EnsureSchema-on-write (preseden gov.user_profiles / gov.tenant_roles): tabel
// framework gov.* belum punya runner migrasi formal. Adjacency dipilih (bukan closure/ltree):
// tree OPD dangkal & jarang bermutasi, dan ltree butuh ekstensi PG yang tak terjamin di Tier-3.
// DEFERRED(Phase-2.x): runner migrasi framework-gov formal + FK unit_kerja_id→org_units (ROADMAP).
const orgUnitDDL = `
CREATE SCHEMA IF NOT EXISTS gov;
CREATE TABLE IF NOT EXISTS gov.org_units (
    id        UUID PRIMARY KEY,
    parent_id UUID REFERENCES gov.org_units(id),
    name      VARCHAR(255) NOT NULL
);`

func ensureOrgUnitSchema(ctx context.Context, exec db.Conn) error {
	_, err := exec.Exec(ctx, orgUnitDDL)
	return err
}

// OrgUnitHierarchy mengimplementasi permission.Hierarchy terhadap gov.org_units pada TENANT DB
// (isolasi per-tenant struktural: konek ke tenant DB-nya sendiri).
type OrgUnitHierarchy struct {
	conn db.Conn
}

var _ permission.Hierarchy = (*OrgUnitHierarchy)(nil)

func NewOrgUnitHierarchy(conn db.Conn) *OrgUnitHierarchy { return &OrgUnitHierarchy{conn: conn} }

// IsWithin melaporkan apakah unit == root atau keturunan root. Menelusuri ANCESTOR unit KE ATAS
// (tree OPD dangkal) lalu memeriksa apakah root terlewati — recursive CTE, satu query. Arah
// naik-dari-unit lebih hemat daripada mengembang seluruh subtree root.
func (h *OrgUnitHierarchy) IsWithin(ctx context.Context, root, unit uuid.UUID) (bool, error) {
	if root == unit {
		return true, nil
	}
	if err := ensureOrgUnitSchema(ctx, h.conn); err != nil {
		return false, err
	}
	const q = `
	WITH RECURSIVE anc AS (
	    SELECT id, parent_id FROM gov.org_units WHERE id = $1
	    UNION ALL
	    SELECT u.id, u.parent_id FROM gov.org_units u JOIN anc ON u.id = anc.parent_id
	)
	SELECT EXISTS (SELECT 1 FROM anc WHERE id = $2)`
	var within bool
	if err := h.conn.QueryRow(ctx, q, unit, root).Scan(&within); err != nil {
		return false, err
	}
	return within, nil
}
