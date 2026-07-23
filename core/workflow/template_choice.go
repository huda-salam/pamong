package workflow

import (
	"time"

	"github.com/huda-salam/pamong/port"
)

// TemplateChoiceManager adalah jalur TULIS ber-tata-kelola untuk pilihan template tenant
// (PR-3.3.2b), menutup utang PR-3.2.4 butir (a)/(d) dengan pola yang sama seperti
// core/config.ChoiceManager (PR-3.3.3):
//
//   - Ber-versi & ter-audit: SetChoice menambah versi baru (append-only) yang mencatat SIAPA
//     (SetBy dari AuthContext) dan SEJAK KAPAN (EffectiveFrom). Tabel ber-versi itu sendiri
//     jejaknya — pola workflow_definitions/tenant_configs, bukan audit_logs terpisah.
//   - Validasi template_id saat TULIS: menolak pilihan yang merujuk WorkflowDefinition tak
//     terdaftar, sehingga config tidak bisa menunjuk ID sembarang (error tak lagi tertunda
//     sampai GetForTenant). Jalur seed TemplateStore.SetTenantTemplate SENGAJA tidak divalidasi
//     agar template boleh diseed setelah config — pembatasan ini milik lapisan ber-aktor ini.
//
// Permission check BUKAN tanggung jawab manager ini — itu milik use case admin pemanggil
// (belum ada; permission string ditetapkan saat write path di-wire ke gateway).
type TemplateChoiceManager struct {
	store TemplateStore
	defs  DefinitionStore
	now   func() time.Time
}

// NewTemplateChoiceManager membuat manager di atas store pilihan + store definisi.
func NewTemplateChoiceManager(store TemplateStore, defs DefinitionStore) *TemplateChoiceManager {
	return &TemplateChoiceManager{store: store, defs: defs, now: time.Now}
}

// SetChoice menetapkan pilihan template tenant untuk aktor tertentu, berlaku sejak
// effectiveFrom (nol → sekarang). Memvalidasi template_id terdaftar lalu menambah versi baru
// dengan SetBy = aktor. Pilihan lama tetap terbaca lewat TemplateStore.GetTenantConfigVersions.
func (m *TemplateChoiceManager) SetChoice(ctx port.AuthContext, cfg TenantWorkflowConfig, effectiveFrom time.Time) error {
	if cfg.TenantID == "" || cfg.Slot == "" || cfg.TemplateID == "" {
		return ErrInvalidTemplateConfig("tenant_id, slot, dan template_id wajib diisi")
	}
	// Validasi template_id merujuk definisi yang ADA (cegah slot menunjuk ID sembarang).
	if _, err := m.defs.Get(cfg.TemplateID); err != nil {
		return err
	}
	if effectiveFrom.IsZero() {
		effectiveFrom = m.now()
	}
	actor := ctx.PersonID()
	cfg.EffectiveFrom = effectiveFrom
	cfg.SetAt = m.now()
	cfg.SetBy = &actor
	return m.store.SetTenantTemplate(cfg)
}
