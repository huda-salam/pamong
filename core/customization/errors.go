package customization

import (
	"fmt"

	"github.com/huda-salam/pamong/core"
)

// ErrInvalidCapabilityName dipublikasikan saat nama capability tidak sesuai konvensi
// {modul}.{fitur} (minimal 2 segmen non-kosong) (HTTP 422).
func ErrInvalidCapabilityName(name, reason string) error {
	return core.ErrValidation("capability", fmt.Sprintf("nama %q: %s", name, reason))
}

// ErrCapabilityExists dipublikasikan saat sebuah capability didaftarkan dua kali —
// registrasi ganda menandakan bug wiring modul (HTTP 409).
func ErrCapabilityExists(name string) error {
	return core.ErrConflict(fmt.Sprintf("capability %q sudah terdaftar", name))
}

// ErrUnknownCapability dipublikasikan saat gate merujuk capability yang tidak terdaftar —
// fail-closed, tak ada anggapan "aktif" diam-diam (HTTP 404).
func ErrUnknownCapability(name string) error {
	return core.ErrNotFound("Capability", name)
}

// ErrCustomFieldInvalid dipublikasikan saat definisi custom field melanggar invarian struktural
// (target tak lengkap, class tak dikenal, dst) (HTTP 422). field boleh "" bila nama belum ada.
func ErrCustomFieldInvalid(field, reason string) error {
	if field == "" {
		return core.ErrValidation("custom_field", reason)
	}
	return core.ErrValidation("custom_field", fmt.Sprintf("field %q: %s", field, reason))
}

// ErrCustomFieldExists dipublikasikan saat nama custom field bentrok dengan field lain (inti
// atau custom aktif) pada entity yang sama (HTTP 409).
func ErrCustomFieldExists(field string) error {
	return core.ErrConflict(fmt.Sprintf("custom field %q sudah ada pada entity ini", field))
}

// ErrCustomFieldNotFound dipublikasikan saat custom field yang dirujuk (mis. untuk deaktivasi)
// tidak ditemukan (HTTP 404).
func ErrCustomFieldNotFound(field string) error {
	return core.ErrNotFound("CustomField", field)
}

// ErrEntityNotFound dipublikasikan saat custom field menargetkan module.entity yang tidak
// terdaftar di registry — fail-closed, tak boleh menambah field ke entity tak dikenal (HTTP 404).
func ErrEntityNotFound(module, entity string) error {
	return core.ErrNotFound("Entity", module+"."+entity)
}
