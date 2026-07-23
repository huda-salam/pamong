package notification

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

// DeliveryStatus adalah status satu upaya pengiriman lewat satu channel (F4).
type DeliveryStatus string

const (
	// StatusDelivered: channel menerima & memproses pesan (mis. email diterima provider,
	// item masuk inbox in-app). BUKAN jaminan "sudah dibaca".
	StatusDelivered DeliveryStatus = "delivered"
	// StatusFailed: pengiriman gagal (alamat kosong, provider menolak, dst). Error tercatat.
	StatusFailed DeliveryStatus = "failed"
	// StatusRead: penerima membaca notifikasi (in-app). Ditandai belakangan oleh use case baca.
	StatusRead DeliveryStatus = "read"
)

// DeliveryRecord adalah jejak satu upaya kirim lewat satu channel — untuk audit & introspeksi
// "kenapa notif tak sampai". Satu Notification multi-channel menghasilkan satu record per channel.
type DeliveryRecord struct {
	ID          string
	TenantID    string
	PersonID    uuid.UUID
	Channel     string
	TemplateKey string
	Status      DeliveryStatus
	Error       string // kosong bila sukses; pesan aman (tanpa detail provider mentah)
	At          time.Time
}

// InAppItem adalah satu notifikasi di kotak masuk in-app penerima.
type InAppItem struct {
	ID          string
	TenantID    string
	PersonID    uuid.UUID
	TemplateKey string
	Subject     string
	Body        string
	Read        bool
	CreatedAt   time.Time
}

// MemoryDeliveryRecorder adalah DeliveryRecorder in-memory untuk test/bootstrap. Aman untuk
// akses konkuren (Hub bisa dipanggil paralel).
type MemoryDeliveryRecorder struct {
	mu      sync.Mutex
	records []DeliveryRecord
}

// NewMemoryDeliveryRecorder membuat recorder kosong.
func NewMemoryDeliveryRecorder() *MemoryDeliveryRecorder {
	return &MemoryDeliveryRecorder{}
}

var _ DeliveryRecorder = (*MemoryDeliveryRecorder)(nil)

// Record menyimpan record; mengisi ID & At bila kosong.
func (r *MemoryDeliveryRecorder) Record(_ context.Context, rec DeliveryRecord) error {
	if rec.ID == "" {
		rec.ID = uuid.NewString()
	}
	if rec.At.IsZero() {
		rec.At = time.Now()
	}
	r.mu.Lock()
	r.records = append(r.records, rec)
	r.mu.Unlock()
	return nil
}

// Records mengembalikan salinan seluruh record — untuk assertion di test.
func (r *MemoryDeliveryRecorder) Records() []DeliveryRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]DeliveryRecord, len(r.records))
	copy(out, r.records)
	return out
}

// MemoryInAppInbox adalah InAppInbox in-memory untuk test/bootstrap.
type MemoryInAppInbox struct {
	mu    sync.Mutex
	items []InAppItem
}

// NewMemoryInAppInbox membuat inbox kosong.
func NewMemoryInAppInbox() *MemoryInAppInbox {
	return &MemoryInAppInbox{}
}

var _ InAppInbox = (*MemoryInAppInbox)(nil)

// Append menambah item; mengisi ID & CreatedAt bila kosong, mengembalikan ID.
func (b *MemoryInAppInbox) Append(_ context.Context, item InAppItem) (string, error) {
	if item.ID == "" {
		item.ID = uuid.NewString()
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = time.Now()
	}
	b.mu.Lock()
	b.items = append(b.items, item)
	b.mu.Unlock()
	return item.ID, nil
}

// List mengembalikan item milik (tenant, person), terbaru dulu, maksimal limit (0 = semua).
func (b *MemoryInAppInbox) List(_ context.Context, tenantID, personID string, limit int) ([]InAppItem, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []InAppItem
	// iterasi mundur = terbaru dulu
	for i := len(b.items) - 1; i >= 0; i-- {
		it := b.items[i]
		if it.TenantID == tenantID && it.PersonID.String() == personID {
			out = append(out, it)
			if limit > 0 && len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}
