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
