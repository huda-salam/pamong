// Package sequence menyediakan driven adapter Postgres untuk port.SequenceGenerator:
// sumber nomor ber-urut ATOMIK per-tenant (nomor agenda surat, nomor SPM, dst), reset per
// tahun fiskal. Seluruh kode yang menyentuh pgx/tenant-pool HANYA ada di infra — use case
// bergantung pada port.SequenceGenerator, bukan paket ini.
package sequence

import (
	"context"
	"sync"

	coreSeq "github.com/huda-salam/pamong/core/sequence"
	"github.com/huda-salam/pamong/infra/db"
	"github.com/huda-salam/pamong/port"
)

// DBGenerator mengimplementasi port.SequenceGenerator di atas tenant DB (tabel gov.sequences).
// Pool tenant diresolusi per-request dari TenantConnManager (DB-per-tenant); skema dipastikan
// sekali per tenant per proses (pola sama dengan idempotency.DBStore).
type DBGenerator struct {
	connMgr *db.TenantConnManager

	ensuredMu sync.Mutex
	ensured   map[string]bool // tenantID → skema gov.sequences sudah dipastikan
}

// NewDBGenerator membuat generator di atas manajer koneksi tenant.
func NewDBGenerator(connMgr *db.TenantConnManager) *DBGenerator {
	return &DBGenerator{
		connMgr: connMgr,
		ensured: make(map[string]bool),
	}
}

var _ port.SequenceGenerator = (*DBGenerator)(nil)

// Next mengembalikan nomor berikutnya untuk (tenant, pattern, tahun), ter-render sesuai pola.
// Penghitung di-increment ATOMIK: satu UPDATE ... RETURNING per pemanggilan, sehingga dua
// request paralel tak pernah menerima nomor yang sama (baris dikunci oleh Postgres selama
// UPDATE). Tahun menjadi bagian kunci → tahun baru memulai dari 1 tanpa reset terpisah.
func (g *DBGenerator) Next(ctx context.Context, tenantID, pattern string, tahun int) (string, error) {
	pool, err := g.pool(ctx, tenantID)
	if err != nil {
		return "", err
	}

	// INSERT baru (nilai awal 1) atau, bila baris (name,tahun) sudah ada, increment atomik.
	// RETURNING selalu memberi nilai final yang dipegang pemanggil ini — eksklusif.
	// gov:raw-ok reason=sequence-atomic-increment query=sequence-next
	row := pool.QueryRow(ctx, `
		INSERT INTO gov.sequences AS s (name, tahun, current)
		VALUES ($1, $2, 1)
		ON CONFLICT (name, tahun) DO UPDATE
			SET current = s.current + 1, updated_at = now()
		RETURNING current`, pattern, tahun)

	var current int64
	if err := row.Scan(&current); err != nil {
		return "", err
	}
	return formatPattern(pattern, current, tahun)
}

// pool meresolusi pool tenant & memastikan skema tabel ada (sekali per tenant).
func (g *DBGenerator) pool(ctx context.Context, tenantID string) (*db.Pool, error) {
	pool, err := g.connMgr.Tenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if err := g.ensureSchema(ctx, tenantID, pool); err != nil {
		return nil, err
	}
	return pool, nil
}

// ensureSchema menerapkan migrasi tabel sequence ke tenant DB sekali per proses. Aman bila dua
// request pertama untuk tenant sama menerapkannya bersamaan: ApplyEmbeddedSchema idempoten &
// ber-advisory-lock (paling banter redundan).
func (g *DBGenerator) ensureSchema(ctx context.Context, tenantID string, pool *db.Pool) error {
	g.ensuredMu.Lock()
	done := g.ensured[tenantID]
	g.ensuredMu.Unlock()
	if done {
		return nil
	}
	if err := db.ApplyEmbeddedSchema(ctx, pool, coreSeq.MigrationModule, coreSeq.MigrationsFS); err != nil {
		return err
	}
	g.ensuredMu.Lock()
	g.ensured[tenantID] = true
	g.ensuredMu.Unlock()
	return nil
}
