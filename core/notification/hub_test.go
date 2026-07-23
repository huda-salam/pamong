package notification_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core/notification"
	"github.com/huda-salam/pamong/port"
	"github.com/huda-salam/pamong/testkit"
)

func newHubEnv(t *testing.T) (*notification.Hub, *notification.MemoryInAppInbox, *testkit.MockMessaging, *notification.MemoryDeliveryRecorder) {
	t.Helper()
	tmplStore := notification.NewMemoryTemplateStore()
	_ = tmplStore.Upsert(context.Background(), notification.Template{
		Key: "surat.disposisi.dibuat", Locale: "id",
		Subject: "Disposisi {{.nomor}}", Body: "Halo {{.nama}}, ada disposisi",
	})
	engine := notification.NewTemplateEngine(tmplStore)

	inbox := notification.NewMemoryInAppInbox()
	msg := testkit.NewMockMessaging()
	reg := notification.NewChannelRegistry()
	_ = reg.Register(notification.NewInAppChannel(inbox))
	_ = reg.Register(notification.NewEmailChannel(msg))

	rec := notification.NewMemoryDeliveryRecorder()
	return notification.NewHub(reg, engine, rec), inbox, msg, rec
}

func sampleNotif(channels ...string) notification.Notification {
	return notification.Notification{
		TenantID:    "pemkot-surabaya",
		Recipient:   notification.Recipient{PersonID: uuid.New(), Email: "budi@example.gov", Locale: "id"},
		TemplateKey: "surat.disposisi.dibuat",
		Data:        map[string]any{"nomor": "001/IN", "nama": "Budi"},
		Channels:    channels,
	}
}

func TestHub_SendInAppAndEmail(t *testing.T) {
	hub, inbox, msg, rec := newHubEnv(t)
	n := sampleNotif(notification.ChannelInApp, notification.ChannelEmail)
	if err := hub.Send(context.Background(), n); err != nil {
		t.Fatalf("send: %v", err)
	}

	items, _ := inbox.List(context.Background(), n.TenantID, n.Recipient.PersonID.String(), 0)
	if len(items) != 1 || items[0].Subject != "Disposisi 001/IN" {
		t.Fatalf("inbox = %+v", items)
	}
	emails := msg.SentEmails()
	if len(emails) != 1 || emails[0].To != "budi@example.gov" || emails[0].Body != "Halo Budi, ada disposisi" {
		t.Fatalf("emails = %+v", emails)
	}
	if got := len(rec.Records()); got != 2 {
		t.Fatalf("delivery records = %d, mau 2", got)
	}
	for _, r := range rec.Records() {
		if r.Status != notification.StatusDelivered {
			t.Fatalf("status = %q, mau delivered", r.Status)
		}
	}
}

func TestHub_NoChannel(t *testing.T) {
	hub, _, _, _ := newHubEnv(t)
	if err := hub.Send(context.Background(), sampleNotif()); err == nil {
		t.Fatal("mau ErrNoChannel")
	}
}

func TestHub_UnknownChannel(t *testing.T) {
	hub, _, _, rec := newHubEnv(t)
	if err := hub.Send(context.Background(), sampleNotif("push")); err == nil {
		t.Fatal("mau error channel tak terdaftar")
	}
	if len(rec.Records()) != 0 {
		t.Fatal("channel tak terdaftar = bug caller; tak boleh mencatat delivery")
	}
}

func TestHub_TemplateNotFound(t *testing.T) {
	hub, _, _, rec := newHubEnv(t)
	n := sampleNotif(notification.ChannelInApp)
	n.TemplateKey = "tak.ada"
	if err := hub.Send(context.Background(), n); err == nil {
		t.Fatal("mau ErrTemplateNotFound")
	}
	if len(rec.Records()) != 0 {
		t.Fatal("render gagal = tak ada dispatch; tak boleh mencatat delivery")
	}
}

func TestHub_EmailFailureRecordedAndReturned(t *testing.T) {
	hub, inbox, msg, rec := newHubEnv(t)
	msg.FailEmail = &port.MessagingError{Code: port.MsgErrTransient}

	n := sampleNotif(notification.ChannelInApp, notification.ChannelEmail)
	err := hub.Send(context.Background(), n)
	if err == nil {
		t.Fatal("mau error gabungan karena email gagal")
	}
	// in-app tetap sukses meski email gagal
	items, _ := inbox.List(context.Background(), n.TenantID, n.Recipient.PersonID.String(), 0)
	if len(items) != 1 {
		t.Fatalf("in-app harus tetap terkirim; items = %+v", items)
	}
	var failed, delivered int
	for _, r := range rec.Records() {
		switch r.Status {
		case notification.StatusFailed:
			failed++
			if r.Channel != notification.ChannelEmail {
				t.Fatalf("yang gagal harusnya email, dapat %q", r.Channel)
			}
		case notification.StatusDelivered:
			delivered++
		}
	}
	if failed != 1 || delivered != 1 {
		t.Fatalf("records: failed=%d delivered=%d, mau 1/1", failed, delivered)
	}
}

func TestHub_EmailEmptyAddressFails(t *testing.T) {
	hub, _, _, rec := newHubEnv(t)
	n := sampleNotif(notification.ChannelEmail)
	n.Recipient.Email = "" // alamat kosong
	if err := hub.Send(context.Background(), n); err == nil {
		t.Fatal("mau error karena alamat email kosong")
	}
	recs := rec.Records()
	if len(recs) != 1 || recs[0].Status != notification.StatusFailed {
		t.Fatalf("records = %+v, mau 1 failed", recs)
	}
}
