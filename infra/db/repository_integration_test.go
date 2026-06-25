//go:build integration

package db_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/infra/db"
	"github.com/huda-salam/pamong/port"
	"github.com/jackc/pgx/v5/pgxpool"
)

// catatan: ASN test entity sederhana — hanya untuk menguji SQLRepository generik.
type produk struct {
	ID      uuid.UUID
	Nama    string
	Harga   int
	Version int
}

type produkMapper struct{}

func (produkMapper) Table() string         { return "test_repo.produk" }
func (produkMapper) DataColumns() []string { return []string{"nama", "harga"} }
func (produkMapper) DataValues(e *produk) []any {
	return []any{e.Nama, e.Harga}
}
func (produkMapper) Scan(s db.RowScanner) (*produk, error) {
	var p produk
	if err := s.Scan(&p.ID, &p.Nama, &p.Harga, &p.Version); err != nil {
		return nil, err
	}
	return &p, nil
}
func (produkMapper) ID(e *produk) uuid.UUID      { return e.ID }
func (produkMapper) Version(e *produk) int       { return e.Version }
func (produkMapper) SetVersion(e *produk, v int) { e.Version = v }
func (produkMapper) SearchColumns() []string     { return []string{"nama"} }

// newTestDB membuka pool ke Postgres uji dari PAMONG_TEST_DB_DSN, lalu menyiapkan
// schema + tabel uji yang dibersihkan saat test selesai. Skip bila DSN tidak diset.
func newTestRepo(t *testing.T) (*db.SQLRepository[produk], context.Context) {
	t.Helper()
	dsn := os.Getenv("PAMONG_TEST_DB_DSN")
	if dsn == "" {
		t.Skip("PAMONG_TEST_DB_DSN tidak diset — lewati integration test")
	}
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("buka pool: %v", err)
	}
	t.Cleanup(pool.Close)

	conn := db.NewPool(pool)
	stmts := []string{
		`DROP SCHEMA IF EXISTS test_repo CASCADE`,
		`CREATE SCHEMA test_repo`,
		`CREATE TABLE test_repo.produk (
			id         uuid PRIMARY KEY,
			nama       text NOT NULL,
			harga      int  NOT NULL,
			version    int  NOT NULL,
			created_at timestamptz NOT NULL,
			updated_at timestamptz NOT NULL,
			deleted_at timestamptz
		)`,
	}
	for _, s := range stmts {
		if _, err := conn.Exec(ctx, s); err != nil {
			t.Fatalf("setup schema: %v", err)
		}
	}
	t.Cleanup(func() {
		_, _ = conn.Exec(context.Background(), `DROP SCHEMA IF EXISTS test_repo CASCADE`)
	})

	repo, err := db.NewSQLRepository[produk](conn, produkMapper{})
	if err != nil {
		t.Fatalf("buat repo: %v", err)
	}
	return repo, ctx
}

func TestSQLRepository_CRUD(t *testing.T) {
	repo, ctx := newTestRepo(t)

	p := &produk{ID: uuid.New(), Nama: "Pulpen", Harga: 5000}
	if err := repo.Save(ctx, p); err != nil {
		t.Fatalf("save: %v", err)
	}
	if p.Version != 1 {
		t.Fatalf("version setelah save = %d, mau 1", p.Version)
	}

	got, err := repo.FindByID(ctx, p.ID)
	if err != nil {
		t.Fatalf("findByID: %v", err)
	}
	if got.Nama != "Pulpen" || got.Harga != 5000 || got.Version != 1 {
		t.Fatalf("hasil findByID tidak cocok: %+v", got)
	}

	got.Harga = 7500
	if err := repo.Update(ctx, got); err != nil {
		t.Fatalf("update: %v", err)
	}
	if got.Version != 2 {
		t.Fatalf("version setelah update = %d, mau 2", got.Version)
	}

	if err := repo.SoftDelete(ctx, p.ID); err != nil {
		t.Fatalf("softDelete: %v", err)
	}
	if _, err := repo.FindByID(ctx, p.ID); err == nil {
		t.Fatal("findByID setelah soft delete harus ErrNotFound")
	}
}

func TestSQLRepository_OptimisticLockConflict(t *testing.T) {
	repo, ctx := newTestRepo(t)

	p := &produk{ID: uuid.New(), Nama: "Buku", Harga: 10000}
	if err := repo.Save(ctx, p); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Dua salinan dengan version yang sama (1).
	a, _ := repo.FindByID(ctx, p.ID)
	b, _ := repo.FindByID(ctx, p.ID)

	a.Harga = 11000
	if err := repo.Update(ctx, a); err != nil {
		t.Fatalf("update pertama: %v", err)
	}

	b.Harga = 12000
	err := repo.Update(ctx, b)
	if err == nil {
		t.Fatal("update kedua dengan version usang harus konflik")
	}
	var ce *core.FrameworkError
	if !errors.As(err, &ce) || ce.Code != "CONFLICT" {
		t.Fatalf("error harus CONFLICT, dapat: %v", err)
	}
}

func TestSQLRepository_ListPaginationSearch(t *testing.T) {
	repo, ctx := newTestRepo(t)

	for _, n := range []string{"Apel", "Apricot", "Mangga", "Manggis", "Jeruk"} {
		if err := repo.Save(ctx, &produk{ID: uuid.New(), Nama: n, Harga: 1000}); err != nil {
			t.Fatalf("save %s: %v", n, err)
		}
	}

	// Pencarian "Mang" -> Mangga, Manggis.
	res, err := repo.List(ctx, port.ListFilter{Search: "Mang", Sort: "nama", Order: "asc"})
	if err != nil {
		t.Fatalf("list search: %v", err)
	}
	if res.Total != 2 || len(res.Items) != 2 {
		t.Fatalf("search Mang total=%d items=%d, mau 2/2", res.Total, len(res.Items))
	}
	if res.Items[0].Nama != "Mangga" || res.Items[1].Nama != "Manggis" {
		t.Fatalf("urutan/isi salah: %+v", res.Items)
	}

	// Pagination: 5 item, pageSize 2 -> 3 halaman.
	page1, err := repo.List(ctx, port.ListFilter{Page: 1, PageSize: 2, Sort: "nama"})
	if err != nil {
		t.Fatalf("list page1: %v", err)
	}
	if page1.Total != 5 || page1.TotalPages != 3 || len(page1.Items) != 2 {
		t.Fatalf("pagination salah: total=%d pages=%d items=%d", page1.Total, page1.TotalPages, len(page1.Items))
	}
}
