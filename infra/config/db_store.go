// Package config menyediakan driven adapter Postgres untuk config.TenantConfigStore.
// Seluruh kode yang menyentuh pgx HANYA ada di sini dan di infra/db — core/config tidak
// pernah mengimport infra (linter: domain-no-infra-import).
package config

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	coreCfg "github.com/huda-salam/pamong/core/config"
	"github.com/huda-salam/pamong/infra/db"
)

// tenantConfigDDL membuat schema gov & tabel tenant_configs bila belum ada, dalam bentuk
// FINAL ber-versi (PR-3.3.3). Identik dengan migration 001+002 digabung — dipakai EnsureSchema
// untuk bootstrap langsung (pola AuditRepo/DBStore). ALTER idempoten menutup deployment lama
// yang sempat memakai skema non-versi 3.3.2 (pola PR-3.1.4 outbox).
//
// Append-only ber-versi: tiap perubahan pilihan menambah baris (version = max+1 per scope+key)
// dengan effective_from — pilihan lama tetap tersimpan (non-retroaktif, titik ekstensi #7).
// Keunikan memakai UNIQUE NULLS NOT DISTINCT (Postgres 15+) agar scope tenant-level (unit &
// resource NULL) tetap dibandingkan benar per versi.
const tenantConfigDDL = `
CREATE SCHEMA IF NOT EXISTS gov;
CREATE TABLE IF NOT EXISTS gov.tenant_configs (
    tenant_id      TEXT        NOT NULL,
    unit_kerja_id  UUID,
    resource_id    UUID,
    config_key     TEXT        NOT NULL,
    value          TEXT        NOT NULL,
    version        INT         NOT NULL DEFAULT 1,
    effective_from TIMESTAMPTZ NOT NULL DEFAULT now(),
    set_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    set_by         UUID,
    CONSTRAINT ck_tenant_config_scope
        CHECK (resource_id IS NULL OR unit_kerja_id IS NOT NULL)
);
ALTER TABLE gov.tenant_configs ADD COLUMN IF NOT EXISTS version INT NOT NULL DEFAULT 1;
ALTER TABLE gov.tenant_configs ADD COLUMN IF NOT EXISTS effective_from TIMESTAMPTZ NOT NULL DEFAULT now();
ALTER TABLE gov.tenant_configs DROP CONSTRAINT IF EXISTS uq_tenant_config_scope;
ALTER TABLE gov.tenant_configs DROP CONSTRAINT IF EXISTS uq_tenant_config_version;
ALTER TABLE gov.tenant_configs ADD CONSTRAINT uq_tenant_config_version
    UNIQUE NULLS NOT DISTINCT (tenant_id, config_key, unit_kerja_id, resource_id, version);
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

// Candidates mengembalikan semua VERSI entry untuk (tenant, key) lintas level scope.
func (s *DBTenantConfigStore) Candidates(ctx context.Context, tenantID, key string) ([]coreCfg.ConfigEntry, error) {
	// gov:raw-ok reason=scope-candidates query=tenant-config-candidates
	rows, err := s.pool.Query(ctx, `
		SELECT tenant_id, unit_kerja_id, resource_id, config_key, value, version, effective_from, set_by
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
		if err := rows.Scan(&e.Scope.TenantID, &unitID, &resID, &e.Key, &e.Value,
			&e.Version, &e.EffectiveFrom, &setBy); err != nil {
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

// Set MENAMBAH satu versi baru untuk scope+key (append-only). version = max+1 per scope+key,
// dihitung atomik dalam INSERT ... SELECT (scope nullable dicocokkan dgn IS NOT DISTINCT FROM).
// effective_from nol → now().
func (s *DBTenantConfigStore) Set(ctx context.Context, entry coreCfg.ConfigEntry) error {
	if err := coreCfg.ValidateEntry(entry); err != nil {
		return err
	}
	effectiveFrom := entry.EffectiveFrom
	if effectiveFrom.IsZero() {
		effectiveFrom = time.Now()
	}
	// gov:raw-ok reason=append-versioned-config query=tenant-config-append-version
	_, err := s.pool.Exec(ctx, `
		INSERT INTO gov.tenant_configs
		    (tenant_id, unit_kerja_id, resource_id, config_key, value, version, effective_from, set_by)
		SELECT $1, $2, $3, $4, $5,
		       COALESCE(MAX(version), 0) + 1, $6, $7
		FROM gov.tenant_configs
		WHERE tenant_id = $1
		  AND config_key = $4
		  AND unit_kerja_id IS NOT DISTINCT FROM $2
		  AND resource_id   IS NOT DISTINCT FROM $3`,
		entry.Scope.TenantID, entry.Scope.UnitKerjaID, entry.Scope.ResourceID,
		entry.Key, entry.Value, effectiveFrom, entry.SetBy)
	if err != nil {
		return fmt.Errorf("append versi tenant_config: %w", err)
	}
	return nil
}
