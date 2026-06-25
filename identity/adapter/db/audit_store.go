package db

import (
	"context"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core/audit"
	infradb "github.com/huda-salam/pamong/infra/db"
)

// identityChainPartition adalah nilai partisi chain untuk audit identity. Karena operasi
// identity sentral (tak ada tenant), seluruh entry dirantai jadi satu chain lewat
// sentinel ini di kolom tenant_id (ADR-003).
const identityChainPartition = "central"

// AuditStore adalah audit.Store untuk mutasi identity, menulis ke id.audit_logs (identity
// DB sentral). Reuse penuh engine & hash chain via infra/db.AuditRepo schema "id".
type AuditStore struct {
	inner *infradb.AuditRepo
}

var _ audit.Store = (*AuditStore)(nil)

func NewAuditStore(pool *infradb.Pool) *AuditStore {
	return &AuditStore{inner: infradb.NewSchemaAuditRepo(pool, "id")}
}

// EnsureSchema membuat id.audit_logs bila belum ada.
func (s *AuditStore) EnsureSchema(ctx context.Context) error { return s.inner.EnsureSchema(ctx) }

// Append menyetel partisi chain sentral lalu mendelegasikan ke engine.
func (s *AuditStore) Append(ctx context.Context, e audit.AuditEntry) error {
	e.TenantID = identityChainPartition
	return s.inner.Append(ctx, e)
}

// ByEntity mengembalikan riwayat audit satu entity identity.
func (s *AuditStore) ByEntity(ctx context.Context, entity string, id uuid.UUID) ([]audit.AuditEntry, error) {
	return s.inner.ByEntity(ctx, entity, id)
}

// Chain mengembalikan seluruh entry audit identity terurut, untuk verifikasi integritas.
func (s *AuditStore) Chain(ctx context.Context) ([]audit.AuditEntry, error) {
	return s.inner.ByTenant(ctx, identityChainPartition)
}
