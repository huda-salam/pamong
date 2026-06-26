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
