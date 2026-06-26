// Package sync adalah clone engine identity: meng-subscribe event identity dan menulis
// salinan read-only person+employment ke gov.user_profiles pada DB tenant tujuan. Inilah
// jembatan identity DB sentral → tenant DB (CLAUDE.md "Identity sync engine").
//
// Modul bisnis TIDAK menyentuh package ini; mereka membaca data user lewat UserResolver
// port. Sync engine berdiri di sisi adapter (boleh import infra) — bukan domain/usecase.
package sync

import (
	"context"

	"github.com/google/uuid"
)

// UserProfileClone adalah snapshot person+employment yang ditulis ke gov.user_profiles
// satu tenant. Sumbernya event "fat" (EmploymentDitugaskanPayload), bukan baca-balik
// identity DB — sehingga consumer mandiri dari skema sentral.
type UserProfileClone struct {
	PersonID         uuid.UUID
	AssignmentID     uuid.UUID
	NIK              string
	NIP              string
	NamaLengkap      string
	EmploymentStatus string
	IsCrossTenant    bool
}

// Writer menulis clone ke DB satu tenant. Implementasi (writer_tenantdb.go) memilih pool
// tenant lewat TenantConnManager lalu upsert idempoten (event bisa terkirim ulang).
// Diabstraksikan sebagai port agar Engine bisa diuji tanpa Postgres.
type Writer interface {
	Upsert(ctx context.Context, tenantID string, c UserProfileClone) error
}
