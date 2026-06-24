// Package domain (clean) — contoh lapisan domain yang patuh hexagonal.
// Tidak ada satu pun import terlarang, jadi analyzer tidak boleh melaporkan apa-apa.
package domain

import (
	"context"
	"time"

	// Library murni tanpa side-effect I/O — diperbolehkan.
	"github.com/google/uuid"
)

// SuratMasuk adalah entity domain murni.
type SuratMasuk struct {
	ID         uuid.UUID
	NomorSurat string
	Tanggal    time.Time
}

// Repository adalah PORT — interface yang didefinisikan domain dan
// diimplementasikan di lapisan adapter. Inilah cara domain "berbicara"
// ke dunia luar tanpa tahu detail teknisnya.
type Repository interface {
	FindByID(ctx context.Context, id uuid.UUID) (*SuratMasuk, error)
	Save(ctx context.Context, s *SuratMasuk) error
}

// Validate adalah business logic murni — tidak ada I/O.
func (s *SuratMasuk) Validate() error {
	if s.NomorSurat == "" {
		return ErrNomorKosong
	}
	return nil
}
