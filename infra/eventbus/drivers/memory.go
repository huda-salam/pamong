// Package drivers berisi implementasi transport event bus (memory, dan kelak
// NATS/Redis Streams). Driver hanya mengantar event ke handler; validasi schema
// dilakukan Bus sebelum dispatch agar seragam lintas driver.
package drivers

import (
	"context"
	"errors"
	"sync"

	"github.com/huda-salam/pamong/port"
)

// Memory adalah driver in-process untuk test cepat tanpa infra (PRD eventbus,
// "memory hanya untuk test"). Dispatch SINKRON: Publish memanggil tiap handler
// langsung dan menggabungkan error-nya, sehingga test deterministik dan kegagalan
// handler mudah di-assert. Bukan untuk produksi — tidak ada durability/retry.
type Memory struct {
	mu       sync.RWMutex
	handlers map[string][]port.EventHandler
}

// NewMemory membuat driver memory kosong.
func NewMemory() *Memory {
	return &Memory{handlers: make(map[string][]port.EventHandler)}
}

// Subscribe mendaftarkan handler untuk satu nama event. Boleh banyak handler per event.
func (m *Memory) Subscribe(event string, handler port.EventHandler) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handlers[event] = append(m.handlers[event], handler)
	return nil
}

// Dispatch mengirim event ke semua handler yang ter-subscribe ke event.Name secara
// sinkron. Error dari handler digabung (errors.Join) dan dikembalikan; tanpa
// subscriber adalah no-op (nil). Validasi schema sudah dilakukan pemanggil (Bus).
func (m *Memory) Dispatch(ctx context.Context, event port.Event) error {
	m.mu.RLock()
	hs := m.handlers[event.Name]
	dst := make([]port.EventHandler, len(hs))
	copy(dst, hs)
	m.mu.RUnlock()

	var errs []error
	for _, h := range dst {
		if err := h(ctx, event); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
