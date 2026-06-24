package domain

import (
	"context"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/port"
)

// SuratRepository adalah port persistensi surat. Interface didefinisikan di domain;
// implementasi konkret (Postgres) ada di adapter/db. Domain tidak tahu Postgres.
type SuratRepository interface {
	FindByID(ctx context.Context, id uuid.UUID) (*SuratMasuk, error)
	Save(ctx context.Context, s *SuratMasuk) error
	Update(ctx context.Context, s *SuratMasuk) error // cek optimistic lock di adapter
}

// DisposisiRepository port untuk disposisi.
type DisposisiRepository interface {
	Save(ctx context.Context, d *Disposisi) error
	ListBySurat(ctx context.Context, suratID uuid.UUID) ([]*Disposisi, error)
}

// Dependency ke modul lain dideklarasikan sebagai alias port framework — BUKAN import
// package kepegawaian. Saat ini in-process; bila kepegawaian di-extract jadi service,
// cukup ganti adapter, use case tak berubah (CODING_PHILOSOPHY #2 & #7).
type PegawaiResolver = port.UserResolver
