package workflow

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/huda-salam/pamong/port"
)

// RoleChecker adalah seam validasi (PR-N2 bagian C): memastikan setiap NILAI RoleBindings
// yang ditulis lewat SetChoice benar-benar merujuk role tenant yang terdaftar (gov.tenant_roles
// milik tenant tsb), bukan salah ketik atau role tenant lain. Diimplementasikan di luar core
// (adapter atas tenantrole/, mis. infra/workflow.TenantRoleChecker) — core/workflow tidak
// pernah mengimport tenantrole secara langsung.
type RoleChecker interface {
	RoleExists(ctx context.Context, tenantID, roleName string) (bool, error)
}

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
// Permission ditegakkan lewat PermTemplatePilih (PR-3.3.2b butir c) — actor tanpa permission
// ditolak sebelum validasi apapun. TenantID selalu dipaksa dari AuthContext (token tersigning),
// tak pernah dari parameter — pola sama dengan customization.Manager, mencegah actor menulis
// pilihan template tenant lain.
type TemplateChoiceManager struct {
	store TemplateStore
	defs  DefinitionStore
	roles RoleChecker
	now   func() time.Time
}

// NewTemplateChoiceManager membuat manager di atas store pilihan + store definisi + RoleChecker
// (PR-N2 bagian C). roles wajib non-nil di produksi — SetChoice memakainya untuk menolak
// RoleBindings yang menunjuk role tak dikenal tenant SEBELUM notifikasi hidup mengirim ke peran
// salah. Test yang tidak menguji RoleBindings boleh memasang RoleChecker yang selalu true.
func NewTemplateChoiceManager(store TemplateStore, defs DefinitionStore, roles RoleChecker) *TemplateChoiceManager {
	return &TemplateChoiceManager{store: store, defs: defs, roles: roles, now: time.Now}
}

// SetChoice menetapkan pilihan template tenant untuk aktor tertentu, berlaku sejak
// effectiveFrom (nol → sekarang). Menegakkan PermTemplatePilih, memaksa TenantID dari
// AuthContext, memvalidasi template_id terdaftar DAN milik slot yang dituju, lalu menambah
// versi baru dengan SetBy = aktor. Pilihan lama tetap terbaca lewat GetTenantConfigVersions.
func (m *TemplateChoiceManager) SetChoice(ctx port.AuthContext, cfg TenantWorkflowConfig, effectiveFrom time.Time) error {
	if err := ctx.RequirePermission(PermTemplatePilih); err != nil {
		return err
	}
	cfg.TenantID = ctx.TenantID() // paksa dari token, jangan dari input (cegah tulis tenant lain)
	if cfg.TenantID == "" || cfg.Slot == "" || cfg.TemplateID == "" {
		return ErrInvalidTemplateConfig("tenant_id, slot, dan template_id wajib diisi")
	}
	// Cegah slot diarahkan ke definisi milik slot/modul lain — relasi slot↔template lewat
	// konvensi penamaan template key "{slot}.{varian}", varian SATU segmen (tanpa titik
	// lagi). Prefix check saja tidak cukup: slot "keuangan.spm" adalah prefix string dari
	// template "keuangan.spm.lanjutan.standar" milik slot bertingkat "keuangan.spm.lanjutan"
	// yang berbeda — wajib tolak sisa setelah prefix yang masih mengandung titik.
	varian, ok := strings.CutPrefix(cfg.TemplateID, cfg.Slot+".")
	if !ok || strings.Contains(varian, ".") {
		return ErrInvalidTemplateConfig(fmt.Sprintf("template_id %q harus milik slot %q (format %q.{varian})", cfg.TemplateID, cfg.Slot, cfg.Slot))
	}
	// Validasi template_id merujuk definisi yang ADA (cegah slot menunjuk ID sembarang).
	if _, err := m.defs.Get(cfg.TemplateID); err != nil {
		return err
	}
	// Validasi tiap NILAI RoleBindings merujuk role tenant yang benar-benar terdaftar
	// (PR-N2 bagian C, backlog ROADMAP §819-827): sekali notifikasi hidup, RoleBindings
	// menentukan SIAPA menerima dokumen — binding ke role salah ketik / role tenant lain
	// tak boleh lolos tersimpan. RoleChecker tak terpasang (wiring salah) TIDAK dianggap
	// "tak ada binding untuk divalidasi" — ditolak eksplisit, bukan panic nil-interface atau
	// lolos diam-diam melewati gerbang keamanan ini.
	if len(cfg.RoleBindings) > 0 && m.roles == nil {
		return ErrInvalidTemplateConfig(
			"RoleChecker tidak terpasang di TemplateChoiceManager — tidak bisa validasi role_bindings")
	}
	for peran, roleName := range cfg.RoleBindings {
		ok, err := m.roles.RoleExists(ctx, cfg.TenantID, roleName)
		if err != nil {
			return err
		}
		if !ok {
			return ErrInvalidTemplateConfig(fmt.Sprintf(
				"role_bindings[%q]=%q bukan role terdaftar di tenant %q", peran, roleName, cfg.TenantID))
		}
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
