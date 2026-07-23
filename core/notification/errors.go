package notification

import (
	"fmt"

	"github.com/huda-salam/pamong/core"
)

// ErrChannelExists dipublikasikan saat sebuah channel didaftarkan dua kali ke registry —
// registrasi ganda menandakan bug wiring saat bootstrap (HTTP 409).
func ErrChannelExists(name string) error {
	return core.ErrConflict(fmt.Sprintf("notification channel %q sudah terdaftar", name))
}

// ErrNilChannel dipublikasikan saat channel nil didaftarkan (HTTP 422).
func ErrNilChannel(name string) error {
	return core.ErrValidation("channel", fmt.Sprintf("channel %q tidak boleh nil", name))
}

// ErrChannelNotRegistered dipublikasikan saat notifikasi merujuk channel yang tak terdaftar —
// tak ada fallback diam-diam ke channel lain (HTTP 404).
func ErrChannelNotRegistered(name string) error {
	return core.ErrNotFound("NotificationChannel", name)
}

// ErrNoChannel dipublikasikan saat Notification tidak menyebut channel tujuan apa pun (HTTP 422).
func ErrNoChannel() error {
	return core.ErrValidation("channels", "notifikasi harus menyebut minimal satu channel")
}

// ErrTemplateNotFound dipublikasikan saat tak ada template (tenant-spesifik maupun global)
// untuk kunci yang diminta (HTTP 404).
func ErrTemplateNotFound(tenantID, key string) error {
	return core.ErrNotFound("NotificationTemplate", fmt.Sprintf("tenant=%s key=%s", tenantID, key))
}

// ErrTemplateRender dipublikasikan saat parse/eksekusi template gagal — mis. sintaks
// template salah atau field data yang dirujuk tak ada (missingkey=error) (HTTP 422).
func ErrTemplateRender(key, reason string) error {
	return core.ErrValidation("template", fmt.Sprintf("render template %q gagal: %s", key, reason))
}

// ErrInvalidTemplate dipublikasikan saat template yang disimpan tidak valid (kunci/body kosong)
// (HTTP 422).
func ErrInvalidTemplate(reason string) error {
	return core.ErrValidation("template", reason)
}

// ErrInvalidPersonID dipublikasikan saat person_id yang diberikan bukan UUID valid (HTTP 422).
func ErrInvalidPersonID(value string) error {
	return core.ErrValidation("person_id", fmt.Sprintf("person_id %q bukan UUID valid", value))
}

// ErrNoRecipient dipublikasikan saat sebuah peran tak punya pemegang definitif MAUPUN PLT —
// notifikasi ke peran tak bertuan tak boleh hilang diam-diam, itu salah konfigurasi (HTTP 404).
func ErrNoRecipient(tenantID, role string) error {
	return core.ErrNotFound("NotificationRecipient",
		fmt.Sprintf("tenant=%s role=%s (pemegang & PLT kosong)", tenantID, role))
}
