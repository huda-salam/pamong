package crypto

import (
	"context"
	"errors"
	"fmt"

	"github.com/huda-salam/pamong/port"
	"github.com/jackc/pgx/v5"
)

// isNoRows sengaja memakai pgx langsung alih-alih infra/db.IsNoRows: PR-3.8.3 akan membuat
// infra/db memanggil kripto (enkripsi transparan di lapis repository), jadi ketergantungan
// infra/crypto → infra/db akan menjadi siklus. Adapter memang boleh menyentuh pgx.
func isNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

// DEKRecord adalah satu baris id.data_keys: DEK yang SUDAH ter-wrap beserta metadata untuk
// membukanya kembali. Tidak pernah memuat kunci mentah.
type DEKRecord struct {
	Version   int
	Wrapped   []byte
	Custody   Custody
	KEKDriver string
}

// DEKStore menyimpan DEK ter-wrap. Implementasi produksi menulis ke id.data_keys di IDENTITY
// DB (sentral), bukan tenant DB: dump satu tenant DB harus berisi ciphertext saja, tanpa
// kunci apa pun untuk membukanya (ADR-010 §2).
type DEKStore interface {
	// Active mengembalikan versi kunci yang dipakai untuk MENULIS. found=false bila tenant
	// belum punya kunci untuk (purpose, kind) — pemanggil yang memutuskan membuatnya.
	Active(ctx context.Context, ref KeyRef) (rec DEKRecord, found bool, err error)
	// ByVersion mengambil versi tertentu — dipakai saat MEMBACA ciphertext lama setelah
	// rotasi (ciphertext membawa versinya sendiri).
	ByVersion(ctx context.Context, ref KeyRef, version int) (rec DEKRecord, found bool, err error)
	// InsertActive menyimpan versi baru sebagai versi aktif. Aman terhadap balapan: bila
	// proses lain sudah membuat versi aktif lebih dulu, baris yang MENANG-lah yang
	// dikembalikan (DEK pemanggil yang kalah dibuang, tak pernah dipakai) sehingga dua
	// proses tak pernah menulis dengan kunci berbeda.
	InsertActive(ctx context.Context, ref KeyRef, rec DEKRecord) (DEKRecord, error)
}

// ErrDEKMissing dikembalikan saat ciphertext merujuk versi kunci yang tak ada di store —
// mis. baris data_keys terhapus. Data tak bisa didekripsi; ini kondisi operasional serius,
// bukan sekadar "tidak ditemukan".
var ErrDEKMissing = errors.New("crypto: DEK untuk versi kunci ini tidak ada di store")

// DBDEKStore adalah DEKStore di atas id.data_keys (identity DB).
//
// conn WAJIB koneksi identity DB (sentral). Menyuntik koneksi tenant DB ke sini membatalkan
// alasan tabel ini sentral — tabelnya pun tak ada di tenant DB, jadi kesalahan itu gagal
// cepat saat query pertama.
type DBDEKStore struct {
	conn port.DBConn
}

func NewDBDEKStore(identityConn port.DBConn) *DBDEKStore { return &DBDEKStore{conn: identityConn} }

const dekCols = `key_version, wrapped_dek, custody, kek_driver`

func (s *DBDEKStore) Active(ctx context.Context, ref KeyRef) (DEKRecord, bool, error) {
	row := s.conn.QueryRow(ctx, `SELECT `+dekCols+` FROM id.data_keys
		WHERE tenant_id = $1 AND purpose = $2 AND kind = $3 AND is_active`,
		ref.TenantID, ref.Purpose, string(ref.Kind))
	return scanDEK(row, ref)
}

func (s *DBDEKStore) ByVersion(ctx context.Context, ref KeyRef, version int) (DEKRecord, bool, error) {
	row := s.conn.QueryRow(ctx, `SELECT `+dekCols+` FROM id.data_keys
		WHERE tenant_id = $1 AND purpose = $2 AND kind = $3 AND key_version = $4`,
		ref.TenantID, ref.Purpose, string(ref.Kind), version)
	return scanDEK(row, ref)
}

func (s *DBDEKStore) InsertActive(ctx context.Context, ref KeyRef, rec DEKRecord) (DEKRecord, error) {
	// ON CONFLICT DO NOTHING menangkap DUA benturan sekaligus: primary key (versi sama) dan
	// unique index parsial uq_data_keys_active (sudah ada versi aktif lain). Tanpa target
	// kolom karena keduanya harus diperlakukan sama: pemanggil kalah balapan → pakai yang ada.
	row := s.conn.QueryRow(ctx, `INSERT INTO id.data_keys
		(tenant_id, purpose, kind, key_version, wrapped_dek, kek_driver, custody, is_active)
		VALUES ($1, $2, $3, $4, $5, $6, $7, true)
		ON CONFLICT DO NOTHING
		RETURNING `+dekCols,
		ref.TenantID, ref.Purpose, string(ref.Kind), rec.Version, rec.Wrapped, rec.KEKDriver, string(rec.Custody))

	inserted, found, err := scanDEK(row, ref)
	if err != nil {
		return DEKRecord{}, err
	}
	if found {
		return inserted, nil
	}

	// Kalah balapan: baca versi aktif yang menang.
	existing, found, err := s.Active(ctx, ref)
	if err != nil {
		return DEKRecord{}, err
	}
	if !found {
		// INSERT ditolak tapi tak ada versi aktif — mis. versi ini pernah ada lalu
		// dinonaktifkan. Jangan diam-diam menulis dengan kunci yang salah.
		return DEKRecord{}, fmt.Errorf("crypto: DEK %s/%s/%s versi %d ditolak tapi tak ada versi aktif",
			ref.TenantID, ref.Purpose, ref.Kind, rec.Version)
	}
	return existing, nil
}

func scanDEK(row port.Row, ref KeyRef) (DEKRecord, bool, error) {
	var (
		rec     DEKRecord
		custody string
	)
	if err := row.Scan(&rec.Version, &rec.Wrapped, &custody, &rec.KEKDriver); err != nil {
		if isNoRows(err) {
			return DEKRecord{}, false, nil
		}
		return DEKRecord{}, false, fmt.Errorf("crypto: baca DEK %s/%s/%s: %w", ref.TenantID, ref.Purpose, ref.Kind, err)
	}
	rec.Custody = Custody(custody)
	return rec, true, nil
}
