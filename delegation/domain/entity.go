// Package domain memodelkan delegasi/PLT (PR-2.3.5): pelimpahan SUBSET permission dari satu
// user ke user lain dalam SATU tenant untuk rentang waktu terbatas (PRD F5). Disimpan di tenant
// DB (schema gov). Berbeda dari penugasan cross-tenant (PJ/PLT antar tenant = id.tenant_assignments,
// ranah identity/2.4.5): delegasi di sini intra-tenant. Evaluasinya di core/permission.ScopedEngine
// (delegasi aktif → scoped-grant); di sini hanya entity + invariant tanpa I/O.
package domain

import (
	"time"

	"github.com/google/uuid"
)

// Delegation melimpahkan Permissions dari FromUserID ke ToUserID untuk [ValidFrom, ValidUntil).
// SELALU berbatas waktu (ValidUntil wajib) — kedaluwarsa otomatis & lazy saat evaluasi
// (AppliesTo), tak ada delegasi permanen (PRD anti-pattern). UnitKerjaID menyempitkan jangkauan
// data (nil = seluruh tenant); IncludeSubtree memperluas ke keturunan unit pada hierarki OPD.
type Delegation struct {
	ID             uuid.UUID
	FromUserID     uuid.UUID
	ToUserID       uuid.UUID
	Permissions    []string
	UnitKerjaID    *uuid.UUID
	IncludeSubtree bool
	Reason         string
	ValidFrom      time.Time
	ValidUntil     time.Time // wajib & di masa depan dari ValidFrom — selalu berbatas
	AssignedBy     uuid.UUID
	CreatedAt      time.Time
}

// Validate memeriksa invariant delegasi tanpa I/O.
func (d *Delegation) Validate() error {
	if d.FromUserID == uuid.Nil {
		return ErrFromUserKosong
	}
	if d.ToUserID == uuid.Nil {
		return ErrToUserKosong
	}
	if d.FromUserID == d.ToUserID {
		return ErrDelegasiKeDiriSendiri
	}
	if d.AssignedBy == uuid.Nil {
		return ErrAssignedByKosong
	}
	if len(d.Permissions) == 0 {
		return ErrPermissionsKosong
	}
	if d.ValidUntil.IsZero() {
		return ErrValidUntilWajib
	}
	if !d.ValidUntil.After(d.ValidFrom) {
		return ErrPeriodeTerbalik
	}
	return nil
}

// AppliesTo melaporkan apakah delegasi aktif pada now (dalam masa berlaku). Kedaluwarsa =
// otomatis tidak berlaku tanpa aksi manual (DoD PR-2.3.5b) — penegakan lazy saat evaluasi.
func (d *Delegation) AppliesTo(now time.Time) bool {
	if now.Before(d.ValidFrom) {
		return false
	}
	return now.Before(d.ValidUntil)
}
