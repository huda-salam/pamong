package customization

import (
	"context"
	"time"

	"github.com/huda-salam/pamong/core/config"
	"github.com/huda-salam/pamong/core/domain"
	"github.com/huda-salam/pamong/port"
)

// Manager adalah jalur TULIS ber-tata-kelola untuk kustomisasi tenant (custom field, label
// override, capability). Pola sama dengan config.ChoiceManager: manager menegakkan permission +
// invarian + publikasi event; penyimpanan didelegasikan ke store pluggable. Setiap operasi tulis:
//  1. permission check lewat AuthContext (aturan #3/#8) — bukan hardcode string di luar konstanta;
//  2. invarian domain (validasi field, collision, capability terdaftar) — fail-closed;
//  3. publikasi event (bila publisher di-wire) sebagai jejak + sinyal invalidasi cache merge.
//
// TenantID selalu diambil dari AuthContext (token tersigning), tak pernah dari parameter —
// mencegah aktor menulis kustomisasi tenant lain.
//
// DEFERRED(Phase-5.1.1): Manager belum di-construct di produksi (cmd/server/main.go masih stub;
// Router = Phase 5.1.1). Saat wiring, panggil RegisterEventSchemas(bus.Schema()) SEBELUM publisher
// non-nil dipakai — bila tidak, Bus.Publish menolak event dan write gagal di langkah publish.
// Checklist wiring lengkap ada di ROADMAP §Backlog "[Phase-5.1.1] Live wiring customization write-path".
//
// DEFERRED(Phase-5.1.x): store-write dan publish belum atomik — Save/Set lalu Publish adalah dua
// langkah terpisah, jadi kegagalan publish meninggalkan data tertulis sementara caller menerima
// error & cache merge tak ter-invalidasi. Saat di-wire produksi, alihkan ke OUTBOX (PR-3.1.2)
// agar event ikut transaksi tulis. Selama publisher nil (belum di-wire) tak ada dampak.
type Manager struct {
	fields    CustomFieldStore
	labels    config.TenantConfigStore
	caps      TenantCapabilityStore
	capReg    *CapabilityRegistry
	lookup    EntityLookup
	publisher port.EventPublisher // opsional; nil = tanpa publikasi/invalidasi event
	now       func() time.Time
}

// NewManager merakit Manager. publisher boleh nil (bootstrap/test tanpa event bus). lookup wajib
// non-nil untuk custom field (menargetkan entity tak dikenal ditolak fail-closed).
func NewManager(
	fields CustomFieldStore,
	labels config.TenantConfigStore,
	caps TenantCapabilityStore,
	capReg *CapabilityRegistry,
	lookup EntityLookup,
	publisher port.EventPublisher,
) *Manager {
	return &Manager{
		fields:    fields,
		labels:    labels,
		caps:      caps,
		capReg:    capReg,
		lookup:    lookup,
		publisher: publisher,
		now:       time.Now,
	}
}

// CreateCustomField menambah custom field ke entity modul untuk tenant si actor (PRD F1). Menolak
// bila: tak punya permission; entity target tak terdaftar; nama bentrok dengan field inti atau
// custom aktif lain; atau FieldDef tak valid. Class kosong → internal (default aman).
func (m *Manager) CreateCustomField(ctx port.AuthContext, def CustomFieldDef) error {
	if err := ctx.RequirePermission(PermCustomFieldBuat); err != nil {
		return err
	}
	def.TenantID = ctx.TenantID()
	def.CreatedBy = ctx.PersonID()
	if def.CreatedAt.IsZero() {
		def.CreatedAt = m.now()
	}
	def.IsActive = true
	def = def.Normalize()

	if err := def.Validate(); err != nil {
		return err
	}
	// Entity target harus ada; collision dengan field inti ditolak (fail-closed).
	base, ok := m.lookup.Entity(def.Module, def.Entity)
	if !ok {
		return ErrEntityNotFound(def.Module, def.Entity)
	}
	if err := ValidateAgainstBase(base, def); err != nil {
		return err
	}
	// Collision dengan custom field AKTIF lain (bukan reaktivasi nama sama yang sudah nonaktif).
	existing, err := m.fields.List(ctx, def.TenantID, def.Module, def.Entity)
	if err != nil {
		return err
	}
	for _, e := range existing {
		if e.Field.Name == def.Field.Name {
			return ErrCustomFieldExists(def.Field.Name)
		}
	}

	if err := m.fields.Save(ctx, def); err != nil {
		return err
	}
	return m.publish(ctx, EventCustomFieldDitambahkan, FieldChangedPayload{
		TenantID: def.TenantID, Module: def.Module, Entity: def.Entity, FieldName: def.Field.Name,
	})
}

