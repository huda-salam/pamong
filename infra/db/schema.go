package db

import (
	"context"
	"fmt"
	"io/fs"
)

// schemaBootstrapLock adalah kunci advisory TUNGGAL untuk seluruh EnsureSchema pada satu DB.
// Semua pembuatan skema (CREATE SCHEMA gov + tabel) diserialisasi di kunci ini, sehingga dua
// proses yang boot berbarengan ke DB yang sama tidak balapan pada `CREATE SCHEMA IF NOT EXISTS`
// — operasi itu BUKAN atomik di Postgres (dua koneksi bisa sama-sama lolos cek lalu satu kalah
// di unique index pg_namespace_nspname_index). Idiom advisory-lock sama dgn AuditRepo.Append
// (infra/db/audit.go). Kunci tunggal (bukan per-komponen) supaya CREATE SCHEMA gov yang dipakai
// bersama antar komponen juga terserialisasi.
const schemaBootstrapLock = "pamong.schema.bootstrap"

// ApplyEmbeddedSchema menerapkan migrasi ter-embed satu komponen (module, migrations/*.sql)
// sebagai bootstrap dev/test, dengan pelacakan yang SAMA seperti `pamongctl migrate`: migrasi
// yang sudah tercatat di gov.migration_history di-skip, sehingga aman dipanggil berulang
// (tests memanggilnya beberapa kali) — ini mengapa file .sql mentah (yang tak selalu idempoten
// per-statement, mis. ADD CONSTRAINT tanpa DROP) tetap benar di sini. Seluruh proses berjalan
// dalam SATU transaksi ber-advisory-lock agar boot paralel tidak balapan pada CREATE SCHEMA.
//
// Menjadikan file .sql satu-satunya sumber skema: tidak ada lagi const DDL Go paralel yang bisa
// drift. Jalur produksi otoritatif tetap `pamongctl migrate` (yang kini juga memuat migrasi
// core lewat infra/schema.CoreMigrations).
func ApplyEmbeddedSchema(ctx context.Context, pool *Pool, module string, fsys fs.FS) error {
	migs, err := LoadEmbedded(module, fsys)
	if err != nil {
		return err
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op setelah Commit

	// Serialisasi seluruh bootstrap skema pada DB ini (tutup race CREATE SCHEMA gov).
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, schemaBootstrapLock); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, historyDDL); err != nil {
		return err
	}

	applied, err := appliedInTx(ctx, tx, module)
	if err != nil {
		return err
	}
	for _, mig := range migs {
		if applied[mig.Version] {
			continue
		}
		if _, err := tx.Exec(ctx, mig.UpSQL); err != nil {
			return fmt.Errorf("apply %s:%s: %w", mig.Module, mig.Version, err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO gov.migration_history (module, version, name, checksum) VALUES ($1,$2,$3,$4)`,
			mig.Module, mig.Version, mig.Name, mig.checksum()); err != nil {
			return fmt.Errorf("catat %s:%s: %w", mig.Module, mig.Version, err)
		}
	}
	return tx.Commit(ctx)
}

// appliedInTx mengembalikan set versi migrasi module yang sudah tercatat, dibaca dalam tx.
func appliedInTx(ctx context.Context, tx *Tx, module string) (map[string]bool, error) {
	rows, err := tx.Query(ctx, `SELECT version FROM gov.migration_history WHERE module = $1`, module)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	set := make(map[string]bool)
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		set[v] = true
	}
	return set, rows.Err()
}
