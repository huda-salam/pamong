package notification

import (
	"context"

	"github.com/huda-salam/pamong/port"
)

// ChannelEmail adalah nama channel email di registry.
const ChannelEmail = "email"

// EmailChannel mengirim notifikasi lewat email menggunakan port.MessagingPort. Channel ini
// hanya TRANSPORT — konten sudah dirender Hub. Detail provider (SMTP/SES) hidup di
// infra/messaging yang mengimplementasi MessagingPort; channel tak tahu providernya.
type EmailChannel struct {
	messaging port.MessagingPort
}

// NewEmailChannel merakit channel di atas MessagingPort.
func NewEmailChannel(m port.MessagingPort) *EmailChannel {
	return &EmailChannel{messaging: m}
}

var _ Channel = (*EmailChannel)(nil)

// Name mengembalikan ChannelEmail.
func (c *EmailChannel) Name() string { return ChannelEmail }

// Send mengirim email ke r.Email. Alamat kosong = tak bisa dikirim → *port.MessagingError
// berkode INVALID_RECIPIENT (permanen, tanpa menebak alamat) yang dicatat Hub sebagai gagal.
func (c *EmailChannel) Send(ctx context.Context, _ string, r Recipient, msg RenderedMessage) error {
	if r.Email == "" {
		return &port.MessagingError{Code: port.MsgErrInvalidRecipient}
	}
	return c.messaging.SendEmail(ctx, r.Email, msg.Subject, msg.Body)
}
