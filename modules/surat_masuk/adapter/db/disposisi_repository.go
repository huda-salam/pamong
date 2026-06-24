package db

import (
	"context"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/infra/db"
	"github.com/huda-salam/pamong/modules/surat_masuk/domain"
)

var _ domain.DisposisiRepository = (*DisposisiRepo)(nil)

// DisposisiRepo mengakses tabel surat_masuk.disposisis pada DB tenant aktif.
type DisposisiRepo struct {
	conn db.Conn
}

func NewDisposisiRepo(conn db.Conn) *DisposisiRepo { return &DisposisiRepo{conn: conn} }

func (r *DisposisiRepo) Save(ctx context.Context, d *domain.Disposisi) error {
	const q = `INSERT INTO surat_masuk.disposisis
	    (id, surat_id, dari_jabatan, kepada_jabatan, instruksi, tanggal)
	    VALUES ($1,$2,$3,$4,$5,$6)`
	_, err := r.conn.Exec(ctx, q, d.ID, d.SuratID, d.DariJabatan, d.KepadaJabatan, d.Instruksi, d.Tanggal)
	return err
}

func (r *DisposisiRepo) ListBySurat(ctx context.Context, suratID uuid.UUID) ([]*domain.Disposisi, error) {
	const q = `SELECT id, surat_id, dari_jabatan, kepada_jabatan, instruksi, tanggal
	           FROM surat_masuk.disposisis
	           WHERE surat_id = $1
	           ORDER BY tanggal ASC`
	rows, err := r.conn.Query(ctx, q, suratID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var hasil []*domain.Disposisi
	for rows.Next() {
		var d domain.Disposisi
		if err := rows.Scan(&d.ID, &d.SuratID, &d.DariJabatan, &d.KepadaJabatan, &d.Instruksi, &d.Tanggal); err != nil {
			return nil, err
		}
		hasil = append(hasil, &d)
	}
	return hasil, rows.Err()
}
