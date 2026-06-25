package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/huda-salam/pamong/port"
)

// Migration runner (PR-1.2.3). Definisi migrasi hidup di kode modul
// (modules/{modul}/migrations/{version}_{name}.up.sql + .down.sql). Runner
// menjalankannya per-tenant (DB-per-tenant) dan melacak yang sudah jalan di
// gov.migration_history pada DB tenant tersebut. Tiap migrasi atomik (satu transaksi).

// Migration adalah satu unit migrasi: pasangan up/down milik satu modul.
type Migration struct {
	Module  string // nama modul pemilik
	Version string // prefix numerik terurut, mis. "001"
	Name    string // deskripsi, mis. "create_surat_masuk"
	UpSQL   string
	DownSQL string
}

// key adalah identitas global migrasi untuk pengurutan & tracking.
func (m Migration) key() string { return m.Module + ":" + m.Version }

func (m Migration) checksum() string {
	sum := sha256.Sum256([]byte(m.UpSQL))
	return hex.EncodeToString(sum[:])
}

// MigrationStatus melaporkan satu migrasi beserta apakah sudah diterapkan.
type MigrationStatus struct {
	Migration
	Applied bool
}

// beginner adalah subset Pool yang dibutuhkan runner: memulai transaksi atomik.
type beginner interface {
	Begin(ctx context.Context) (*Tx, error)
}

// Migrator menjalankan migrasi terhadap satu DB (tenant). Migrasi diurutkan global
// secara deterministik berdasarkan (module, version).
type Migrator struct {
	conn       beginner
	exec       port.DBConn // untuk query histori non-transaksional (status)
	migrations []Migration
}

// NewMigrator membuat runner. `pool` dipakai untuk transaksi maupun query histori.
func NewMigrator(pool *Pool, migrations []Migration) *Migrator {
	sorted := append([]Migration(nil), migrations...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].key() < sorted[j].key() })
	return &Migrator{conn: pool, exec: pool, migrations: sorted}
}

const historyDDL = `
CREATE SCHEMA IF NOT EXISTS gov;
CREATE TABLE IF NOT EXISTS gov.migration_history (
    id         BIGSERIAL PRIMARY KEY,
    module     TEXT NOT NULL,
    version    TEXT NOT NULL,
    name       TEXT NOT NULL,
    checksum   TEXT NOT NULL,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (module, version)
);`

// EnsureHistory membuat schema gov & tabel tracking bila belum ada.
func (m *Migrator) EnsureHistory(ctx context.Context) error {
	_, err := m.exec.Exec(ctx, historyDDL)
	return err
}

// applied mengembalikan set key migrasi yang sudah diterapkan.
func (m *Migrator) applied(ctx context.Context) (map[string]bool, error) {
	rows, err := m.exec.Query(ctx, `SELECT module, version FROM gov.migration_history`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	set := make(map[string]bool)
	for rows.Next() {
		var mod, ver string
		if err := rows.Scan(&mod, &ver); err != nil {
			return nil, err
		}
		set[mod+":"+ver] = true
	}
	return set, rows.Err()
}

// Status melaporkan seluruh migrasi (terurut) beserta status penerapannya.
func (m *Migrator) Status(ctx context.Context) ([]MigrationStatus, error) {
	if err := m.EnsureHistory(ctx); err != nil {
		return nil, err
	}
	applied, err := m.applied(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]MigrationStatus, 0, len(m.migrations))
	for _, mig := range m.migrations {
		out = append(out, MigrationStatus{Migration: mig, Applied: applied[mig.key()]})
	}
	return out, nil
}

