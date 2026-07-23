package notification

import "context"

// ChannelInApp adalah nama channel in-app di registry.
const ChannelInApp = "in_app"

// InAppChannel mengirim notifikasi ke kotak masuk in-app penerima (InAppInbox). Tak ada
// transport eksternal — "pengiriman" = menyimpan item agar UI penerima menampilkannya.
type InAppChannel struct {
	inbox InAppInbox
}

// NewInAppChannel merakit channel di atas inbox.
func NewInAppChannel(inbox InAppInbox) *InAppChannel {
	return &InAppChannel{inbox: inbox}
}

var _ Channel = (*InAppChannel)(nil)

// Name mengembalikan ChannelInApp.
func (c *InAppChannel) Name() string { return ChannelInApp }

// Send menyimpan pesan sebagai item inbox milik penerima. Subject → judul, Body → isi.
func (c *InAppChannel) Send(ctx context.Context, tenantID string, r Recipient, msg RenderedMessage) error {
	_, err := c.inbox.Append(ctx, InAppItem{
		TenantID: tenantID,
		PersonID: r.PersonID,
		Subject:  msg.Subject,
		Body:     msg.Body,
	})
	return err
}
