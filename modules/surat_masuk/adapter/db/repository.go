// Package db berisi driven adapter persistensi untuk surat_masuk (Postgres/pgx).
// Implementasi port domain.SuratRepository. Domain tidak tahu file ini ada.
package db

import (
	"context"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/infra/db"
	"github.com/huda-salam/pamong/modules/surat_masuk/domain"
)

// Compile-time assertion: adapter memenuhi kontrak port.
var _ domain.SuratRepository = (*SuratRepo)(nil)

// SuratRepo mengakses tabel surat_masuk.surat_masuks pada DB tenant aktif.
// Koneksi tenant disediakan oleh db.Conn (tenant resolver) — adapter tidak memilih DB.
type SuratRepo struct {
	conn db.Conn
}

func NewSuratRepo(conn db.Conn) *SuratRepo { return &SuratRepo{conn: conn} }

func (r *SuratRepo) FindByID(ctx context.Context, id uuid.UUID) (*domain.SuratMasuk, error) {
	// Soft-delete filter default. Tidak ada JOIN lintas-schema modul lain
	// (linter: no-cross-schema-join).
	const q = `SELECT id, nomor_agenda, nomor_surat, tanggal_surat, tanggal_agenda,
	                  pengirim, perihal, sifat, status, version
	           FROM surat_masuk.surat_masuks
	           WHERE id = $1 AND deleted_at IS NULL`
	row := r.conn.QueryRow(ctx, q, id)

	var s domain.SuratMasuk
	if err := row.Scan(&s.ID, &s.NomorAgenda, &s.NomorSurat, &s.TanggalSurat,
		&s.TanggalAgenda, &s.Pengirim, &s.Perihal, &s.Sifat, &s.Status, &s.Version); err != nil {
		if db.IsNoRows(err) {
			return nil, core.ErrNotFound("SuratMasuk", id.String())
		}
		return nil, err
	}
	return &s, nil
}

func (r *SuratRepo) Save(ctx context.Context, s *domain.SuratMasuk) error {
	const q = `INSERT INTO surat_masuk.surat_masuks
	    (id, nomor_agenda, nomor_surat, tanggal_surat, tanggal_agenda,
	     pengirim, perihal, sifat, status, version)
	    VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9, 1)`
	_, err := r.conn.Exec(ctx, q, s.ID, s.NomorAgenda, s.NomorSurat, s.TanggalSurat,
		s.TanggalAgenda, s.Pengirim, s.Perihal, s.Sifat, s.Status)
	return err
}

func (r *SuratRepo) Update(ctx context.Context, s *domain.SuratMasuk) error {
	// Optimistic locking: update hanya bila version cocok; 0 baris -> konflik.
	const q = `UPDATE surat_masuk.surat_masuks
	           SET nomor_surat=$2, perihal=$3, sifat=$4, status=$5, version=version+1
	           WHERE id=$1 AND version=$6 AND deleted_at IS NULL`
	tag, err := r.conn.Exec(ctx, q, s.ID, s.NomorSurat, s.Perihal, s.Sifat, s.Status, s.Version)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return core.ErrConflict("surat telah diubah pihak lain (version mismatch)")
	}
	return nil
}
