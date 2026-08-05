// Package eventbus adalah driven adapter yang mengimplementasi port.EventPublisher
// dan port.EventSubscriber. Bus memvalidasi schema event lalu mendelegasikan
// transport ke driver yang dipilih (memory untuk test; NATS/Redis menyusul).
package eventbus

import (
	"context"

	"github.com/huda-salam/pamong/infra/eventbus/drivers"
	"github.com/huda-salam/pamong/port"
)

// Driver adalah transport yang mengantar event ke handler. Bus tetap agnostik
// terhadap implementasi (memory/NATS/Redis) — validasi schema ada di Bus, bukan
// driver, sehingga setiap driver baru otomatis ter-cover schema check yang sama
// (titik ekstensi #1, registry pattern).
type Driver interface {
	Subscribe(event string, handler port.EventHandler) error
	Dispatch(ctx context.Context, event port.Event) error
}

// Drainer adalah kemampuan OPSIONAL sebuah Driver: menutup transport secara graceful dengan
// menunggu handler yang sedang berjalan selesai. Sengaja bukan bagian dari Driver — driver
// in-process (memory) mengantar sinkron di goroutine pemanggil, jadi baginya tak ada apa pun
// yang perlu dikuras dan memaksanya mengimplementasi metode kosong hanya menambah kebisingan.
type Drainer interface {
	Drain() error
}

// Bus menggabungkan SchemaRegistry dengan sebuah Driver. Ia satu-satunya pintu
// publish: event tanpa schema atau payload tak sesuai ditolak sebelum menyentuh
// transport (PRD eventbus F2).
type Bus struct {
	schema *SchemaRegistry
	driver Driver
}

var (
	_ port.EventPublisher  = (*Bus)(nil)
	_ port.EventSubscriber = (*Bus)(nil)
)

// New membuat Bus dari registry schema dan driver transport.
func New(schema *SchemaRegistry, driver Driver) *Bus {
	return &Bus{schema: schema, driver: driver}
}

// NewMemory adalah jalan pintas membuat Bus berdriver memory (test). Schema tetap
// wajib didaftarkan lewat Schema().Register sebelum publish.
func NewMemory() *Bus {
	return New(NewSchemaRegistry(), drivers.NewMemory())
}

// Schema mengembalikan registry agar pemanggil bisa mendaftarkan event (biasanya
// dari EventManifest.Produces saat wiring modul).
func (b *Bus) Schema() *SchemaRegistry { return b.schema }

// Publish memvalidasi event terhadap schema registry lalu mengantarnya via driver.
// Event tak terdaftar atau payload tipe salah → ditolak, tidak ada dispatch.
func (b *Bus) Publish(ctx context.Context, event port.Event) error {
	if err := b.schema.Validate(event.Name, event.Payload); err != nil {
		return err
	}
	return b.driver.Dispatch(ctx, event)
}

// Subscribe mendaftarkan handler untuk satu event lewat driver.
func (b *Bus) Subscribe(event string, handler port.EventHandler) error {
	return b.driver.Subscribe(event, handler)
}

// Drain menutup transport secara graceful bila driver-nya mendukung (lihat Drainer); driver
// tanpa dukungan = no-op sukses.
//
// WAJIB dipanggil saat shutdown aplikasi bila ada subscriber terdaftar, SESUDAH server HTTP
// berhenti menerima request dan SEBELUM pool DB ditutup. Tanpa itu, handler yang sedang
// berjalan kehilangan koneksi DB di tengah jalan dan pesan yang sudah ter-buffer di klien
// dibuang — pada NATS Core keduanya berarti kehilangan permanen (tak ada re-delivery).
func (b *Bus) Drain() error {
	if d, ok := b.driver.(Drainer); ok {
		return d.Drain()
	}
	return nil
}
