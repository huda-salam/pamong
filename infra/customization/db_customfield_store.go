// Package customization menyediakan driven adapter Postgres untuk store kustomisasi tenant
// (core/customization). Seluruh kode yang menyentuh pgx HANYA ada di lapis infra ini — core
// tidak pernah mengimport infra (linter: domain-no-infra-import).
package customization

import (
	"context"
	"encoding/json"
	"fmt"

	coreCust "github.com/huda-salam/pamong/core/customization"
	"github.com/huda-salam/pamong/core/domain"
	"github.com/huda-salam/pamong/infra/db"
)

// DBCustomFieldStore mengimplementasi coreCust.CustomFieldStore di atas Postgres
// (gov.tenant_custom_fields). FieldDef disimpan sebagai JSONB; kolom pencarian eksplisit.
type DBCustomFieldStore struct {
	pool *db.Pool
}

// NewDBCustomFieldStore membuat store baru. Panggil EnsureSchema sebelum dipakai.
func NewDBCustomFieldStore(pool *db.Pool) *DBCustomFieldStore {
	return &DBCustomFieldStore{pool: pool}
}

var _ coreCust.CustomFieldStore = (*DBCustomFieldStore)(nil)

// EnsureSchema membuat schema gov + tabel kustomisasi bila belum ada, dari SQL migrasi ter-embed
// (sumber tunggal) di bawah advisory lock. Idempoten. Jalur produksi otoritatif = `pamongctl
// migrate`; ini untuk bootstrap dev/test.
func (s *DBCustomFieldStore) EnsureSchema(ctx context.Context) error {
	return db.ApplyEmbeddedSchema(ctx, s.pool, coreCust.MigrationModule, coreCust.MigrationsFS)
}

// List mengembalikan custom field AKTIF untuk (tenant, module, entity), terurut nama.
func (s *DBCustomFieldStore) List(ctx context.Context, tenantID, module, entity string) ([]coreCust.CustomFieldDef, error) {
	// gov:raw-ok reason=list-custom-fields query=tenant-custom-fields-list
	rows, err := s.pool.Query(ctx, `
		SELECT field_def, data_class, insert_after, created_by, created_at
		FROM gov.tenant_custom_fields
		WHERE tenant_id = $1 AND module = $2 AND entity = $3 AND is_active
		ORDER BY field_name`,
		tenantID, module, entity)
	if err != nil {
		return nil, fmt.Errorf("query tenant_custom_fields: %w", err)
	}
	defer rows.Close()

	var out []coreCust.CustomFieldDef
	for rows.Next() {
		var (
			def     coreCust.CustomFieldDef
			fieldJS []byte
		)
		if err := rows.Scan(&fieldJS, &def.Class, &def.InsertAfter, &def.CreatedBy, &def.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan tenant_custom_field: %w", err)
		}
		var fd domain.FieldDef
		if err := json.Unmarshal(fieldJS, &fd); err != nil {
			return nil, fmt.Errorf("unmarshal field_def: %w", err)
		}
		def.Field = fd
		def.TenantID = tenantID
		def.Module = module
		def.Entity = entity
		def.IsActive = true
		out = append(out, def)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterasi tenant_custom_fields: %w", err)
	}
	return out, nil
}

// Save meng-upsert satu custom field berdasarkan (tenant, module, entity, field_name).
func (s *DBCustomFieldStore) Save(ctx context.Context, def coreCust.CustomFieldDef) error {
	def = def.Normalize()
	if err := def.Validate(); err != nil {
		return err
	}
	fieldJS, err := json.Marshal(def.Field)
	if err != nil {
		return fmt.Errorf("marshal field_def: %w", err)
	}
	// gov:raw-ok reason=upsert-custom-field query=tenant-custom-field-upsert
	_, err = s.pool.Exec(ctx, `
		INSERT INTO gov.tenant_custom_fields
		    (tenant_id, module, entity, field_name, field_def, data_class, insert_after, is_active, created_by, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (tenant_id, module, entity, field_name) DO UPDATE SET
		    field_def    = EXCLUDED.field_def,
		    data_class   = EXCLUDED.data_class,
		    insert_after = EXCLUDED.insert_after,
		    is_active    = EXCLUDED.is_active,
		    created_by   = EXCLUDED.created_by,
		    created_at   = EXCLUDED.created_at`,
		def.TenantID, def.Module, def.Entity, def.Field.Name, fieldJS,
		string(def.Class), def.InsertAfter, def.IsActive, def.CreatedBy, def.CreatedAt)
	if err != nil {
		return fmt.Errorf("upsert tenant_custom_field: %w", err)
	}
	return nil
}

// Deactivate menandai custom field non-aktif. Tak ada baris terpengaruh → ErrCustomFieldNotFound.
func (s *DBCustomFieldStore) Deactivate(ctx context.Context, tenantID, module, entity, fieldName string) error {
	// gov:raw-ok reason=deactivate-custom-field query=tenant-custom-field-deactivate
	tag, err := s.pool.Exec(ctx, `
		UPDATE gov.tenant_custom_fields SET is_active = false
		WHERE tenant_id = $1 AND module = $2 AND entity = $3 AND field_name = $4 AND is_active`,
		tenantID, module, entity, fieldName)
	if err != nil {
		return fmt.Errorf("deactivate tenant_custom_field: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return coreCust.ErrCustomFieldNotFound(fieldName)
	}
	return nil
}
