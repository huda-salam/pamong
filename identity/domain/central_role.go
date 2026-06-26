package domain

import (
	"regexp"
	"time"

	"github.com/google/uuid"
)

// Role sentral (PR-2.3.2). Dikelola admin platform, disimpan di identity DB sentral
// (id.central_roles), berlaku lintas tenant. Dua sub-tipe (CLAUDE.md "Lapisan 1"):
//   - global → berlaku di SEMUA tenant tanpa terkecuali (mis. super_admin).
//   - scoped → berlaku hanya di tenant dalam tenant_scope assignment (mis. regional_helpdesk).
//
// Evaluasi permission-nya ada di core/permission.Engine (lewat catalog DB). Pencocokan
// scope (tenant mana yang berlaku) dikurung di resolver — lihat CentralRoleAssignment.AppliesTo.

// centralRoleNameRe: snake_case, mis. "super_admin", "regional_helpdesk".
var centralRoleNameRe = regexp.MustCompile(`^[a-z][a-z0-9_]{2,99}$`)

// ScopeType menandai jangkauan role sentral.
type ScopeType string

const (
	ScopeGlobal ScopeType = "global" // berlaku semua tenant
	ScopeScoped ScopeType = "scoped" // berlaku hanya tenant dalam tenant_scope
)

// CentralRole adalah satu definisi role sentral beserta permission yang diberikannya.
// Permissions berisi string {modul}:{entity}:{aksi} apa adanya (sumber: manifest modul);
// validasi terhadap registry manifest menyusul PR-2.3.4.
type CentralRole struct {
	ID          uuid.UUID
	Name        string
	Label       string
	ScopeType   ScopeType
	Description string
	Permissions []string
	CreatedAt   time.Time
}

// Validate memeriksa invariant role sentral tanpa I/O.
func (r *CentralRole) Validate() error {
	if !centralRoleNameRe.MatchString(r.Name) {
		return ErrCentralRoleNameInvalid
	}
	if r.Label == "" {
		return ErrCentralRoleLabelKosong
	}
	if r.ScopeType != ScopeGlobal && r.ScopeType != ScopeScoped {
		return ErrScopeTypeInvalid
	}
	return nil
}

// CentralRoleAssignment menugaskan sebuah role sentral ke person. tenant_scope kosong =
// role global (semua tenant); berisi = scoped (hanya tenant dalam daftar). Koherensi antara
// scope_type role dan tenant_scope ditegakkan di use case AssignCentralRole (yang memuat role).
type CentralRoleAssignment struct {
	ID          uuid.UUID
	PersonID    uuid.UUID
	RoleID      uuid.UUID
	TenantScope []string
	AssignedBy  uuid.UUID
	ValidFrom   time.Time
	ValidUntil  *time.Time // nil = berlaku tak terbatas
	CreatedAt   time.Time
}

// Validate memeriksa invariant assignment tanpa I/O.
func (a *CentralRoleAssignment) Validate() error {
	if a.PersonID == uuid.Nil {
		return ErrPersonIDKosong
	}
	if a.RoleID == uuid.Nil {
		return ErrRoleIDKosong
	}
	if a.AssignedBy == uuid.Nil {
		return ErrAssignedByKosong
	}
	return nil
}

// AppliesTo melaporkan apakah assignment ini aktif untuk tenantID pada saat now. Inilah
// satu-satunya tempat arti "scope" diputuskan (keputusan PR-2.3.2): memperdalamnya nanti
// (mis. token region 'prov:jatim' untuk wildcard provinsi) cukup mengubah fungsi ini —
// engine, catalog, dan skema tidak tersentuh. Fungsi murni → teruji tanpa DB.
//
// Aturan: di luar masa berlaku → tidak aktif. scope_type=global → berlaku semua tenant
// (tenant_scope diabaikan). scope_type=scoped → hanya bila tenantID ada di tenant_scope
// (exact match; wildcard ditunda).
//
// PERBAIKAN review PR-2.3.2 (fail-closed): otoritas global vs scoped adalah `scope` (yakni
// central_roles.scope_type, diteruskan caller/resolver), BUKAN kekosongan tenant_scope.
// Versi sebelumnya menyimpulkan "tenant_scope kosong ⇒ global", sehingga assignment SCOPED
// yang—karena data rusak yang melewati use case (insert langsung/migrasi/bulk-import)—punya
// tenant_scope kosong akan berlaku di SEMUA tenant: eskalasi wewenang diam-diam. Dengan
// keying ke scope_type, scoped-tanpa-tenant kini berlaku di MANA PUN TIDAK (gagal aman).
// Read-path tidak mengandalkan invariant write-time (checkScopeCoherence) tetap utuh.
func (a *CentralRoleAssignment) AppliesTo(scope ScopeType, tenantID string, now time.Time) bool {
	if now.Before(a.ValidFrom) {
		return false
	}
	if a.ValidUntil != nil && !now.Before(*a.ValidUntil) {
		return false
	}
	if scope == ScopeGlobal {
		return true
	}
	for _, s := range a.TenantScope {
		if s == tenantID {
			return true
		}
	}
	return false
}
