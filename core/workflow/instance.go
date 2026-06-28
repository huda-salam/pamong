package workflow

import (
	"time"

	"github.com/google/uuid"
)

// WorkflowInstance adalah satu jalannya workflow untuk satu entitas bisnis.
// DefinitionVersion dikunci saat Start — perubahan definisi setelah instance
// dimulai tidak mengubah alur yang sedang berjalan (PRD F1, F7).
//
// PR-3.2.1: instance dikembalikan sebagai nilai ke caller; storage di-handle PR-3.2.3.
// Engine stateless terhadap instance — caller menyimpan dan meneruskan saat Execute.
type WorkflowInstance struct {
	ID                uuid.UUID
	DefinitionID      string
	DefinitionVersion int
	EntityID          uuid.UUID
	CurrentState      string
	StartedAt         time.Time
	History           []TransitionRecord
}

// TransitionRecord adalah entri immutable dalam riwayat instance.
// Setiap transisi sukses menghasilkan satu record — tidak pernah dihapus atau diubah.
type TransitionRecord struct {
	From      string
	To        string
	Action    string // nama use case yang dipanggil, kosong bila tidak ada
	ActorID   uuid.UUID
	Timestamp time.Time
	Comment   string
}
