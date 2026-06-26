// Package domain memodelkan role tenant (PR-2.3.3): role yang dikelola admin tenant,
// disimpan di tenant DB (schema gov), dan berlaku HANYA di dalam tenant-nya. Ini lapis
// kedua model role (CLAUDE.md "Lapisan 2") — pelengkap role sentral (identity DB) yang
// dimodelkan di identity/domain. Evaluasi permission-nya ada di core/permission.Engine
// lewat catalog DB tenant; di sini hanya entity + invariant tanpa I/O.
package domain

import (
	"regexp"
	"time"

	"github.com/google/uuid"
)

// tenantRoleNameRe: snake_case bebas sesuai kebutuhan OPD (CLAUDE.md), mis.
// "bendahara_pengeluaran", "ppk_opd", "verifikator_keuangan".
var tenantRoleNameRe = regexp.MustCompile(`^[a-z][a-z0-9_]{2,99}$`)

// TenantRole adalah satu definisi role tenant beserta grant permission-nya. Permissions
// berisi string {modul}:{entity}:{aksi} apa adanya (sumber: manifest modul); validasi
// terhadap registry manifest menyusul PR-2.3.4. Tidak ada scope_type seperti role sentral —
// role tenant selalu berlaku pada satu tenant (tenant DB tempat ia disimpan).
type TenantRole struct {
	ID          uuid.UUID
	Name        string
	Label       string
	Description string
	Permissions []string
	CreatedAt   time.Time
}

// Validate memeriksa invariant role tenant tanpa I/O.
func (r *TenantRole) Validate() error {
	if !tenantRoleNameRe.MatchString(r.Name) {
		return ErrTenantRoleNameInvalid
	}
	if r.Label == "" {
		return ErrTenantRoleLabelKosong
	}
	return nil
}

// TenantRoleAssignment menugaskan role tenant ke seorang user (gov.user_profiles.id =
// person_id). UnitKerjaID menyempitkan scope ke satu unit kerja; nil = seluruh tenant.
//
// Penegakan scope unit kerja (ABAC data-level) AKTIF sejak PR-2.3.5 di core/permission.ScopedEngine:
// resolver (adapter/db.TenantScopedGrantResolver) memetakan tiap assignment+permission ke
// permission.Grant — UnitKerjaID nil → TenantWide, IncludeSubtree → menjangkau keturunan unit
// pada hierarki OPD. Engine RBAC (Engine.Allows) tetap scope-agnostik; scope dievaluasi terpisah.
type TenantRoleAssignment struct {
	ID             uuid.UUID
	UserID         uuid.UUID  // -> gov.user_profiles(id)
	RoleID         uuid.UUID  // -> gov.tenant_roles(id)
	UnitKerjaID    *uuid.UUID // nil = seluruh tenant (TenantWide)
	IncludeSubtree bool       // saat UnitKerjaID diisi: jangkau keturunan unit (hierarki OPD)
	AssignedBy     uuid.UUID
	ValidFrom      time.Time
	ValidUntil     *time.Time // nil = berlaku tak terbatas
	CreatedAt      time.Time
}

// Validate memeriksa invariant assignment tanpa I/O.
func (a *TenantRoleAssignment) Validate() error {
	if a.UserID == uuid.Nil {
		return ErrUserIDKosong
	}
	if a.RoleID == uuid.Nil {
		return ErrRoleIDKosong
	}
	if a.AssignedBy == uuid.Nil {
		return ErrAssignedByKosong
	}
	return nil
}

// AppliesTo melaporkan apakah assignment aktif pada saat now (dalam masa berlaku).
// Berbeda dari role sentral, tidak ada pencocokan scope tenant: assignment ini hidup di
// tenant DB-nya sendiri, sehingga "berlaku hanya di tenant-nya" terpenuhi secara struktural.
// Scope unit kerja bukan urusan masa berlaku — ia dievaluasi di core/permission.ScopedEngine
// (data-level), bukan di sini.
func (a *TenantRoleAssignment) AppliesTo(now time.Time) bool {
	if now.Before(a.ValidFrom) {
		return false
	}
	if a.ValidUntil != nil && !now.Before(*a.ValidUntil) {
		return false
	}
	return true
}
