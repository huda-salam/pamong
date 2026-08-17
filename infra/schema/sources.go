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
	coreIdem "github.com/huda-salam/pamong/core/idempotency"
	coreNotif "github.com/huda-salam/pamong/core/notification"
	coreSched "github.com/huda-salam/pamong/core/scheduler"
	coreSeq "github.com/huda-salam/pamong/core/sequence"
	coreWf "github.com/huda-salam/pamong/core/workflow"
	"github.com/huda-salam/pamong/infra/db"
)

// coreComponent memasangkan nama modul migrasi dengan FS embed-nya.
type coreComponent struct {
	module string
	fs     fs.FS
}

// tenantComponents adalah komponen framework yang tabelnya hidup di DB TIAP TENANT. Identity
// SENGAJA belum di sini — dimasukkan pada PR terpisah (perubahan identity butuh review ekstra,
// CLAUDE.md).
var tenantComponents = []coreComponent{
	{coreCfg.MigrationModule, coreCfg.MigrationsFS},
	{coreIdem.MigrationModule, coreIdem.MigrationsFS},
	{coreNotif.MigrationModule, coreNotif.MigrationsFS},
	{coreSeq.MigrationModule, coreSeq.MigrationsFS},
	{coreWf.MigrationModule, coreWf.MigrationsFS},
}

// centralComponents adalah komponen framework yang tabelnya hidup di DB SENTRAL (ADR-023).
//
// Scheduler ada di sini karena PEMBACA-nya tak punya tenant: Runner.RunDue adalah loop
// proses-lebar yang bertanya "apa yang jatuh tempo, di mana saja?" — pertanyaan yang tak bisa
// dijawab satu pool tenant. Residensi mengikuti pembaca, bukan penulis (ADR-023 Keputusan 1).
//
// Nama schema-nya tetap `gov` (ADR-023 Keputusan 7): `gov` menandai "tabel framework", bukan
// "tabel tenant". Yang memisahkan tenant dari sentral adalah DAFTAR INI — bukan nama schema —
// jadi menambahkan komponen ke daftar yang keliru akan menempatkan tabelnya di DB yang keliru
// tanpa satu pun error.
var centralComponents = []coreComponent{
	{coreSched.MigrationModule, coreSched.MigrationsFS},
}

// CoreMigrations mengumpulkan migrasi komponen core yang ber-residensi TENANT. Migrator akan
// mengurut ulang secara global (module:version), jadi urutan di sini tidak signifikan.
func CoreMigrations() ([]db.Migration, error) {
	return collect(tenantComponents)
}

// CentralMigrations mengumpulkan migrasi komponen core yang ber-residensi SENTRAL (ADR-023).
// Diterapkan ke DB sentral (`AppConfig.CentralDBResolved()`), bukan ke DB tenant mana pun —
// lihat `pamongctl migrate --central`.
func CentralMigrations() ([]db.Migration, error) {
	return collect(centralComponents)
}

func collect(comps []coreComponent) ([]db.Migration, error) {
	var out []db.Migration
	for _, c := range comps {
		migs, err := db.LoadEmbedded(c.module, c.fs)
		if err != nil {
			return nil, fmt.Errorf("muat migrasi core %s: %w", c.module, err)
		}
		out = append(out, migs...)
	}
	return out, nil
}
