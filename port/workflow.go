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
