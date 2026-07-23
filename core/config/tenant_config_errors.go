package config

import "github.com/huda-salam/pamong/core"

// ValidateEntry memvalidasi invarian ConfigEntry sebelum disimpan: tenant & key wajib,
// dan ResourceID hanya boleh di-set bila UnitKerjaID juga di-set (resource ber-nested di
// bawah unit kerja). Dipakai oleh MemoryTenantConfigStore maupun store Postgres (yang juga
// menegakkan hal yang sama lewat CHECK) agar keduanya menolak input tak-valid seragam.
func ValidateEntry(e ConfigEntry) error {
	if e.Scope.TenantID == "" {
		return core.ErrValidation("tenant_id", "wajib diisi")
	}
	if e.Key == "" {
		return core.ErrValidation("config_key", "wajib diisi")
	}
	if e.Scope.ResourceID != nil && e.Scope.UnitKerjaID == nil {
		return core.ErrValidation("resource_id",
			"resource_id hanya boleh di-set bila unit_kerja_id juga di-set")
	}
	return nil
}
