package audit

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/port"
)

// Store adalah driven port persistensi audit. Didefinisikan di sini (domain),
// diimplementasi di infra/db. Append-only: tidak ada update/delete.
type Store interface {
	Append(ctx context.Context, e AuditEntry) error
}

// Engine merangkai diff + metadata actor menjadi AuditEntry lalu menyimpannya.
// Modul tidak memanggil Engine langsung pada PR-1.3.3 — framework yang meng-attach.
type Engine struct {
	store Store
	now   func() time.Time
	newID func() uuid.UUID
}

// NewEngine membuat Engine dengan Store yang diberikan.
func NewEngine(store Store) *Engine {
	return &Engine{store: store, now: time.Now, newID: uuid.New}
}

// RecordInput adalah masukan satu pencatatan audit. Before/After adalah snapshot
// field entity (nil untuk create/delete sesuai sisi yang tidak ada).
type RecordInput struct {
	Entity       string
	EntityID     uuid.UUID
	Action       Action
	Before       map[string]any
	After        map[string]any
	WorkflowFrom string
	WorkflowTo   string
}

// Record menghitung diff, menyusun AuditEntry dari AuthContext (actor + tenant),
// lalu menyimpannya. Update tanpa perubahan field tidak menghasilkan entry.
func (e *Engine) Record(ctx port.AuthContext, in RecordInput) error {
	diff := Diff(in.Before, in.After)
	if len(diff) == 0 && in.Action == ActionUpdate {
		return nil // no-op update: tidak ada yang berubah, tidak perlu jejak
	}

	entry := AuditEntry{
		ID:           e.newID(),
		TenantID:     ctx.TenantID(),
		Entity:       in.Entity,
		EntityID:     in.EntityID,
		Action:       in.Action,
		ActorID:      ctx.PersonID(),
		Diff:         diff,
		WorkflowFrom: in.WorkflowFrom,
		WorkflowTo:   in.WorkflowTo,
		Timestamp:    e.now(),
	}
	return e.store.Append(ctx, entry)
}
