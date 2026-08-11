// Package domain memodelkan role tenant (PR-2.3.3): role yang dikelola admin tenant,
// disimpan di tenant DB (schema gov), dan berlaku HANYA di dalam tenant-nya. Ini lapis
// kedua model role (CLAUDE.md "Lapisan 2") — pelengkap role sentral (identity DB) yang
// dimodelkan di identity/domain. Evaluasi permission-nya ada di core/permission.Engine
// lewat catalog DB tenant; di sini hanya entity + invariant tanpa I/O.
package domain

import (
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// reservedPermissionPrefix adalah namespace permission yang HANYA boleh diberikan lapis SENTRAL
// (id.central_roles), tak pernah oleh role tenant.
//
// Alasannya bukan kerapian namespace melainkan penutupan jalur ESKALASI. core/permission.Engine
// menggabungkan grant lintas lapis secara UNION (satu role non-global yang memberi permission
// sudah cukup), dan nama role tenant yang dipegang seseorang ikut ke klaim token yang sama dengan
// role sentralnya. Tanpa pagar ini, admin tenant yang memegang `iam:tenant_role:buat` dapat
// membuat role tenant berisi `identity:credential:buat`, menugaskannya ke dirinya sendiri, lalu
// menerbitkan kredensial ber-password pilihannya untuk person MANA PUN yang id-nya ia ketahui —
// termasuk admin platform yang ter-clone ke tenantnya — dan login sebagai orang itu. Satu tenant
// karena itu dapat mengambil alih seluruh platform.
//
// Kelemahan ini DORMAN sampai PR-W2: sebelum ada permukaan HTTP-nya, tak ada permission
// `identity:*` yang bisa dieksekusi siapa pun. Memasang `/admin/identity/*` tanpa pagar ini
// berarti mempromosikan kelemahan dorman menjadi permukaan serang nyata — pola yang sama dengan
// proteksi brute-force yang ikut dibayar PR-W1 saat `/auth/login` dipasang.
//
// Ditegakkan di DOMAIN (pintu masuk definisi role), bukan di titik evaluasi: Engine sengaja
// scope-agnostik dan tak tahu lapis mana yang "berhak" atas sebuah string, sementara di sini
// aturannya bisa dinyatakan sekali dan berlaku untuk setiap penulis role tenant — sekarang dan
// nanti. Permission tenant yang sah (`iam:`, `customization:`, modul bisnis) tak tersentuh.
const reservedPermissionPrefix = "identity:"

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

// Validate memeriksa invariant role tenant tanpa I/O — termasuk pagar namespace `identity:`
// (lihat reservedPermissionPrefix), yang menutup jalur eskalasi tenant → platform.
func (r *TenantRole) Validate() error {
	if !tenantRoleNameRe.MatchString(r.Name) {
		return ErrTenantRoleNameInvalid
	}
	if r.Label == "" {
		return ErrTenantRoleLabelKosong
	}
	for _, p := range r.Permissions {
		if strings.HasPrefix(p, reservedPermissionPrefix) {
			return ErrPermissionTerlarangTenant
		}
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
	if a.UnitKerjaID != nil && *a.UnitKerjaID == uuid.Nil {
		return ErrUnitKerjaNol
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
