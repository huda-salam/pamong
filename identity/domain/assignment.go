package domain

import (
	"time"

	"github.com/google/uuid"
)

// TenantAssignment menugaskan sebuah employment ke tenant — inilah yang memunculkan
// persona employee di tenant tersebut. Citizen TIDAK butuh baris ini (akses publik
// tersedia untuk semua person tanpa kaitan tenant). Penugasan menempel pada employment,
// bukan langsung pada person (CLAUDE.md "Identity").
//
// is_home_tenant=false menandai penugasan cross-tenant (mis. PJ Bupati dari Pemprov) yang
// butuh otorisasi khusus di use case. Otorisasi penuh cross-tenant (PLT, pemilihan tenant)
// dilengkapi PR-2.4.5; di sini hanya constraint dasar + gerbang permission.
type TenantAssignment struct {
	ID           uuid.UUID
	EmploymentID uuid.UUID
	TenantID     string
	IsHomeTenant bool
	AssignedBy   uuid.UUID
	ValidFrom    time.Time
	ValidUntil   *time.Time // nil = berlaku tak terbatas
	CreatedAt    time.Time
}

// Validate memeriksa invariant penugasan tanpa I/O.
func (a *TenantAssignment) Validate() error {
	if a.EmploymentID == uuid.Nil {
		return ErrEmploymentIDKosong
	}
	if !tenantIDRe.MatchString(a.TenantID) {
		return ErrTenantIDInvalid
	}
	if a.AssignedBy == uuid.Nil {
		return ErrAssignedByKosong
	}
	return nil
}

// AppliesTo melaporkan apakah penugasan aktif pada saat now (dalam [ValidFrom, ValidUntil)).
// Tidak ada flag is_active terpisah pada id.tenant_assignments — masa berlaku yang menentukan.
// Dipakai alur login (PR-2.4.3) untuk menyaring tenant yang berhak dimasuki person. Fungsi
// murni — teruji tanpa DB.
func (a *TenantAssignment) AppliesTo(now time.Time) bool {
	if now.Before(a.ValidFrom) {
		return false
	}
	if a.ValidUntil != nil && !now.Before(*a.ValidUntil) {
		return false
	}
	return true
}
