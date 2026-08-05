package eventbus_test

import (
	"context"
	"errors"
	"testing"

	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/infra/eventbus"
	"github.com/huda-salam/pamong/port"
)

// payload contoh untuk schema registry.
type suratDiterima struct {
	NomorSurat string
}

type lainPayload struct {
	Foo int
}

const eventSuratDiterima = "surat_masuk.surat.diterima"

func newBusWithSchema(t *testing.T) *eventbus.Bus {
	t.Helper()
	bus := eventbus.NewMemory()
	if err := bus.Schema().Register(eventSuratDiterima, suratDiterima{}); err != nil {
		t.Fatalf("register schema: %v", err)
	}
	return bus
}

func ctx() context.Context { return context.Background() }

func TestPublish_DispatchKeSubscriber(t *testing.T) {
	bus := newBusWithSchema(t)

	var got []port.Event
	err := bus.Subscribe(eventSuratDiterima, func(_ context.Context, e port.Event) error {
		got = append(got, e)
		return nil
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	want := port.Event{Name: eventSuratDiterima, Payload: suratDiterima{NomorSurat: "001/IN/2025"}}
	if err := bus.Publish(ctx(), want); err != nil {
		t.Fatalf("publish: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("handler dipanggil %d kali, mau 1", len(got))
	}
	if got[0].Payload.(suratDiterima).NomorSurat != "001/IN/2025" {
		t.Errorf("payload tidak utuh sampai handler: %+v", got[0].Payload)
	}
}

func TestPublish_MultiSubscriber(t *testing.T) {
	bus := newBusWithSchema(t)

	var a, b int
	_ = bus.Subscribe(eventSuratDiterima, func(_ context.Context, _ port.Event) error { a++; return nil })
	_ = bus.Subscribe(eventSuratDiterima, func(_ context.Context, _ port.Event) error { b++; return nil })

	if err := bus.Publish(ctx(), port.Event{Name: eventSuratDiterima, Payload: suratDiterima{}}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if a != 1 || b != 1 {
		t.Errorf("kedua handler harus terpanggil sekali, dapat a=%d b=%d", a, b)
	}
}

func TestPublish_EventTanpaSchemaDitolak(t *testing.T) {
	bus := eventbus.NewMemory() // tidak mendaftarkan schema apa pun

	called := false
	_ = bus.Subscribe(eventSuratDiterima, func(_ context.Context, _ port.Event) error { called = true; return nil })

	err := bus.Publish(ctx(), port.Event{Name: eventSuratDiterima, Payload: suratDiterima{}})
	if err == nil {
		t.Fatal("publish event tak terdaftar harus ditolak")
	}
	assertValidationError(t, err)
	if called {
		t.Error("handler tidak boleh dipanggil saat event ditolak")
	}
}

func TestPublish_PayloadTipeSalahDitolak(t *testing.T) {
	bus := newBusWithSchema(t)

	err := bus.Publish(ctx(), port.Event{Name: eventSuratDiterima, Payload: lainPayload{Foo: 1}})
	if err == nil {
		t.Fatal("payload tipe salah harus ditolak")
	}
	assertValidationError(t, err)
}

func TestPublish_PointerPayloadSetaraValue(t *testing.T) {
	bus := newBusWithSchema(t)
	// schema didaftarkan sebagai value; publish pakai pointer harus tetap lolos.
	if err := bus.Publish(ctx(), port.Event{Name: eventSuratDiterima, Payload: &suratDiterima{}}); err != nil {
		t.Errorf("pointer payload harus setara value: %v", err)
	}
}

func TestPublish_TanpaSubscriberNoOp(t *testing.T) {
	bus := newBusWithSchema(t)
	if err := bus.Publish(ctx(), port.Event{Name: eventSuratDiterima, Payload: suratDiterima{}}); err != nil {
		t.Errorf("publish tanpa subscriber harus no-op, dapat: %v", err)
	}
}

func TestPublish_ErrorHandlerDikembalikan(t *testing.T) {
	bus := newBusWithSchema(t)
	sentinel := errors.New("handler gagal")
	_ = bus.Subscribe(eventSuratDiterima, func(_ context.Context, _ port.Event) error { return sentinel })

	err := bus.Publish(ctx(), port.Event{Name: eventSuratDiterima, Payload: suratDiterima{}})
	if !errors.Is(err, sentinel) {
		t.Errorf("error handler harus dikembalikan publish, dapat: %v", err)
	}
}

func assertValidationError(t *testing.T, err error) {
	t.Helper()
	var fe *core.FrameworkError
	if !errors.As(err, &fe) || fe.Code != "VALIDATION_ERROR" {
		t.Errorf("mau VALIDATION_ERROR, dapat: %v", err)
	}
}

// drainerPalsu adalah Driver yang sekaligus Drainer — untuk membuktikan Bus meneruskan
// Drain ke driver yang mendukungnya, tanpa perlu koneksi NATS.
type drainerPalsu struct {
	dipanggil int
	err       error
}

func (d *drainerPalsu) Subscribe(string, port.EventHandler) error  { return nil }
func (d *drainerPalsu) Dispatch(context.Context, port.Event) error { return nil }
func (d *drainerPalsu) Drain() error                               { d.dipanggil++; return d.err }

// TestBusDrain_DiteruskanKeDriver: driver ber-Drainer harus benar-benar dikuras, dan errornya
// diteruskan ke pemanggil (shutdown perlu tahu bahwa ada handler yang ditinggalkan).
func TestBusDrain_DiteruskanKeDriver(t *testing.T) {
	d := &drainerPalsu{}
	bus := eventbus.New(eventbus.NewSchemaRegistry(), d)
	if err := bus.Drain(); err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if d.dipanggil != 1 {
		t.Fatalf("Drain driver dipanggil %d kali, mau 1", d.dipanggil)
	}

	d.err = errors.New("drain gagal")
	if err := bus.Drain(); err == nil {
		t.Fatal("error drain driver harus diteruskan, bukan ditelan")
	}
}

// TestBusDrain_DriverTanpaDrainerNoop: driver memory mengantar sinkron di goroutine pemanggil —
// tak ada yang perlu dikuras, dan Drain tak boleh gagal hanya karena driver tak mendukungnya
// (kalau tidak, jalur shutdown dev akan selalu melaporkan error palsu).
func TestBusDrain_DriverTanpaDrainerNoop(t *testing.T) {
	if err := eventbus.NewMemory().Drain(); err != nil {
		t.Fatalf("Drain driver tanpa Drainer harus no-op sukses, dapat %v", err)
	}
}
