package domain

import "github.com/google/uuid"

// Event identitas mengikuti format {modul}.{entity}.{kejadian_past_tense} (CLAUDE.md).
// Use case menerbitkannya lewat port.EventPublisher; sync engine (identity/sync)
// mengonsumsinya untuk meng-clone data ke gov.user_profiles tenant. Nama event wajib
// berupa konstanta — tidak ada string literal di publisher (linter event-must-use-const).
//
// Payload dirancang "fat" (self-contained): membawa seluruh kolom yang dibutuhkan
// consumer sehingga sync engine tidak perlu membaca-balik identity DB. Perubahan bentuk
// payload = versi schema baru, bukan penimpaan diam-diam (SchemaRegistry menolaknya).
const (
	EventPersonDibuat         = "identity.person.dibuat"
	EventEmploymentDibuat     = "identity.employment.dibuat"
	EventEmploymentDitugaskan = "identity.employment.ditugaskan"
)

// PersonDibuatPayload menyertai EventPersonDibuat — master person baru di identity DB.
type PersonDibuatPayload struct {
	PersonID    uuid.UUID
	NIK         string
	NamaLengkap string
}

// EmploymentDibuatPayload menyertai EventEmploymentDibuat — person ini kini punya
// kepegawaian (belum tentu tertugaskan ke tenant manapun).
type EmploymentDibuatPayload struct {
	EmploymentID uuid.UUID
	PersonID     uuid.UUID
	Status       string
	NIP          string
}

// EmploymentDitugaskanPayload menyertai EventEmploymentDitugaskan — pemicu clone person
// ke gov.user_profiles tenant tujuan. Membawa seluruh kolom clone agar consumer mandiri.
type EmploymentDitugaskanPayload struct {
	AssignmentID     uuid.UUID
	EmploymentID     uuid.UUID
	PersonID         uuid.UUID
	TenantID         string
	NIK              string
	NIP              string
	NamaLengkap      string
	EmploymentStatus string
	IsCrossTenant    bool
}
