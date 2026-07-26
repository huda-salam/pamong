package customization

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	coreCust "github.com/huda-salam/pamong/core/customization"
	"github.com/huda-salam/pamong/infra/db"
)

// DBTenantCapabilityStore mengimplementasi coreCust.TenantCapabilityStore di atas Postgres
// (gov.tenant_capability_overrides). Hanya menyimpan override EKSPLISIT — ketiadaan baris berarti
// "pakai DefaultEnabled" (resolver yang menafsirkan). Persistensi ini adalah carry-over PR-3.4.2.
type DBTenantCapabilityStore struct {
	pool *db.Pool
}

// NewDBTenantCapabilityStore membuat store baru. Panggil EnsureSchema (via DBCustomFieldStore
// atau pamongctl migrate) agar tabel tersedia — keduanya berbagi MigrationModule customization.
func NewDBTenantCapabilityStore(pool *db.Pool) *DBTenantCapabilityStore {
	return &DBTenantCapabilityStore{pool: pool}
}

var _ coreCust.TenantCapabilityStore = (*DBTenantCapabilityStore)(nil)

// EnsureSchema menerapkan migrasi komponen customization (berbagi dengan custom field). Idempoten.
func (s *DBTenantCapabilityStore) EnsureSchema(ctx context.Context) error {
	return db.ApplyEmbeddedSchema(ctx, s.pool, coreCust.MigrationModule, coreCust.MigrationsFS)
}

// Override mengembalikan (enabled, ok, err). ok=false bila tenant tak menetapkan override.
func (s *DBTenantCapabilityStore) Override(ctx context.Context, tenantID, capability string) (bool, bool, error) {
	// gov:raw-ok reason=read-capability-override query=tenant-capability-override-get
	row := s.pool.QueryRow(ctx, `
		SELECT enabled FROM gov.tenant_capability_overrides
		WHERE tenant_id = $1 AND capability = $2`,
		tenantID, capability)
	var enabled bool
	if err := row.Scan(&enabled); err != nil {
		if db.IsNoRows(err) {
			return false, false, nil
		}
		return false, false, fmt.Errorf("read capability override: %w", err)
	}
	return enabled, true, nil
}

// Set meng-upsert override per-tenant, mencatat set_by (nil = seed/framework) untuk atribusi.
func (s *DBTenantCapabilityStore) Set(ctx context.Context, tenantID, capability string, enabled bool, setBy *uuid.UUID) error {
	// gov:raw-ok reason=upsert-capability-override query=tenant-capability-override-upsert
	_, err := s.pool.Exec(ctx, `
		INSERT INTO gov.tenant_capability_overrides (tenant_id, capability, enabled, set_by)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (tenant_id, capability) DO UPDATE SET
		    enabled = EXCLUDED.enabled,
		    set_by  = EXCLUDED.set_by,
		    set_at  = now()`,
		tenantID, capability, enabled, setBy)
	if err != nil {
		return fmt.Errorf("upsert capability override: %w", err)
	}
	return nil
}
