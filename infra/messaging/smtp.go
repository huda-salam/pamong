package messaging

import (
	"context"
	"fmt"
	"mime"
	"net/smtp"
	"strings"

	"github.com/huda-salam/pamong/core/config"
	"github.com/huda-salam/pamong/port"
)

// SMTP adalah MessagingPort untuk email nyata lewat stdlib net/smtp — tanpa dependency
// eksternal, kompatibel dengan relay SMTP apa pun (termasuk MailHog di dev). Ini driver
// transport nyata paling sederhana untuk email.
//
// SMS TIDAK didukung driver ini: SMTP hanya email. SendSMS mengembalikan
// *port.MessagingError PERMANENT. Provider SMS nyata = driver terpisah yang dipilih saat
// onboarding (lihat NewFromConfig).
type SMTP struct {
	addr string // host:port
	from string
	auth smtp.Auth // nil bila server tak butuh auth

	// sendMail dapat diganti di test agar tak membuka koneksi TCP nyata. Default = smtp.SendMail.
	sendMail func(addr string, a smtp.Auth, from string, to []string, msg []byte) error
}

var _ port.MessagingPort = (*SMTP)(nil)

// NewSMTP merakit driver SMTP dari config. Host & from wajib — kosong = gagal saat boot
// (fail-fast) ketimbang gagal per-kirim. Auth dipasang hanya bila user diisi.
func NewSMTP(cfg config.MessagingConfig) (*SMTP, error) {
	if cfg.SMTPHost == "" {
		return nil, fmt.Errorf("messaging: driver smtp butuh smtp_host (GOV_MESSAGING_SMTP_HOST)")
	}
	if cfg.FromEmail == "" {
		return nil, fmt.Errorf("messaging: driver smtp butuh from_email (GOV_MESSAGING_FROM_EMAIL)")
	}
	// From masuk mentah ke header; alamat ber-CR/LF membuka header injection (lihat SendEmail).
	if containsCRLF(cfg.FromEmail) {
		return nil, fmt.Errorf("messaging: from_email tidak boleh mengandung karakter CR/LF")
	}
	smtpPort := cfg.SMTPPort
	if smtpPort == 0 {
		smtpPort = 587
	}
	var auth smtp.Auth
	if cfg.SMTPUser != "" {
		auth = smtp.PlainAuth("", cfg.SMTPUser, cfg.SMTPPassword, cfg.SMTPHost)
	}
	return &SMTP{
		addr:     fmt.Sprintf("%s:%d", cfg.SMTPHost, smtpPort),
		from:     cfg.FromEmail,
		auth:     auth,
		sendMail: smtp.SendMail,
	}, nil
}

func (s *SMTP) SendEmail(ctx context.Context, email, subject, body string) error {
	if strings.TrimSpace(email) == "" {
		return &port.MessagingError{Code: port.MsgErrInvalidRecipient}
	}
	// Cegah email header injection: alamat masuk mentah ke header To:, jadi CR/LF di dalamnya
	// bisa menyisipkan header tambahan (mis. Bcc). Alamat sering berasal dari input (mis.
	// CredValue OTP citizen), maka WAJIB ditolak di sini — bukan diandalkan tervalidasi hulu.
	if containsCRLF(email) {
		return &port.MessagingError{
			Code: port.MsgErrInvalidRecipient,
			Err:  fmt.Errorf("alamat email mengandung karakter CR/LF"),
		}
	}
	msg := buildEmailMessage(s.from, email, subject, body)

	// smtp.SendMail tidak menerima context; jalankan di goroutine agar pembatalan/deadline ctx
	// membebaskan pemanggil (mis. RequestOTP) meski server SMTP menggantung. Channel ber-buffer
	// agar goroutine tak bocor bila ctx keburu batal (ia tetap selesai saat sendMail balik).
	errCh := make(chan error, 1)
	go func() { errCh <- s.sendMail(s.addr, s.auth, s.from, []string{email}, msg) }()
	select {
	case <-ctx.Done():
		return &port.MessagingError{Code: port.MsgErrTransient, Err: ctx.Err()}
	case err := <-errCh:
		if err != nil {
			// Klasifikasi halus (auth vs greylist vs alamat) butuh mem-parse balasan provider
			// yang rapuh; driver stdlib ini memetakan semua kegagalan kirim ke TRANSIENT (boleh
			// retry). Driver provider nyata boleh memetakan lebih presisi kelak.
			return &port.MessagingError{Code: port.MsgErrTransient, Err: err}
		}
		return nil
	}
}

// SendSMS tidak didukung driver SMTP (email-only). PERMANENT: retry tak akan menolong;
// butuh driver SMS yang benar.
func (s *SMTP) SendSMS(ctx context.Context, phoneNumber, message string) error {
	return &port.MessagingError{
		Code: port.MsgErrPermanent,
		Err:  fmt.Errorf("driver smtp tidak mendukung SMS (gunakan driver provider SMS)"),
	}
}

// containsCRLF melaporkan apakah s memuat CR atau LF — karakter yang, bila masuk ke sebuah
// header email, memungkinkan penyisipan header tambahan (email header injection).
func containsCRLF(s string) bool { return strings.ContainsAny(s, "\r\n") }

// buildEmailMessage menyusun pesan RFC 5322 sederhana (text/plain UTF-8). Subject di-encode
// RFC 2047 agar aman untuk karakter non-ASCII. Header dipisah CRLF sesuai protokol SMTP.
func buildEmailMessage(from, to, subject, body string) []byte {
	var b strings.Builder
	b.WriteString("From: " + from + "\r\n")
	b.WriteString("To: " + to + "\r\n")
	b.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", subject) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=\"UTF-8\"\r\n")
	b.WriteString("\r\n")
	b.WriteString(body)
	return []byte(b.String())
}
