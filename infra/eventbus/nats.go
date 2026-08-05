package eventbus

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/huda-salam/pamong/port"
	"github.com/nats-io/nats.go"
)

// Batas tunggu pengurasan saat shutdown. Sengaja lebih pendek dari timeout shutdown HTTP
// (15 detik di cmd/server) agar drain yang macet tetap menyisakan waktu bagi sisa penutupan,
// dan bukan berubah menjadi proses yang menggantung sampai dibunuh orchestrator.
const (
	drainTimeout      = 10 * time.Second
	drainPollInterval = 20 * time.Millisecond
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
// per-message. NATS Core tidak bisa re-deliver pesan; error handler di-log structured
// untuk observability. Guaranteed delivery bergantung pada outbox at-least-once
// (OutboxRelay yang dispatch via driver ini setelah commit transaksi bisnis).
// Deserialisasi payload memakai schema.Unmarshal sehingga handler menerima struct
// konkret bertipe sama dengan yang didaftarkan di SchemaRegistry.
//
// Subscribe BLOKIR sampai server NATS mengonfirmasi subscription (Flush). Ini bukan
// kehati-hatian berlebih: nc.Subscribe hanya menaruh protokol SUB di buffer tulis klien,
// dan NATS Core MEMBUANG pesan yang tiba sebelum subscription tercatat di server — tanpa
// re-delivery. Tanpa Flush, event yang di-dispatch pada jendela antara Bootstrap dan
// pencatatan SUB hilang permanen, sementara OutboxRelay sudah menandainya terkirim.
// Biaya blokirnya satu round-trip saat boot (batas 10 detik bawaan klien).
func (d *NATSDriver) Subscribe(event string, handler port.EventHandler) error {
	sub, err := d.nc.Subscribe(event, func(msg *nats.Msg) {
		ev, err := unmarshalEvent(msg.Data, event, d.schema)
		if err != nil {
			// Schema belum terdaftar atau payload corrupt — abaikan message.
			return
		}
		if err := handler(context.Background(), ev); err != nil {
			// NATS Core tidak bisa re-deliver — log untuk observability.
			// Re-delivery bergantung pada outbox at-least-once (via OutboxRelay).
			slog.Error("NATS subscriber handler gagal",
				"event", event,
				"err", err,
			)
		}
	})
	if err != nil {
		return err
	}
	if err := d.nc.Flush(); err != nil {
		// Subscription sudah terdaftar di klien tapi belum tentu di server: lepaskan supaya
		// tak ada handler menggantung yang mengira dirinya aktif, lalu laporkan kegagalannya
		// (bootstrap gagal lebih baik daripada subscriber diam-diam tuli).
		_ = sub.Unsubscribe()
		return fmt.Errorf("eventbus: konfirmasi subscribe %q ke server NATS gagal: %w", event, err)
	}
	d.mu.Lock()
	d.subs = append(d.subs, sub)
	d.mu.Unlock()
	return nil
}

// Drain menguras semua subscription secara graceful: menunggu message in-flight
// selesai diproses sebelum koneksi ditutup. Dipanggil saat shutdown aplikasi.
//
// `nats.Conn.Drain()` sendiri ASINKRON — ia hanya menaruh koneksi ke state draining lalu
// kembali seketika; pengurasan berjalan di goroutine klien dan koneksi baru tertutup setelah
// selesai. Mengembalikannya apa adanya berarti pemanggil mengira sudah aman padahal handler
// masih berjalan, dan proses yang langsung keluar (atau menutup pool DB) memotongnya di tengah.
// Karena itu di sini ditunggu sampai koneksi benar-benar tertutup, dengan batas waktu supaya
// shutdown tak menggantung selamanya bila satu handler tak kunjung selesai.
func (d *NATSDriver) Drain() error {
	if err := d.nc.Drain(); err != nil {
		return fmt.Errorf("eventbus: mulai drain NATS: %w", err)
	}
	deadline := time.Now().Add(drainTimeout)
	for !d.nc.IsClosed() {
		if time.Now().After(deadline) {
			return fmt.Errorf("eventbus: drain NATS tak selesai dalam %s — handler in-flight ditinggalkan", drainTimeout)
		}
		time.Sleep(drainPollInterval)
	}
	return nil
}