// Up menerapkan seluruh migrasi yang belum jalan, terurut. Mengembalikan daftar
// migrasi yang baru diterapkan. Tiap migrasi berjalan dalam transaksinya sendiri.
func (m *Migrator) Up(ctx context.Context) ([]Migration, error) {
	if err := m.EnsureHistory(ctx); err != nil {
		return nil, err
	}
	applied, err := m.applied(ctx)
	if err != nil {
		return nil, err
	}
	var done []Migration
	for _, mig := range m.migrations {
		if applied[mig.key()] {
			continue
		}
		if err := m.runInTx(ctx, mig.UpSQL, func(tx *Tx) error {
			_, e := tx.Exec(ctx,
				`INSERT INTO gov.migration_history (module, version, name, checksum) VALUES ($1,$2,$3,$4)`,
				mig.Module, mig.Version, mig.Name, mig.checksum())
			return e
		}); err != nil {
			return done, fmt.Errorf("migrasi %s gagal: %w", mig.key(), err)
		}
		done = append(done, mig)
	}
	return done, nil
}

// Down me-rollback satu migrasi terakhir yang diterapkan (LIFO). Mengembalikan
// migrasi yang di-rollback, atau nil bila tidak ada yang bisa di-rollback.
func (m *Migrator) Down(ctx context.Context) (*Migration, error) {
	if err := m.EnsureHistory(ctx); err != nil {
		return nil, err
	}
	applied, err := m.applied(ctx)
	if err != nil {
		return nil, err
	}
	// Cari migrasi terurut paling akhir yang sudah diterapkan.
	for i := len(m.migrations) - 1; i >= 0; i-- {
		mig := m.migrations[i]
		if !applied[mig.key()] {
			continue
		}
		if err := m.runInTx(ctx, mig.DownSQL, func(tx *Tx) error {
			_, e := tx.Exec(ctx,
				`DELETE FROM gov.migration_history WHERE module=$1 AND version=$2`,
				mig.Module, mig.Version)
			return e
		}); err != nil {
			return nil, fmt.Errorf("rollback %s gagal: %w", mig.key(), err)
		}
		return &mig, nil
	}
	return nil, nil
}

// runInTx menjalankan sql migrasi + bookkeeping histori dalam satu transaksi.
func (m *Migrator) runInTx(ctx context.Context, sql string, book func(*Tx) error) error {
	tx, err := m.conn.Begin(ctx)
	if err != nil {
		return err
	}
	if strings.TrimSpace(sql) != "" {
		if _, err := tx.Exec(ctx, sql); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
	}
	if err := book(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

var migFileRe = regexp.MustCompile(`^(\d+)_(.+)\.(up|down)\.sql$`)

// LoadMigrations memindai modulesFS untuk file migrasi pada
// {modul}/migrations/{version}_{name}.(up|down).sql dan memasangkannya.
// File up tanpa pasangan down (atau sebaliknya) -> error (sejalan linter migration-needs-down).
func LoadMigrations(modulesFS fs.FS) ([]Migration, error) {
	type half struct{ up, down, name string }
	collected := map[string]*half{} // "module:version" -> half

	err := fs.WalkDir(modulesFS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || path.Base(path.Dir(p)) != "migrations" {
			return nil
		}
		match := migFileRe.FindStringSubmatch(d.Name())
		if match == nil {
			return nil
		}
		// p = {modul}/migrations/{file}; modul = dua tingkat di atas file.
		module := path.Base(path.Dir(path.Dir(p)))
		version, name, dir := match[1], match[2], match[3]
		content, err := fs.ReadFile(modulesFS, p)
		if err != nil {
			return err
		}
		key := module + ":" + version
		h := collected[key]
		if h == nil {
			h = &half{name: name}
			collected[key] = h
		}
		if dir == "up" {
			h.up = string(content)
		} else {
			h.down = string(content)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	out := make([]Migration, 0, len(collected))
	for key, h := range collected {
		parts := strings.SplitN(key, ":", 2)
		if h.up == "" {
			return nil, fmt.Errorf("migrasi %s: file .up.sql tidak ditemukan", key)
		}
		if h.down == "" {
			return nil, fmt.Errorf("migrasi %s: file .down.sql tidak ditemukan (wajib)", key)
		}
		out = append(out, Migration{
			Module: parts[0], Version: parts[1], Name: h.name,
			UpSQL: h.up, DownSQL: h.down,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key() < out[j].key() })
	return out, nil
}
