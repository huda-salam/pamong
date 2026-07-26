// Package messaging adalah driven adapter yang menyediakan port.MessagingPort:
// transport pesan keluar (SMS/email) untuk OTP identity dan channel notifikasi.
//
// messaging.go adalah entry: factory pemilih driver (log|smtp). Implementasi konkret
// ada di file driver (log.go, smtp.go). Pemanggil (use case OTP, EmailChannel) tetap
// bergantung pada port.MessagingPort sehingga mengganti driver tidak mengubah kodenya
// (titik ekstensi #1, registry pattern — pola sama infra/storage).
package messaging

import (
	"fmt"

	"github.com/huda-salam/pamong/core/config"
	"github.com/huda-salam/pamong/port"
)

// NewFromConfig membuat MessagingPort siap pakai dari konfigurasi. Driver dipilih
// berdasarkan cfg.Driver.
//
// Menambah driver baru (mis. provider SMS pemda): tambahkan case di sini dan
// implementasikan port.MessagingPort di file driver baru. Driver "log" adalah default
// dev (nol dependency); driver transport nyata (smtp/dst) dipilih saat onboarding.
func NewFromConfig(cfg config.MessagingConfig) (port.MessagingPort, error) {
	switch cfg.Driver {
	case "log", "":
		return NewLogDriver(nil), nil
	case "smtp":
		return NewSMTP(cfg)
	default:
		return nil, fmt.Errorf("messaging: driver tidak dikenal: %q (pilihan: log|smtp)", cfg.Driver)
	}
}
