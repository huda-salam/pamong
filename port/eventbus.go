package port

import "context"

type Event struct {
	Name           string
	Payload        any
	TenantID       string
	CausedBy       string
	IdempotencyKey string
}

type EventPublisher interface {
	Publish(ctx context.Context, event Event) error
}

type EventSubscriber interface {
	Subscribe(event string, handler EventHandler) error
}

type EventHandler func(ctx context.Context, event Event) error
