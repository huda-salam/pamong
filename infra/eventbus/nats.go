package eventbus

import (
	"context"
	"sync"

	"github.com/huda-salam/pamong/port"
	"github.com/nats-io/nats.go"
)

// NATSDriver adalah Driver yang menggunakan NATS Core sebagai transport. Volatile:
// tidak ada persistence atau ack — subscriber yang tidak terhubung saat publish
// tidak menerima event. Untuk guaranteed delivery, pakai OutboxStore+OutboxRelay
// yang mengantarkan event via driver ini setelah commit transaksi bisnis.
//
// Subject NATS = event.Name (format modul.entity.kejadian — cocok dengan wildcard
// NATS seperti "surat_masuk.>" bila dibutuhkan kelak).
//
// Serialisasi lewat marshalEvent/unmarshalEvent (wire.go): JSON dengan schema-
// guided deserialisasi sehingga subscriber menerima struct konkret, bukan
// map[string]any.
type NATSDriver struct {
	nc     *nats.Conn
	schema *SchemaRegistry
	subs   []*nats.Subscription
	mu     sync.Mutex
}

var _ Driver = (*NATSDriver)(nil)

// NewNATSDriver membuat NATSDriver dari koneksi NATS yang sudah dibuka dan
// schema registry. Caller bertanggung jawab membuka dan menutup nc; driver
// tidak mengambil kepemilikan koneksi.
func NewNATSDriver(nc *nats.Conn, schema *SchemaRegistry) *NATSDriver {
	return &NATSDriver{nc: nc, schema: schema}
}

// Dispatch menyerialisasi event ke JSON lalu mempublikasikannya ke NATS subject
// yang sesuai event.Name. Bila ctx sudah dibatalkan, publish dilewati.
func (d *NATSDriver) Dispatch(ctx context.Context, event port.Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := marshalEvent(event)
	if err != nil {
		return err
	}
	return d.nc.Publish(event.Name, data)
}

// Subscribe mendaftarkan handler ke NATS subject yang sesuai nama event. Handler
// dipanggil dengan context.Background() karena NATS Core tidak membawa context
// per-message. Error dari handler di-swallow (DEFERRED PR-3.1.4: DLQ/retry).
// Deserialisasi payload memakai schema.Unmarshal sehingga handler menerima struct
// konkret bertipe sama dengan yang didaftarkan di SchemaRegistry.
func (d *NATSDriver) Subscribe(event string, handler port.EventHandler) error {
	sub, err := d.nc.Subscribe(event, func(msg *nats.Msg) {
		ev, err := unmarshalEvent(msg.Data, event, d.schema)
		if err != nil {
			// Schema belum terdaftar atau payload corrupt — abaikan message.
			return
		}
		_ = handler(context.Background(), ev)
	})
	if err != nil {
		return err
	}
	d.mu.Lock()
	d.subs = append(d.subs, sub)
	d.mu.Unlock()
	return nil
}

// Drain menguras semua subscription secara graceful: menunggu message in-flight
// selesai diproses sebelum koneksi ditutup. Dipanggil saat shutdown aplikasi.
func (d *NATSDriver) Drain() error {
	return d.nc.Drain()
}
