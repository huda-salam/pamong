package port

import "context"

// MessagingPort mengirim pesan keluar lewat kanal (SMS, email) ke penerima. Didefinisikan
// sebagai port lintas-modul agar use case (mis. RequestOTP di identity) tetap bebas dependency
// provider: detail Twilio/SNS/SMTP hidup di infra/messaging, use case hanya "kirim ke penerima".
//
// Pesan diasumsikan sudah dirakit caller (template + nilai). Port ini tidak menyusun konten —
// ia hanya transport. Kegagalan dikembalikan sebagai *MessagingError agar caller bisa memetakan
// jenis kegagalan TANPA membocorkan detail provider ke klien.
type MessagingPort interface {
	SendSMS(ctx context.Context, phoneNumber, message string) error
	SendEmail(ctx context.Context, email, subject, body string) error
}

// MessagingErrorCode mengklasifikasi kegagalan pengiriman tanpa mengekspos detail provider.
type MessagingErrorCode string

const (
	// MsgErrInvalidRecipient: nomor/email tujuan tidak valid (kesalahan input — permanen).
	MsgErrInvalidRecipient MessagingErrorCode = "INVALID_RECIPIENT"
	// MsgErrTransient: kegagalan sementara (provider down, kuota, timeout) — boleh retry kelak.
	MsgErrTransient MessagingErrorCode = "TRANSIENT"
	// MsgErrPermanent: kegagalan permanen lain (kredensial provider salah, dll).
	MsgErrPermanent MessagingErrorCode = "PERMANENT"
)

// MessagingError membungkus kegagalan pengiriman dengan klasifikasi. Code dipakai caller untuk
// memutuskan tindakan (retry vs tolak) tanpa mem-parse string provider; Err menyimpan penyebab
// asli untuk log internal — JANGAN diteruskan ke respons klien.
type MessagingError struct {
	Code MessagingErrorCode
	Err  error
}

func (e *MessagingError) Error() string {
	if e.Err != nil {
		return string(e.Code) + ": " + e.Err.Error()
	}
	return string(e.Code)
}

func (e *MessagingError) Unwrap() error { return e.Err }