// DeactivateCustomField menonaktifkan custom field (soft): tak lagi di-merge, data lama tetap.
func (m *Manager) DeactivateCustomField(ctx port.AuthContext, module, entity, fieldName string) error {
	if err := ctx.RequirePermission(PermCustomFieldHapus); err != nil {
		return err
	}
	tenantID := ctx.TenantID()
	if err := m.fields.Deactivate(ctx, tenantID, module, entity, fieldName); err != nil {
		return err
	}
	return m.publish(ctx, EventCustomFieldDinonaktifkan, FieldChangedPayload{
		TenantID: tenantID, Module: module, Entity: entity, FieldName: fieldName,
	})
}

// SetLabel menetapkan label override satu field untuk tenant si actor (PRD F2). Disimpan sebagai
// tenant config ber-scope (lihat label.go). Menimpa versi lama lewat append-only config store.
func (m *Manager) SetLabel(ctx port.AuthContext, module, entity, field, label string) error {
	if err := ctx.RequirePermission(PermLabelUbah); err != nil {
		return err
	}
	tenantID := ctx.TenantID()
	actor := ctx.PersonID()
	if err := m.labels.Set(ctx, config.ConfigEntry{
		Scope: config.ConfigScope{TenantID: tenantID},
		Key:   LabelKey(module, entity, field),
		Value: label,
		SetBy: &actor,
	}); err != nil {
		return err
	}
	return m.publish(ctx, EventLabelDiubah, FieldChangedPayload{
		TenantID: tenantID, Module: module, Entity: entity, FieldName: field,
	})
}

// SetCapability meng-override capability flag untuk tenant si actor (PRD F3, carry-over PR-3.4.2:
// persistensi + jalur tulis ber-permission). Capability WAJIB terdaftar (fail-closed) — override
// fitur tak dikenal ditolak, konsisten dengan CapabilityResolver.IsEnabled.
func (m *Manager) SetCapability(ctx port.AuthContext, capability string, enabled bool) error {
	if err := ctx.RequirePermission(PermCapabilityUbah); err != nil {
		return err
	}
	if _, ok := m.capReg.Get(capability); !ok {
		return ErrUnknownCapability(capability)
	}
	tenantID := ctx.TenantID()
	actor := ctx.PersonID()
	if err := m.caps.Set(ctx, tenantID, capability, enabled, &actor); err != nil {
		return err
	}
	return m.publish(ctx, EventCapabilityDiubah, CapabilityChangedPayload{
		TenantID: tenantID, Capability: capability, Enabled: enabled,
	})
}

// EffectiveEntity mengembalikan EntityDef efektif tenant: definisi inti + custom field aktif,
// hasil MergeEntity (PRD F1, "di-merge saat runtime"). Read-only, tanpa permission (dipakai
// generator/form). Entity target harus terdaftar.
func (m *Manager) EffectiveEntity(ctx context.Context, tenantID, module, entity string) (domain.EntityDef, error) {
	base, ok := m.lookup.Entity(module, entity)
	if !ok {
		return domain.EntityDef{}, ErrEntityNotFound(module, entity)
	}
	customs, err := m.fields.List(ctx, tenantID, module, entity)
	if err != nil {
		return domain.EntityDef{}, err
	}
	return MergeEntity(base, customs), nil
}

// publish menerbitkan event bila publisher di-wire; nil publisher = no-op (bootstrap/test).
func (m *Manager) publish(ctx port.AuthContext, name string, payload any) error {
	if m.publisher == nil {
		return nil
	}
	return m.publisher.Publish(ctx, port.Event{
		Name:     name,
		Payload:  payload,
		TenantID: ctx.TenantID(),
		CausedBy: ctx.PersonID().String(),
	})
}
