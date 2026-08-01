package db

import (
	"context"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core/audit"
	"github.com/huda-salam/pamong/infra/crypto"
	infradb "github.com/huda-salam/pamong/infra/db"
)

// identityChainPartition adalah nilai partisi chain untuk audit identity. Karena operasi
// identity sentral (tak ada tenant), seluruh entry dirantai jadi satu chain lewat
// sentinel ini di kolom tenant_id (ADR-003).
//
// Nilainya = realm kunci sentral (ADR-017), bukan sekadar string yang mirip. Menyatukannya
// bukan kerapian: kolom tenant_id pada entry audit identity adalah koordinat yang SAMA yang
// dipakai core/audit.Reader untuk membuka nilai diff terenkripsi (RowRef.TenantID dari
// e.TenantID). Dua nilai berbeda di dua tempat = nilai tersegel yang tak bisa dibuka lagi
// oleh jalur bacanya sendiri.
//
// Ia juga menutup cacat laten sentinel lama `"central"`: string itu LOLOS
// identity/domain.tenantIDRe, sehingga pemda yang kebetulan didaftarkan dengan tenant_id
// itu akan melebur audit-nya ke chain sentral — dan, sejak PR-3.8.6, berbagi ruang kunci.
// `_central` gagal `^[a-z]` sehingga mustahil menjadi tenant_id.
const identityChainPartition = crypto.RealmCentral

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
