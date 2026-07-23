package notification

import (
	"context"
	"errors"
	"time"
)

// Hub adalah entry point pengiriman notifikasi. Alurnya: render template per-tenant (sekali,
// untuk locale penerima) → kirim ke tiap channel yang diminta → catat status tiap upaya.
// Hub tidak menyimpan business logic domain apa pun; ia hanya mengorkestrasi channel +
// template + tracking. Pemetaan peran→penerima (routing) dilakukan SEBELUM Hub (PR-3.6.2).
type Hub struct {
	channels *ChannelRegistry
	engine   *TemplateEngine
	recorder DeliveryRecorder
	now      func() time.Time
}

// NewHub merakit hub dari registry channel, engine template, dan recorder.
func NewHub(channels *ChannelRegistry, engine *TemplateEngine, recorder DeliveryRecorder) *Hub {
	return &Hub{channels: channels, engine: engine, recorder: recorder, now: time.Now}
}

// Send merender template lalu mengirim notifikasi ke tiap channel yang diminta.
//
// Kegagalan pra-dispatch (tak ada channel, channel tak terdaftar, template tak ada / gagal
// render) dikembalikan sebagai error TANPA mencatat delivery — ini bug caller/konfigurasi,
// bukan kegagalan pengiriman. Kegagalan transport per-channel DICATAT (StatusFailed) lalu
// digabung dan dikembalikan, sehingga pemanggil asinkron (outbox/relay) bisa memutuskan retry
// tanpa kehilangan jejak channel mana yang gagal. Channel yang sukses tetap terkirim.
func (h *Hub) Send(ctx context.Context, n Notification) error {
	if len(n.Channels) == 0 {
		return ErrNoChannel()
	}

	// Resolusi semua channel dulu (fail-fast bila ada yang tak terdaftar — bug wiring).
	chans := make([]Channel, 0, len(n.Channels))
	for _, name := range n.Channels {
		ch, err := h.channels.Get(name)
		if err != nil {
			return err
		}
		chans = append(chans, ch)
	}

	msg, err := h.engine.Render(ctx, n.TenantID, n.TemplateKey, n.Recipient.LocaleOrDefault(), n.Data)
	if err != nil {
		return err
	}

	var sendErrs []error
	for _, ch := range chans {
		sendErr := ch.Send(ctx, n.TenantID, n.Recipient, msg)
		rec := DeliveryRecord{
			TenantID:    n.TenantID,
			PersonID:    n.Recipient.PersonID,
			Channel:     ch.Name(),
			TemplateKey: n.TemplateKey,
			Status:      StatusDelivered,
			At:          h.now(),
		}
		if sendErr != nil {
			rec.Status = StatusFailed
			rec.Error = sendErr.Error()
			sendErrs = append(sendErrs, sendErr)
		}
		if err := h.recorder.Record(ctx, rec); err != nil {
			// Gagal mencatat = kehilangan jejak; angkat agar tak diam-diam hilang.
			sendErrs = append(sendErrs, err)
		}
	}
	return errors.Join(sendErrs...)
}
