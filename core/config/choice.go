package config

import (
	"context"
	"time"

	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/port"
)

// ChoiceManager adalah jalur TULIS ber-tata-kelola untuk pilihan config ber-versi (mis.
// pilihan strategy per-tenant). Ia membungkus store dengan dua invarian PR-3.3.3:
//
//   - Ber-versi & ter-audit: tiap SetChoice menambah versi baru (append-only) yang mencatat
//     SIAPA (SetBy dari AuthContext) dan SEJAK KAPAN (EffectiveFrom). Tabel ber-versi itu
//     sendiri adalah jejaknya — pola yang sama dengan gov.workflow_definitions (created_by +
//     version), bukan entri gov.audit_logs terpisah.
//   - Non-retroaktif: pilihan tidak boleh diubah untuk periode fiskal yang sudah terkunci.
//     Gerbang ini lewat port.FiscalChecker (seam); bila checker nil (fiskal belum di-wire,
//     DEFERRED ke modul keuangan), gerbang dilewati dan hanya effective-date resolution yang
//     menjaga non-retroaktivitas baca.
//
// Permission check BUKAN tanggung jawab manager ini — itu milik use case admin pemanggil
// (belum ada; lihat ROADMAP). Manager hanya menegakkan versi + audit + gerbang periode.
type ChoiceManager struct {
	store  TenantConfigStore
	fiscal port.FiscalChecker // opsional; nil = gerbang periode dilewati (seam)
	now    func() time.Time
}

// NewChoiceManager membuat manager. fiscal boleh nil selama modul fiskal belum ada.
func NewChoiceManager(store TenantConfigStore, fiscal port.FiscalChecker) *ChoiceManager {
	return &ChoiceManager{store: store, fiscal: fiscal, now: time.Now}
}

// SetChoice menetapkan pilihan config ber-scope untuk aktor tertentu, berlaku sejak
// effectiveFrom (nol → sekarang). Menolak bila effectiveFrom jatuh di periode fiskal yang
// hard-closed (non-retroaktif). Menambah versi baru; pilihan lama tetap terbaca untuk tanggal
// lama lewat Resolver.ResolveAsOf.
func (m *ChoiceManager) SetChoice(ctx port.AuthContext, scope ConfigScope, key, value string, effectiveFrom time.Time) error {
	if effectiveFrom.IsZero() {
		effectiveFrom = m.now()
	}
	if err := m.guardPeriod(ctx, scope.TenantID, effectiveFrom); err != nil {
		return err
	}
	actor := ctx.PersonID()
	return m.store.Set(ctx, ConfigEntry{
		Scope:         scope,
		Key:           key,
		Value:         value,
		EffectiveFrom: effectiveFrom,
		SetBy:         &actor,
	})
}

// guardPeriod menolak penetapan pilihan yang berlaku di periode yang sudah hard-closed.
func (m *ChoiceManager) guardPeriod(ctx context.Context, tenantID string, effectiveFrom time.Time) error {
	if m.fiscal == nil {
		return nil
	}
	status, err := m.fiscal.CheckPeriod(ctx, tenantID, effectiveFrom)
	if err != nil {
		return err
	}
	if status == port.FiscalHardClosed {
		return ErrPeriodLocked(effectiveFrom)
	}
	return nil
}

// ErrPeriodLocked dipublikasikan saat pilihan hendak ditetapkan berlaku di periode fiskal
// yang sudah terkunci (hard-closed) — perubahan retroaktif dilarang.
func ErrPeriodLocked(effectiveFrom time.Time) error {
	return core.ErrValidation("effective_from",
		"periode "+effectiveFrom.Format("2006-01-02")+" sudah terkunci (hard-closed) — pilihan tidak boleh diubah retroaktif")
}
