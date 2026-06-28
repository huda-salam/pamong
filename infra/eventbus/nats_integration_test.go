//go:build integration

package eventbus_test

import (
	"context"
	"testing"
	"time"

	natssrv "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"

	"github.com/huda-salam/pamong/core/config"
	"github.com/huda-salam/pamong/infra/eventbus"
	"github.com/huda-salam/pamong/port"
)

// startEmbeddedNATS menjalankan NATS server in-process pada port acak dan
// mengembalikan URL client-nya. Server di-shutdown saat test selesai via Cleanup.
func startEmbeddedNATS(t *testing.T) string {
	t.Helper()
	opts := &natssrv.Options{
		Host:   "127.0.0.1",
		Port:   -1, // biarkan OS memilih port bebas
		NoLog:  true,
		NoSigs: true,
	}
	srv, err := natssrv.NewServer(opts)
	if err != nil {
		t.Fatalf("buat NATS server: %v", err)
	}
	go srv.Start()
	if !srv.ReadyForConnections(3 * time.Second) {
		t.Fatal("NATS server tidak siap dalam 3 detik")
	}
	t.Cleanup(srv.Shutdown)
	return srv.ClientURL()
}

// newNATSConn membuka koneksi ke url dan mendaftarkan Cleanup untuk menutupnya.
func newNATSConn(t *testing.T, url string) *nats.Conn {
	t.Helper()
	nc, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("koneksi NATS: %v", err)
	}
	t.Cleanup(nc.Close)
	return nc
}

// newNATSSchema membuat SchemaRegistry dengan event suratDiterima terdaftar.
func newNATSSchema(t *testing.T) *eventbus.SchemaRegistry {
	t.Helper()
	schema := eventbus.NewSchemaRegistry()
	if err := schema.Register(eventSuratDiterima, suratDiterima{}); err != nil {
		t.Fatalf("register schema: %v", err)
	}
	return schema
}

