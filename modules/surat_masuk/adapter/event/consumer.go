// Package event berisi consumer event modul lain (driven adapter).
// Contoh: bereaksi terhadap mutasi pegawai untuk arah-balik dependency yang asinkron
// (event + read projection), bukan import langsung (memutus ketergantungan sirkular).
package event

import (
	"context"

	"github.com/huda-salam/pamong/port"
)

// Consumer menangani event yang di-subscribe modul (didaftarkan di manifest Consumes).
type Consumer struct{}

func NewConsumer() *Consumer { return &Consumer{} }

// OnPegawaiMutasi dipanggil saat kepegawaian memublikasikan event mutasi.
// Di sini modul dapat memperbarui proyeksi lokal (mis. cache jabatan untuk routing
// disposisi) tanpa pernah meng-query DB kepegawaian secara langsung.
func (c *Consumer) OnPegawaiMutasi(ctx context.Context, e port.Event) error {
	// Implementasi proyeksi/cache lokal di sini. Idempoten: event bisa terkirim >1x.
	return nil
}
