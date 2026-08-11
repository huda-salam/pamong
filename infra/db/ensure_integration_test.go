//go:build integration

package db_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/huda-salam/pamong/infra/db"
	"github.com/huda-salam/pamong/port"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestEnsureSchemaLocked_BootParalel_TakBalapan — inti perbaikan review PR-W3b: `IF NOT EXISTS`
// TIDAK membuat DDL Postgres atomik. Dua koneksi bisa sama-sama lolos pemeriksaan "belum ada" lalu
// satu kalah di unique index katalog sistem (`pg_namespace_nspname_index` untuk CREATE SCHEMA,
// `pg_type_typname_nsp_index` untuk CREATE TABLE) dengan SQLSTATE 23505.
//
// Sebelum PR-W3b hal ini nyaris tak terlihat: ensure-on-write hanya jalan saat boot. Sejak
// pemeriksaan wewenang per-unit ikut memicunya, ia menjadi DUA REQUEST BERSAMAAN pada tenant baru —
// dan yang kalah gagal SESUDAH mutasinya commit, jadi barisnya tersimpan tanpa audit.
//
// Test ini menjalankan ensure secara paralel di atas schema yang baru dihapus. Dengan advisory lock
// semuanya lulus; tanpa lock (ganti EnsureSchemaLocked dengan Exec biasa) mayoritas goroutine gagal.
// raceN = jumlah ensure serentak. Cukup untuk membuat balapan katalog sistem muncul konsisten pada
// container test proyek ini; juga dipakai sebagai MaxConns pool agar goroutine tak mengantre.
const raceN = 12

func TestEnsureSchemaLocked_BootParalel_TakBalapan(t *testing.T) {
	dsn := os.Getenv("PAMONG_TEST_DB_DSN")
	if dsn == "" {
		t.Skip("PAMONG_TEST_DB_DSN tidak diset — lewati integration test")
	}
	ctx := context.Background()
	// MaxConns dinaikkan eksplisit: default pgxpool bisa lebih kecil dari jumlah goroutine, dan
	// dengan pool sempit "12 goroutine" berubah menjadi antrean — DDL-nya berurutan dan balapan
	// yang mau diuji tak pernah terjadi (test hijau tanpa menguji apa pun).
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	cfg.MaxConns = raceN
	pgpool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("buka pool: %v", err)
	}
	defer pgpool.Close()
	pool := db.NewPool(pgpool)

	const ddl = `
CREATE SCHEMA IF NOT EXISTS gov_ensure_race;
CREATE TABLE IF NOT EXISTS gov_ensure_race.contoh (
    id   UUID PRIMARY KEY,
    nama TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_contoh_nama ON gov_ensure_race.contoh (nama);`

	drop := func() {
		_, _ = pool.Exec(context.Background(), `DROP SCHEMA IF EXISTS gov_ensure_race CASCADE`)
	}
	drop()
	t.Cleanup(drop)

	errs := make([]error, raceN)
	var wg sync.WaitGroup
	var siap sync.WaitGroup
	start := make(chan struct{})
	for i := range raceN {
		wg.Add(1)
		siap.Add(1)
		go func() {
			defer wg.Done()
			// Panaskan dulu satu koneksi per goroutine. Tanpa ini, burst pertama menghabiskan
			// waktunya MEMBUKA koneksi (pgxpool membukanya satu-satu), sehingga tiap DDL sudah
			// commit sebelum yang berikut mulai — balapan tak terjadi dan test lulus bahkan tanpa
			// advisory lock. Ini yang membuat versi pertama test ini tak menguji apa pun.
			if _, err := pool.Exec(ctx, `SELECT 1`); err != nil {
				errs[i] = err
			}
			siap.Done()
			<-start // lepas serentak
			if errs[i] == nil {
				errs[i] = db.EnsureSchemaLocked(ctx, pool, ddl)
			}
		}()
	}
	siap.Wait()
	close(start)
	wg.Wait()

	var gagal []string
	for i, err := range errs {
		if err != nil {
			gagal = append(gagal, fmt.Sprintf("#%d: %v", i, err))
		}
	}
	if len(gagal) > 0 {
		t.Fatalf("%d/%d ensure paralel gagal — advisory lock tidak menyerialisasi DDL:\n%v",
			len(gagal), raceN, gagal)
	}

	// Tabelnya memang ada (bukan "semua diam-diam tak melakukan apa pun").
	var ada bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (
        SELECT 1 FROM information_schema.tables
        WHERE table_schema = 'gov_ensure_race' AND table_name = 'contoh')`).Scan(&ada); err != nil {
		t.Fatalf("periksa tabel: %v", err)
	}
	if !ada {
		t.Fatal("tabel tidak terbentuk padahal semua ensure melaporkan sukses")
	}
}

// TestSchemaMemo_KunciDariKoneksi — memo dikunci dari KONEKSI (db.DBKeyer), bukan dari tenant di
// payload. Untuk `*Pool` (satu DB) kuncinya konstan: ensure kedua dengan tenant BERBEDA di context
// tak boleh menjalankan DDL lagi. Itulah yang membuat audit sentral `id.audit_logs` — yang sudah
// dipastikan saat boot — tak menyeret balapan bootstrap ke jalur request `/admin/identity/*`.
func TestSchemaMemo_KunciDariKoneksi(t *testing.T) {
	dsn := os.Getenv("PAMONG_TEST_DB_DSN")
	if dsn == "" {
		t.Skip("PAMONG_TEST_DB_DSN tidak diset — lewati integration test")
	}
	ctx := context.Background()
	pgpool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("buka pool: %v", err)
	}
	defer pgpool.Close()
	pool := db.NewPool(pgpool)

	drop := func() {
		_, _ = pool.Exec(context.Background(), `DROP SCHEMA IF EXISTS gov_memo_key CASCADE`)
	}
	drop()
	t.Cleanup(drop)

	var memo db.SchemaMemo
	const ddl = `CREATE SCHEMA IF NOT EXISTS gov_memo_key;
CREATE TABLE IF NOT EXISTS gov_memo_key.contoh (id UUID PRIMARY KEY);`

	if err := memo.Ensure(ctx, pool, ddl); err != nil {
		t.Fatalf("ensure pertama: %v", err)
	}
	// Hapus skemanya, lalu ensure lagi dengan tenant berbeda di context. Bila memo dikunci per
	// TENANT, DDL akan berjalan ulang dan skema terbentuk kembali — dan justru itu yang tak
	// diinginkan pada koneksi satu-DB.
	drop()
	for _, tenant := range []string{"pemkot-a", "pemkot-b"} {
		if err := memo.Ensure(portWithTenant(ctx, tenant), pool, ddl); err != nil {
			t.Fatalf("ensure ulang (tenant %s): %v", tenant, err)
		}
	}
	var ada bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (
        SELECT 1 FROM information_schema.schemata WHERE schema_name = 'gov_memo_key')`).Scan(&ada); err != nil {
		t.Fatalf("periksa schema: %v", err)
	}
	if ada {
		t.Fatal("DDL berjalan ulang untuk tenant berbeda pada *Pool — memo dikunci per tenant, " +
			"bukan per DB; audit sentral akan membayar DDL sekali per tenant")
	}
}

// portWithTenant = pembungkus tipis supaya maksud test terbaca di titik pakainya.
func portWithTenant(ctx context.Context, tenantID string) context.Context {
	return port.WithTenant(ctx, tenantID)
}
