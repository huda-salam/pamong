//go:build integration

package db_test

import (
	"context"
	"os"
	"testing"

	"github.com/huda-salam/pamong/infra/db"
	"github.com/jackc/pgx/v5/pgxpool"
)

func newTestPool(t *testing.T) (*db.Pool, context.Context) {
	t.Helper()
	dsn := os.Getenv("PAMONG_TEST_DB_DSN")
	if dsn == "" {
		t.Skip("PAMONG_TEST_DB_DSN tidak diset — lewati integration test")
	}
	ctx := context.Background()
	pgpool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("buka pool: %v", err)
	}
	pool := db.NewPool(pgpool)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DROP SCHEMA IF EXISTS mig_demo CASCADE; DROP SCHEMA IF EXISTS gov CASCADE`)
		pgpool.Close()
	})
	// Bersihkan state awal agar test deterministik.
	if _, err := pool.Exec(ctx, `DROP SCHEMA IF EXISTS mig_demo CASCADE; DROP SCHEMA IF EXISTS gov CASCADE`); err != nil {
		t.Fatalf("reset: %v", err)
	}
	return pool, ctx
}

func demoMigrations() []db.Migration {
	return []db.Migration{
		{
			Module: "demo", Version: "001", Name: "create_schema",
			UpSQL:   `CREATE SCHEMA mig_demo; CREATE TABLE mig_demo.a (id int);`,
			DownSQL: `DROP TABLE mig_demo.a; DROP SCHEMA mig_demo;`,
		},
		{
			Module: "demo", Version: "002", Name: "add_table",
			UpSQL:   `CREATE TABLE mig_demo.b (id int);`,
			DownSQL: `DROP TABLE mig_demo.b;`,
		},
	}
}

func tableExists(t *testing.T, pool *db.Pool, ctx context.Context, schema, name string) bool {
	t.Helper()
	var exists bool
	err := pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema=$1 AND table_name=$2)`,
		schema, name).Scan(&exists)
	if err != nil {
		t.Fatalf("cek tabel: %v", err)
	}
	return exists
}

func TestMigrator_UpStatusDown(t *testing.T) {
	pool, ctx := newTestPool(t)
	m := db.NewMigrator(pool, demoMigrations())

	// Status awal: dua pending.
	st, err := m.Status(ctx)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if len(st) != 2 || st[0].Applied || st[1].Applied {
		t.Fatalf("status awal harus 2 pending, dapat: %+v", st)
	}

	// Up: terapkan keduanya.
	done, err := m.Up(ctx)
	if err != nil {
		t.Fatalf("up: %v", err)
	}
	if len(done) != 2 {
		t.Fatalf("up harus menerapkan 2, dapat %d", len(done))
	}
	if !tableExists(t, pool, ctx, "mig_demo", "a") || !tableExists(t, pool, ctx, "mig_demo", "b") {
		t.Fatal("tabel a & b harus ada setelah up")
	}

	// Up lagi: idempoten, tidak ada yang baru.
	done2, err := m.Up(ctx)
	if err != nil {
		t.Fatalf("up kedua: %v", err)
	}
	if len(done2) != 0 {
		t.Fatalf("up kedua harus nol, dapat %d", len(done2))
	}

	// Down: rollback migrasi terakhir (002).
	mig, err := m.Down(ctx)
	if err != nil {
		t.Fatalf("down: %v", err)
	}
	if mig == nil || mig.Version != "002" {
		t.Fatalf("down harus rollback 002, dapat %+v", mig)
	}
	if tableExists(t, pool, ctx, "mig_demo", "b") {
		t.Fatal("tabel b harus hilang setelah down")
	}
	if !tableExists(t, pool, ctx, "mig_demo", "a") {
		t.Fatal("tabel a harus tetap ada")
	}

	// Status akhir: 001 applied, 002 pending.
	st, _ = m.Status(ctx)
	if !st[0].Applied || st[1].Applied {
		t.Fatalf("status akhir salah: %+v", st)
	}
}

func TestLoadMigrations_RealModules(t *testing.T) {
	// Muat dari direktori modules nyata agar pasangan up/down surat_masuk terbaca.
	migs, err := db.LoadMigrations(os.DirFS("../../modules"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	var found bool
	for _, m := range migs {
		if m.Module == "surat_masuk" && m.Version == "001" {
			found = true
			if m.UpSQL == "" || m.DownSQL == "" {
				t.Fatal("surat_masuk 001 harus punya up & down")
			}
		}
	}
	if !found {
		t.Fatal("migrasi surat_masuk:001 tidak ditemukan")
	}
}
