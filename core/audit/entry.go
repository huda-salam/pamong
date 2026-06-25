// Package audit menyediakan jejak audit immutable untuk mutasi entity Auditable:
// siapa mengubah apa, kapan, dari nilai apa ke apa. Untuk pemerintahan ini kebutuhan
// hukum (pemeriksaan BPK/BPKP). PR-1.3.1 mengisi writer + field diff; hash chain
// (tamper detection) menyusul di PR-1.3.2, auto-attach ke domain engine di PR-1.3.3.
package audit

import (
	"time"

	"github.com/google/uuid"
)

// Action adalah jenis mutasi yang diaudit.
type Action string

const (
	ActionCreate Action = "create"
	ActionUpdate Action = "update"
	ActionSubmit Action = "submit"
	ActionDelete Action = "delete"
)

// FieldDiff mencatat perubahan satu field (hanya field yang berubah yang dicatat).
type FieldDiff struct {
	Field  string `json:"field"`
	Before any    `json:"before"`
	After  any    `json:"after"`
}

// AuditEntry adalah satu baris jejak audit. Append-only — tidak pernah di-UPDATE/DELETE.
// PrevHash/Hash diisi oleh hash chain di PR-1.3.2; pada PR-1.3.1 keduanya kosong.
type AuditEntry struct {
	ID           uuid.UUID
	TenantID     string
	Entity       string // "penatausahaan.SPM"
	EntityID     uuid.UUID
	Action       Action
	ActorID      uuid.UUID
	ActorIP      string
	Diff         []FieldDiff
	WorkflowFrom string
	WorkflowTo   string
	Timestamp    time.Time
	PrevHash     string
	Hash         string
}
