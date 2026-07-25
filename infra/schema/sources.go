// Package schema merakit migrasi komponen framework (core/*) yang di-embed menjadi daftar
// db.Migration untuk migrator. Tanpa ini, migrasi core tidak pernah dijalankan
// `pamongctl migrate` (yang selama ini hanya memuat dir modules/) — tak masuk
// gov.migration_history, tak bisa di-down. Dipakai oleh pamongctl migrate; nanti juga oleh
// bootstrap cmd/server saat pool DB tersedia.
package schema

import (
	"fmt"
	"io/fs"

	coreCfg "github.com/huda-salam/pamong/core/config"
	coreNotif "github.com/huda-salam/pamong/core/notification"
	coreSched "github.com/huda-salam/pamong/core/scheduler"
	coreWf "github.com/huda-salam/pamong/core/workflow"
	"github.com/huda-salam/pamong/infra/db"
)

// coreComponent memasangkan nama modul migrasi dengan FS embed-nya.
type coreComponent struct {
	module string
	fs     fs.FS
}

// coreComponents adalah komponen framework yang migrasinya di-embed. Identity SENGAJA belum
// di sini — dimasukkan pada PR terpisah (perubahan identity butuh review ekstra, CLAUDE.md).
var coreComponents = []coreComponent{
	{coreCfg.MigrationModule, coreCfg.MigrationsFS},
	{coreNotif.MigrationModule, coreNotif.MigrationsFS},
	{coreSched.MigrationModule, coreSched.MigrationsFS},
	{coreWf.MigrationModule, coreWf.MigrationsFS},
}

// CoreMigrations mengumpulkan migrasi seluruh komponen core ter-embed. Migrator akan mengurut
// ulang secara global (module:version), jadi urutan di sini tidak signifikan.
func CoreMigrations() ([]db.Migration, error) {
	var out []db.Migration
	for _, c := range coreComponents {
		migs, err := db.LoadEmbedded(c.module, c.fs)
		if err != nil {
			return nil, fmt.Errorf("muat migrasi core %s: %w", c.module, err)
		}
		out = append(out, migs...)
	}
	return out, nil
}
