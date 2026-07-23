package notification

import (
	"context"
	"sort"
)

// Channel adalah satu jalur pengiriman notifikasi (in-app, email, push). Implementasi
// menerima pesan yang SUDAH dirender (subjek+body) plus penerima konkret, lalu mengirim.
// Channel tidak me-render template dan tidak memilih penerima — itu tugas Hub.
//
// Send mengembalikan error transport (mis. *port.MessagingError untuk email). Hub yang
// menerjemahkan error itu ke DeliveryRecord berstatus gagal — channel tak menyentuh
// pelacakan agar tetap fokus satu tanggung jawab.
type Channel interface {
	// Name mengembalikan nama unik channel (mis. "in_app", "email") — kunci di registry
	// dan yang dirujuk Notification.Channels.
	Name() string
	// Send mengirim pesan ke penerima. Idempotency di level notifikasi bukan tanggung
	// jawab channel (Hub/outbox yang menjamin at-least-once).
	Send(ctx context.Context, tenantID string, r Recipient, msg RenderedMessage) error
}

// ChannelRegistry memetakan nama channel → Channel. Pola registry seragam framework (titik
// ekstensi #1): menambah channel baru = tulis implementasi Channel + daftar satu baris,
// Hub tak berubah karena bergantung pada interface, bukan implementasi konkret. Diisi saat
// bootstrap lalu dianggap immutable; Register menolak nama ganda/channel nil.
type ChannelRegistry struct {
	channels map[string]Channel
}

// NewChannelRegistry membuat registry kosong.
func NewChannelRegistry() *ChannelRegistry {
	return &ChannelRegistry{channels: make(map[string]Channel)}
}

// Register mendaftarkan channel di bawah namanya sendiri (ch.Name()). Menolak nama ganda
// (ErrChannelExists), channel nil (ErrNilChannel), dan nama kosong (ErrNilChannel) —
// ketiganya menandakan salah wiring saat bootstrap.
func (r *ChannelRegistry) Register(ch Channel) error {
	if ch == nil {
		return ErrNilChannel("")
	}
	name := ch.Name()
	if name == "" {
		return ErrNilChannel(name)
	}
	if _, exists := r.channels[name]; exists {
		return ErrChannelExists(name)
	}
	r.channels[name] = ch
	return nil
}

// Get mengembalikan channel untuk nama, atau ErrChannelNotRegistered bila tak ada —
// tanpa fallback diam-diam ke channel lain.
func (r *ChannelRegistry) Get(name string) (Channel, error) {
	ch, ok := r.channels[name]
	if !ok {
		return nil, ErrChannelNotRegistered(name)
	}
	return ch, nil
}

// Names mengembalikan seluruh nama channel terdaftar, terurut — untuk introspeksi/validasi.
func (r *ChannelRegistry) Names() []string {
	out := make([]string, 0, len(r.channels))
	for n := range r.channels {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
