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

// templateConfigDDL membuat tabel gov.tenant_workflow_configs bila belum ada.
// Identik dengan migration 002_create_tenant_workflow_configs.up.sql — dipakai
// EnsureSchema untuk bootstrap langsung (pola AuditRepo/DBStore).
const templateConfigDDL = `
CREATE SCHEMA IF NOT EXISTS gov;
CREATE TABLE IF NOT EXISTS gov.tenant_workflow_configs (
    tenant_id     TEXT        NOT NULL,
    slot          TEXT        NOT NULL,
    template_id   TEXT        NOT NULL,
    role_bindings JSONB       NOT NULL DEFAULT '{}',
    set_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    set_by        UUID,
    PRIMARY KEY (tenant_id, slot)
);`

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

// SetTenantTemplate menyimpan atau mengganti pilihan template tenant. set_by diambil
// dari cfg.SetBy (NULL bila nil — mis. seed/framework), konsisten dengan MemoryTemplateStore.
func (s *DBTemplateStore) SetTenantTemplate(cfg coreWf.TenantWorkflowConfig) error {
	return s.upsert(context.Background(), cfg)
}

// SetTenantTemplateAsActor menyimpan pilihan template sekaligus mencatat aktor
// (admin yang melakukan perubahan), menimpa cfg.SetBy. Dipakai use case admin, bukan seed.
func (s *DBTemplateStore) SetTenantTemplateAsActor(ctx context.Context, cfg coreWf.TenantWorkflowConfig, actorID uuid.UUID) error {
	cfg.SetBy = &actorID
	return s.upsert(ctx, cfg)
}

func (s *DBTemplateStore) upsert(ctx context.Context, cfg coreWf.TenantWorkflowConfig) error {
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

	// gov:raw-ok reason=upsert-tenant-template query=tenant-workflow-config-upsert
	_, err = s.pool.Exec(ctx, `
		INSERT INTO gov.tenant_workflow_configs
		    (tenant_id, slot, template_id, role_bindings, set_at, set_by)
		VALUES ($1, $2, $3, $4::jsonb, $5, $6)
		ON CONFLICT (tenant_id, slot) DO UPDATE SET
		    template_id   = EXCLUDED.template_id,
		    role_bindings = EXCLUDED.role_bindings,
		    set_at        = EXCLUDED.set_at,
		    set_by        = EXCLUDED.set_by`,
		cfg.TenantID, cfg.Slot, cfg.TemplateID,
		bindingsJSON, setAt, cfg.SetBy,
	)
	return err
}

// GetTenantConfig mengembalikan config tersimpan untuk pasangan tenant+slot.
// ErrTemplateNotConfigured bila belum ada.
func (s *DBTemplateStore) GetTenantConfig(tenantID, slot string) (coreWf.TenantWorkflowConfig, error) {
	// gov:raw-ok reason=select-by-tenant-slot query=tenant-workflow-config-get
	var (
		cfg          coreWf.TenantWorkflowConfig
		bindingsJSON []byte
	)
	err := s.pool.QueryRow(context.Background(), `
		SELECT tenant_id, slot, template_id, role_bindings, set_at, set_by
		FROM gov.tenant_workflow_configs
		WHERE tenant_id = $1 AND slot = $2`,
		tenantID, slot,
	).Scan(
		&cfg.TenantID, &cfg.Slot, &cfg.TemplateID,
		&bindingsJSON, &cfg.SetAt, &cfg.SetBy,
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
