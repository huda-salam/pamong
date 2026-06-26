// Package permission menyediakan engine RBAC: definisi permission, role, dan evaluasi
// keputusan akses. PR-2.3.1 mengirim evaluasi dasar in-memory; PR-2.3.3 menegakkan resolusi
// konflik penuh (global-precedence + strict-intersection) saat lapis central (id.central_roles,
// 2.3.2) dan tenant (gov.tenant_roles, di paket tenantrole) hidup berdampingan lewat
// CompositeCatalog. Export/import manifest (2.3.4), ABAC, hierarki OPD, dan delegasi/PLT (2.3.5)
// menyusul tanpa mengubah kontrak Engine (lihat PRD & ROADMAP).
package permission

// Permission adalah string izin berformat {modul}:{entity}:{aksi},
// mis. "surat_masuk:surat:buat". Selalu dirujuk lewat konstanta (CODE_CONVENTION #8).
// Alias string agar interoperabel dengan gateway.Context yang membawa string mentah.
type Permission = string

// Layer adalah lapisan asal role; menentukan prioritas resolusi konflik (F7 PRD):
// global menang atas semua, scoped setara tenant. Ditegakkan penuh oleh Engine.Allows
// sejak PR-2.3.3 (lihat engine.go).
type Layer int

const (
	// LayerTenant — role tenant (gov.tenant_roles), berlaku di dalam satu tenant.
	LayerTenant Layer = iota
	// LayerScoped — central scoped role (tenant_scope[]), setara tenant di scope-nya.
	LayerScoped
	// LayerGlobal — central global role; menang atas semua role lain.
	LayerGlobal
)

// Role adalah definisi satu role: nama, lapisan asal, dan permission yang diberikan.
type Role struct {
	Name        string
	Layer       Layer
	Permissions []Permission
}
