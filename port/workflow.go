package port

import (
	"context"
	"github.com/google/uuid"
)

type WorkflowPort interface {
	StartInstance(ctx context.Context, workflowID string, entityID uuid.UUID) error
	ExecuteTransition(ctx context.Context, instanceID uuid.UUID, action string) error
	CurrentState(ctx context.Context, instanceID uuid.UUID) (string, error)
	History(ctx context.Context, instanceID uuid.UUID) ([]TransitionRecord, error)
}

type TransitionRecord struct {
	From      string
	To        string
	Action    string
	ActorID   uuid.UUID
	Timestamp int64
	Comment   string
}

// WorkflowActionInput adalah argumen satu pemanggilan action workflow (ADR-022).
// Ia sengaja tidak bertipe modul: engine tak boleh tahu bentuk input use case manapun.
// Pemetaan Params → input bertipe adalah pekerjaan adapter action milik modul.
//
// Params berasal dari REQUEST transisi (niat aktor saat ini), terpisah dari snapshot entity
// yang dipakai guard (keadaan yang sudah ada). Pemisahan itu disengaja — lihat ADR-022
// Keputusan 2: guard tidak boleh dievaluasi terhadap nilai yang dikirim aktor sendiri.
type WorkflowActionInput struct {
	TenantID   string
	InstanceID uuid.UUID
	EntityID   uuid.UUID // entitas bisnis yang dikelola instance (mis. id surat)
	Action     string    // nama action sebagaimana tertulis di definisi workflow
	Params     map[string]any
}

// WorkflowAction adalah kontrak yang dipenuhi modul agar use case-nya bisa dipanggil workflow
// engine tanpa core mengimpor modul (ADR-022 Keputusan 1). Didaftarkan saat Bootstrap lewat
// domain.App.Workflow().RegisterAction(nama, action).
//
// Implementasi WAJIB berupa adapter tipis yang memanggil use case — tidak ada business logic,
// perhitungan, atau akses DB di sini (linter: workflow-action-no-logic, CLAUDE.md #7).
// Error dikembalikan apa adanya: action gagal ⇒ engine membatalkan transisi (state tak berubah).
type WorkflowAction interface {
	RunWorkflowAction(ctx AuthContext, in WorkflowActionInput) error
}