// TestNATSDriver_PublishSubscribe_LintasKoneksi adalah DoD PR-3.1.3: publish dari
// satu koneksi, subscribe dari koneksi lain ke broker NATS sungguhan. Event harus
// tiba dengan payload bertipe konkret (bukan map[string]any).
func TestNATSDriver_PublishSubscribe_LintasKoneksi(t *testing.T) {
	url := startEmbeddedNATS(t)
	schema := newNATSSchema(t)

	// Subscriber — koneksi pertama (simulasi proses/node berbeda)
	sub := eventbus.NewNATSDriver(newNATSConn(t, url), schema)
	received := make(chan port.Event, 1)
	if err := sub.Subscribe(eventSuratDiterima, func(_ context.Context, e port.Event) error {
		received <- e
		return nil
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	// Publisher — koneksi kedua
	pub := eventbus.NewNATSDriver(newNATSConn(t, url), schema)
	want := port.Event{
		Name:     eventSuratDiterima,
		Payload:  suratDiterima{NomorSurat: "001/IN/2025"},
		TenantID: "pemkot-surabaya",
	}
	if err := pub.Dispatch(context.Background(), want); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	select {
	case got := <-received:
		if got.TenantID != want.TenantID {
			t.Errorf("tenant_id: mau %q dapat %q", want.TenantID, got.TenantID)
		}
		sd, ok := got.Payload.(suratDiterima)
		if !ok {
			t.Fatalf("tipe payload salah: %T — harus suratDiterima, bukan map", got.Payload)
		}
		if sd.NomorSurat != "001/IN/2025" {
			t.Errorf("payload tidak utuh: %q", sd.NomorSurat)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout: event tidak diterima dalam 2 detik")
	}
}

// TestNATSDriver_MetadataUtuh membuktikan TenantID, CausedBy, dan IdempotencyKey
// bertahan melewati serialisasi JSON dan transport NATS dengan utuh.
func TestNATSDriver_MetadataUtuh(t *testing.T) {
	url := startEmbeddedNATS(t)
	schema := newNATSSchema(t)
	nc := newNATSConn(t, url)
	driver := eventbus.NewNATSDriver(nc, schema)

	received := make(chan port.Event, 1)
	if err := driver.Subscribe(eventSuratDiterima, func(_ context.Context, e port.Event) error {
		received <- e
		return nil
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	want := port.Event{
		Name:           eventSuratDiterima,
		Payload:        suratDiterima{NomorSurat: "002/IN/2025"},
		TenantID:       "pemprov-jatim",
		CausedBy:       "uuid-user-abc",
		IdempotencyKey: "idem-key-xyz",
	}
	if err := driver.Dispatch(context.Background(), want); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	select {
	case got := <-received:
		if got.CausedBy != want.CausedBy {
			t.Errorf("caused_by: mau %q dapat %q", want.CausedBy, got.CausedBy)
		}
		if got.IdempotencyKey != want.IdempotencyKey {
			t.Errorf("idempotency_key: mau %q dapat %q", want.IdempotencyKey, got.IdempotencyKey)
		}
		if got.TenantID != want.TenantID {
			t.Errorf("tenant_id: mau %q dapat %q", want.TenantID, got.TenantID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout: event tidak diterima dalam 2 detik")
	}
}

// TestNATSDriver_MultiSubscriber membuktikan satu event diterima semua subscriber
// yang terdaftar (NATS fanout).
func TestNATSDriver_MultiSubscriber(t *testing.T) {
	url := startEmbeddedNATS(t)
	schema := newNATSSchema(t)

	nc1 := newNATSConn(t, url)
	nc2 := newNATSConn(t, url)
	pub := eventbus.NewNATSDriver(newNATSConn(t, url), schema)

	recv1 := make(chan struct{}, 1)
	recv2 := make(chan struct{}, 1)

	if err := eventbus.NewNATSDriver(nc1, schema).Subscribe(eventSuratDiterima,
		func(_ context.Context, _ port.Event) error { recv1 <- struct{}{}; return nil },
	); err != nil {
		t.Fatalf("subscribe 1: %v", err)
	}
	if err := eventbus.NewNATSDriver(nc2, schema).Subscribe(eventSuratDiterima,
		func(_ context.Context, _ port.Event) error { recv2 <- struct{}{}; return nil },
	); err != nil {
		t.Fatalf("subscribe 2: %v", err)
	}

	if err := pub.Dispatch(context.Background(), port.Event{
		Name:    eventSuratDiterima,
		Payload: suratDiterima{NomorSurat: "003/IN/2025"},
	}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	timeout := time.After(2 * time.Second)
	for i, ch := range []chan struct{}{recv1, recv2} {
		select {
		case <-ch:
		case <-timeout:
			t.Fatalf("subscriber %d tidak menerima event dalam 2 detik", i+1)
		}
	}
}

// TestNewFromConfig_DriverNATS membuktikan factory NewFromConfig menghasilkan Bus
// yang berfungsi dengan driver NATS.
func TestNewFromConfig_DriverNATS(t *testing.T) {
	url := startEmbeddedNATS(t)
	schema := eventbus.NewSchemaRegistry()
	if err := schema.Register(eventSuratDiterima, suratDiterima{}); err != nil {
		t.Fatalf("register schema: %v", err)
	}

	cfg := config.EventBusConfig{Driver: "nats", URL: url}
	bus, err := eventbus.NewFromConfig(cfg, schema)
	if err != nil {
		t.Fatalf("NewFromConfig: %v", err)
	}

	received := make(chan port.Event, 1)
	if err := bus.Subscribe(eventSuratDiterima, func(_ context.Context, e port.Event) error {
		received <- e
		return nil
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	if err := bus.Publish(context.Background(), port.Event{
		Name:    eventSuratDiterima,
		Payload: suratDiterima{NomorSurat: "004/IN/2025"},
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	select {
	case got := <-received:
		sd, ok := got.Payload.(suratDiterima)
		if !ok {
			t.Fatalf("tipe payload salah: %T", got.Payload)
		}
		if sd.NomorSurat != "004/IN/2025" {
			t.Errorf("payload tidak utuh: %q", sd.NomorSurat)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}
