package notification_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core/notification"
)

// stubChannel adalah Channel palsu untuk menguji registry & Hub.
type stubChannel struct {
	name string
	sent []notification.RenderedMessage
	err  error
}

func (c *stubChannel) Name() string { return c.name }

func (c *stubChannel) Send(_ context.Context, _ string, _ notification.Recipient, msg notification.RenderedMessage) error {
	if c.err != nil {
		return c.err
	}
	c.sent = append(c.sent, msg)
	return nil
}

func TestChannelRegistry_RegisterAndGet(t *testing.T) {
	reg := notification.NewChannelRegistry()
	ch := &stubChannel{name: "in_app"}
	if err := reg.Register(ch); err != nil {
		t.Fatalf("register: %v", err)
	}
	got, err := reg.Get("in_app")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name() != "in_app" {
		t.Fatalf("nama channel = %q, mau in_app", got.Name())
	}
}

func TestChannelRegistry_RejectDuplicate(t *testing.T) {
	reg := notification.NewChannelRegistry()
	_ = reg.Register(&stubChannel{name: "email"})
	if err := reg.Register(&stubChannel{name: "email"}); err == nil {
		t.Fatal("mau error registrasi ganda, dapat nil")
	}
}

func TestChannelRegistry_RejectNilAndEmptyName(t *testing.T) {
	reg := notification.NewChannelRegistry()
	if err := reg.Register(nil); err == nil {
		t.Fatal("mau error channel nil")
	}
	if err := reg.Register(&stubChannel{name: ""}); err == nil {
		t.Fatal("mau error nama kosong")
	}
}

func TestChannelRegistry_GetUnknown(t *testing.T) {
	reg := notification.NewChannelRegistry()
	if _, err := reg.Get("push"); err == nil {
		t.Fatal("mau error channel tak terdaftar")
	}
}

func TestChannelRegistry_Names(t *testing.T) {
	reg := notification.NewChannelRegistry()
	_ = reg.Register(&stubChannel{name: "email"})
	_ = reg.Register(&stubChannel{name: "in_app"})
	names := reg.Names()
	if len(names) != 2 || names[0] != "email" || names[1] != "in_app" {
		t.Fatalf("Names() = %v, mau [email in_app] terurut", names)
	}
}

func TestInAppChannel_Send(t *testing.T) {
	inbox := notification.NewMemoryInAppInbox()
	ch := notification.NewInAppChannel(inbox)
	pid := uuid.New()
	err := ch.Send(context.Background(), "pemkot-surabaya",
		notification.Recipient{PersonID: pid},
		notification.RenderedMessage{Subject: "Disposisi", Body: "Ada disposisi baru"})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	items, _ := inbox.List(context.Background(), "pemkot-surabaya", pid.String(), 0)
	if len(items) != 1 || items[0].Subject != "Disposisi" || items[0].Body != "Ada disposisi baru" {
		t.Fatalf("inbox items = %+v", items)
	}
}
