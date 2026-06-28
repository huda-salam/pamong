package eventbus

import (
	"fmt"

	"github.com/huda-salam/pamong/core/config"
	"github.com/huda-salam/pamong/infra/eventbus/drivers"
	"github.com/nats-io/nats.go"
)

// NewFromConfig membuat Bus siap pakai dari konfigurasi. Driver dipilih
// berdasarkan cfg.Driver (nats|redis|memory). Schema registry diterima dari
// caller agar modul bisa mendaftarkan schema event sebelum Bus digunakan.
//
// Untuk menambah driver baru: tambahkan case di newDriver dan implementasikan
// Driver interface di file tersendiri (titik ekstensi #1, registry pattern).
func NewFromConfig(cfg config.EventBusConfig, schema *SchemaRegistry) (*Bus, error) {
	driver, err := newDriver(cfg, schema)
	if err != nil {
		return nil, err
	}
	return New(schema, driver), nil
}

// newDriver memilih dan membuat implementasi Driver sesuai cfg.Driver.
func newDriver(cfg config.EventBusConfig, schema *SchemaRegistry) (Driver, error) {
	switch cfg.Driver {
	case "nats":
		if cfg.URL == "" {
			return nil, fmt.Errorf("eventbus: eventbus.url wajib diisi untuk driver nats")
		}
		nc, err := nats.Connect(cfg.URL)
		if err != nil {
			return nil, fmt.Errorf("eventbus: koneksi NATS ke %q gagal: %w", cfg.URL, err)
		}
		return NewNATSDriver(nc, schema), nil

	case "redis":
		// DEFERRED(PR-3.1.x): Redis Streams driver.
		return nil, fmt.Errorf("eventbus: driver %q belum tersedia", cfg.Driver)

	case "memory", "":
		return drivers.NewMemory(), nil

	default:
		return nil, fmt.Errorf("eventbus: driver tidak dikenal: %q (pilihan: nats|redis|memory)", cfg.Driver)
	}
}
