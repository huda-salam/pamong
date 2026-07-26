package messaging

import (
	"context"
	"log/slog"

	"github.com/huda-salam/pamong/port"
)

// LogDriver adalah MessagingPort untuk DEV/TEST: alih-alih mengirim, ia mencatat pesan
// ke log lalu melaporkan sukses. Gunanya agar alur yang butuh transport (mis. RequestOTP)
// bisa dijalankan end-to-end tanpa provider nyata — kode OTP dibaca langsung dari log dev.
//
// SENGAJA mencatat body penuh (termasuk kode OTP) — itulah gunanya di dev. Karena itu
// BERBAHAYA di production; config.Validate menolak driver=log di production (fail-fast),
// jadi driver ini tak pernah aktif di sana.
//
// Driver ini tidak pernah menghasilkan *port.MessagingError: transport in-memory tak bisa
// gagal. Validasi bentuk penerima (alamat/nomor kosong) adalah tanggung jawab caller/channel
// (mis. EmailChannel menolak alamat kosong sebelum memanggil port).
type LogDriver struct {
	log *slog.Logger
}

var _ port.MessagingPort = (*LogDriver)(nil)

// NewLogDriver membuat driver log. logger nil = slog.Default().
func NewLogDriver(logger *slog.Logger) *LogDriver {
	if logger == nil {
		logger = slog.Default()
	}
	return &LogDriver{log: logger}
}

func (d *LogDriver) SendSMS(ctx context.Context, phoneNumber, message string) error {
	d.log.InfoContext(ctx, "messaging: SMS (driver=log, tidak benar-benar dikirim)",
		slog.String("to", phoneNumber),
		slog.String("body", message),
	)
	return nil
}

func (d *LogDriver) SendEmail(ctx context.Context, email, subject, body string) error {
	d.log.InfoContext(ctx, "messaging: email (driver=log, tidak benar-benar dikirim)",
		slog.String("to", email),
		slog.String("subject", subject),
		slog.String("body", body),
	)
	return nil
}
