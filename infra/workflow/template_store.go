package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	coreWf "github.com/huda-salam/pamong/core/workflow"
	"github.com/huda-salam/pamong/infra/db"
)

// templateConfigDDL membuat tabel gov.tenant_workflow_configs dalam bentuk FINAL ber-versi
// (PR-3.3.2b). Menggabungkan migration 002 + 003 — dipakai EnsureSchema untuk bootstrap
// langsung (pola AuditRepo/DBStore). ALTER idempoten menutup deployment lama yang sempat
// memakai skema non-versi PK (tenant_id, slot) (pola PR-3.1.4 outbox).
//
// Append-only ber-versi: tiap perubahan pilihan menambah baris (version = max+1 per
// tenant+slot) dengan effective_from — pilihan lama tetap terbaca untuk audit/rollback.
const templateConfigDDL = `
CREATE SCHEMA IF NOT EXISTS gov;
CREATE TABLE IF NOT EXISTS gov.tenant_workflow_configs (
    tenant_id      TEXT        NOT NULL,
    slot           TEXT        NOT NULL,
    template_id    TEXT        NOT NULL,
    role_bindings  JSONB       NOT NULL DEFAULT '{}',
    version        INT         NOT NULL DEFAULT 1,
    effective_from TIMESTAMPTZ NOT NULL DEFAULT now(),
    set_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    set_by         UUID
);
ALTER TABLE gov.tenant_workflow_configs ADD COLUMN IF NOT EXISTS version INT NOT NULL DEFAULT 1;
ALTER TABLE gov.tenant_workflow_configs ADD COLUMN IF NOT EXISTS effective_from TIMESTAMPTZ NOT NULL DEFAULT now();
ALTER TABLE gov.tenant_workflow_configs DROP CONSTRAINT IF EXISTS tenant_workflow_configs_pkey;
ALTER TABLE gov.tenant_workflow_configs DROP CONSTRAINT IF EXISTS uq_twc_version;
ALTER TABLE gov.tenant_workflow_configs ADD CONSTRAINT uq_twc_version
    UNIQUE (tenant_id, slot, version);
CREATE INDEX IF NOT EXISTS idx_twc_lookup
    ON gov.tenant_workflow_configs (tenant_id, slot);`

// DBTemplateStore mengimplementasi coreWf.TemplateStore di atas Postgres.
// UPSERT pada (tenant_id, slot) agar SetTenantTemplate idempoten — panggilan
// berulang untuk slot yang sama menimpa pilihan sebelumnya.
//
// SetTenantTemplateAsActor adalah varian yang mencatat aktor pada kolom set_by;
// SetTenantTemplate untuk seed/framework menyimpan set_by = NULL.
//
// PERHATIAN: set_by BUKAN audit trail. UPSERT menimpa baris, jadi pilihan template
// sebelumnya hilang — tidak ada riwayat, tidak ada entri gov.audit_logs, tidak ada
// effective_from untuk rollback. Audit & versioning penuh menyusul di PR-3.3.2
// (lihat ROADMAP "[PR-3.3.2] Rekonsiliasi penyimpanan template selection").
type DBTemplateStore struct {
	pool *db.Pool
	defs coreWf.DefinitionStore
}

// NewDBTemplateStore membuat store baru. Panggil EnsureSchema sebelum dipakai.
func NewDBTemplateStore(pool *db.Pool, defs coreWf.DefinitionStore) *DBTemplateStore {
	return &DBTemplateStore{pool: pool, defs: defs}
}

var _ coreWf.TemplateStore = (*DBTemplateStore)(nil)

// EnsureSchema membuat schema gov & tabel tenant_workflow_configs bila belum ada.
func (s *DBTemplateStore) EnsureSchema(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, templateConfigDDL)
	return err
}

// SetTenantTemplate MENAMBAH satu versi pilihan template (append-only). set_by diambil
// dari cfg.SetBy (NULL bila nil — mis. seed/framework), konsisten dengan MemoryTemplateStore.
func (s *DBTemplateStore) SetTenantTemplate(cfg coreWf.TenantWorkflowConfig) error {
	return s.appendVersion(context.Background(), cfg)
}

// SetTenantTemplateAsActor menambah versi pilihan sekaligus mencatat aktor (admin yang
// melakukan perubahan), menimpa cfg.SetBy. Dipakai use case admin, bukan seed.
func (s *DBTemplateStore) SetTenantTemplateAsActor(ctx context.Context, cfg coreWf.TenantWorkflowConfig, actorID uuid.UUID) error {
	cfg.SetBy = &actorID
	return s.appendVersion(ctx, cfg)
}

