// Package config menyediakan driven adapter Postgres untuk config.TenantConfigStore.
// Seluruh kode yang menyentuh pgx HANYA ada di sini dan di infra/db — core/config tidak
// pernah mengimport infra (linter: domain-no-infra-import).
package config

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	coreCfg "github.com/huda-salam/pamong/core/config"
	"github.com/huda-salam/pamong/infra/db"
)

// tenantConfigDDL membuat schema gov & tabel tenant_configs bila belum ada. Identik dengan
// migration 001_create_tenant_configs.up.sql — dipakai EnsureSchema untuk bootstrap langsung
// (pola AuditRepo/DBStore).
//
// Keunikan scope memakai UNIQUE NULLS NOT DISTINCT (Postgres 15+) agar dua baris ber-scope
// tenant-level (unit & resource NULL) untuk key yang sama tetap dianggap konflik — tanpa ini,
// NULL != NULL membuat upsert menggandakan baris.
const tenantConfigDDL = `
CREATE SCHEMA IF NOT EXISTS gov;
CREATE TABLE IF NOT EXISTS gov.tenant_configs (
    tenant_id     TEXT        NOT NULL,
    unit_kerja_id UUID,
    resource_id   UUID,
    config_key    TEXT        NOT NULL,
    value         TEXT        NOT NULL,
    set_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    set_by        UUID,
    CONSTRAINT ck_tenant_config_scope
        CHECK (resource_id IS NULL OR unit_kerja_id IS NOT NULL),
    CONSTRAINT uq_tenant_config_scope
        UNIQUE NULLS NOT DISTINCT (tenant_id, config_key, unit_kerja_id, resource_id)
);
CREATE INDEX IF NOT EXISTS idx_tenant_config_lookup
    ON gov.tenant_configs (tenant_id, config_key);`

// DBTenantConfigStore mengimplementasi coreCfg.TenantConfigStore di atas Postgres.
// Resolusi "paling spesifik menang" tetap di core/config.Resolver — store ini hanya
// mengambil seluruh kandidat untuk (tenant, key) dan meng-upsert per scope.
type DBTenantConfigStore struct {
	pool *db.Pool
}

// NewDBTenantConfigStore membuat store baru. Panggil EnsureSchema sebelum dipakai.
func NewDBTenantConfigStore(pool *db.Pool) *DBTenantConfigStore {
	return &DBTenantConfigStore{pool: pool}
}

var _ coreCfg.TenantConfigStore = (*DBTenantConfigStore)(nil)

// EnsureSchema membuat schema gov & tabel tenant_configs bila belum ada. Idempoten.
func (s *DBTenantConfigStore) EnsureSchema(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, tenantConfigDDL)
	return err
}

// Candidates mengembalikan semua entry untuk (tenant, key) lintas level scope.
func (s *DBTenantConfigStore) Candidates(ctx context.Context, tenantID, key string) ([]coreCfg.ConfigEntry, error) {
	// gov:raw-ok reason=scope-candidates query=tenant-config-candidates
	rows, err := s.pool.Query(ctx, `
		SELECT tenant_id, unit_kerja_id, resource_id, config_key, value, set_by
		FROM gov.tenant_configs
		WHERE tenant_id = $1 AND config_key = $2`,
		tenantID, key)
	if err != nil {
		return nil, fmt.Errorf("query tenant_configs: %w", err)
	}
	defer rows.Close()

	var out []coreCfg.ConfigEntry
	for rows.Next() {
		var (
			e      coreCfg.ConfigEntry
			unitID *uuid.UUID
			resID  *uuid.UUID
			setBy  *uuid.UUID
		)
		if err := rows.Scan(&e.Scope.TenantID, &unitID, &resID, &e.Key, &e.Value, &setBy); err != nil {
			return nil, fmt.Errorf("scan tenant_config: %w", err)
		}
		e.Scope.UnitKerjaID = unitID
		e.Scope.ResourceID = resID
		e.SetBy = setBy
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterasi tenant_configs: %w", err)
	}
	return out, nil
}

// Set menyimpan (upsert) satu entry pada scope persisnya. Konflik ditentukan oleh
// uq_tenant_config_scope (tenant, key, unit, resource) — NULLS NOT DISTINCT.
func (s *DBTenantConfigStore) Set(ctx context.Context, entry coreCfg.ConfigEntry) error {
	if err := coreCfg.ValidateEntry(entry); err != nil {
		return err
	}
	// gov:raw-ok reason=upsert-scoped-config query=tenant-config-upsert
	_, err := s.pool.Exec(ctx, `
		INSERT INTO gov.tenant_configs
		    (tenant_id, unit_kerja_id, resource_id, config_key, value, set_by)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT ON CONSTRAINT uq_tenant_config_scope DO UPDATE SET
		    value  = EXCLUDED.value,
		    set_by = EXCLUDED.set_by,
		    set_at = now()`,
		entry.Scope.TenantID, entry.Scope.UnitKerjaID, entry.Scope.ResourceID,
		entry.Key, entry.Value, entry.SetBy)
	if err != nil {
		return fmt.Errorf("upsert tenant_config: %w", err)
	}
	return nil
}