// appendVersion menyisipkan versi baru; version = max+1 per (tenant, slot), dihitung atomik
// dalam INSERT ... SELECT. effective_from nol → set_at (default sekarang).
func (s *DBTemplateStore) appendVersion(ctx context.Context, cfg coreWf.TenantWorkflowConfig) error {
	if cfg.TenantID == "" || cfg.Slot == "" || cfg.TemplateID == "" {
		return coreWf.ErrInvalidTemplateConfig("tenant_id, slot, dan template_id wajib diisi")
	}

	bindingsJSON, err := json.Marshal(cfg.RoleBindings)
	if err != nil {
		return fmt.Errorf("serialisasi role_bindings: %w", err)
	}

	setAt := cfg.SetAt
	if setAt.IsZero() {
		setAt = time.Now()
	}
	effectiveFrom := cfg.EffectiveFrom
	if effectiveFrom.IsZero() {
		effectiveFrom = setAt
	}

	// gov:raw-ok reason=append-versioned-template query=tenant-workflow-config-append-version
	_, err = s.pool.Exec(ctx, `
		INSERT INTO gov.tenant_workflow_configs
		    (tenant_id, slot, template_id, role_bindings, version, effective_from, set_at, set_by)
		SELECT $1, $2, $3, $4::jsonb,
		       COALESCE(MAX(version), 0) + 1, $5, $6, $7
		FROM gov.tenant_workflow_configs
		WHERE tenant_id = $1 AND slot = $2`,
		cfg.TenantID, cfg.Slot, cfg.TemplateID,
		bindingsJSON, effectiveFrom, setAt, cfg.SetBy,
	)
	return err
}

// GetTenantConfig mengembalikan versi TERBARU config untuk pasangan tenant+slot.
// ErrTemplateNotConfigured bila belum ada.
func (s *DBTemplateStore) GetTenantConfig(tenantID, slot string) (coreWf.TenantWorkflowConfig, error) {
	// gov:raw-ok reason=select-latest-version query=tenant-workflow-config-get-latest
	var (
		cfg          coreWf.TenantWorkflowConfig
		bindingsJSON []byte
	)
	err := s.pool.QueryRow(context.Background(), `
		SELECT tenant_id, slot, template_id, role_bindings, version, effective_from, set_at, set_by
		FROM gov.tenant_workflow_configs
		WHERE tenant_id = $1 AND slot = $2
		ORDER BY version DESC LIMIT 1`,
		tenantID, slot,
	).Scan(
		&cfg.TenantID, &cfg.Slot, &cfg.TemplateID,
		&bindingsJSON, &cfg.Version, &cfg.EffectiveFrom, &cfg.SetAt, &cfg.SetBy,
	)
	if db.IsNoRows(err) {
		return coreWf.TenantWorkflowConfig{}, coreWf.ErrTemplateNotConfigured(tenantID, slot)
	}
	if err != nil {
		return coreWf.TenantWorkflowConfig{}, fmt.Errorf("baca tenant_workflow_config: %w", err)
	}
	if err := json.Unmarshal(bindingsJSON, &cfg.RoleBindings); err != nil {
		return coreWf.TenantWorkflowConfig{}, fmt.Errorf("deserialisasi role_bindings: %w", err)
	}
	return cfg, nil
}

// GetTenantConfigVersions mengembalikan SELURUH versi pilihan (terurut naik) untuk
// riwayat/rollback/audit. Slice kosong (bukan error) bila belum ada pilihan.
func (s *DBTemplateStore) GetTenantConfigVersions(tenantID, slot string) ([]coreWf.TenantWorkflowConfig, error) {
	// gov:raw-ok reason=select-all-versions query=tenant-workflow-config-versions
	rows, err := s.pool.Query(context.Background(), `
		SELECT tenant_id, slot, template_id, role_bindings, version, effective_from, set_at, set_by
		FROM gov.tenant_workflow_configs
		WHERE tenant_id = $1 AND slot = $2
		ORDER BY version ASC`,
		tenantID, slot)
	if err != nil {
		return nil, fmt.Errorf("query versi tenant_workflow_config: %w", err)
	}
	defer rows.Close()

	var out []coreWf.TenantWorkflowConfig
	for rows.Next() {
		var (
			cfg          coreWf.TenantWorkflowConfig
			bindingsJSON []byte
		)
		if err := rows.Scan(&cfg.TenantID, &cfg.Slot, &cfg.TemplateID,
			&bindingsJSON, &cfg.Version, &cfg.EffectiveFrom, &cfg.SetAt, &cfg.SetBy); err != nil {
			return nil, fmt.Errorf("scan versi tenant_workflow_config: %w", err)
		}
		if err := json.Unmarshal(bindingsJSON, &cfg.RoleBindings); err != nil {
			return nil, fmt.Errorf("deserialisasi role_bindings: %w", err)
		}
		out = append(out, cfg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterasi versi tenant_workflow_config: %w", err)
	}
	return out, nil
}

// GetForTenant mengembalikan WorkflowDefinition yang dipilih tenant dengan role
// binding sudah diterapkan. ErrTemplateNotConfigured bila belum ada pilihan.
func (s *DBTemplateStore) GetForTenant(tenantID, slot string) (coreWf.WorkflowDefinition, error) {
	cfg, err := s.GetTenantConfig(tenantID, slot)
	if err != nil {
		return coreWf.WorkflowDefinition{}, err
	}
	def, err := s.defs.Get(cfg.TemplateID)
	if err != nil {
		return coreWf.WorkflowDefinition{}, err
	}
	return coreWf.ApplyBindings(def, cfg.RoleBindings), nil
}
