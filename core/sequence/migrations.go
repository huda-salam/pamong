// Package sequence memiliki skema tabel gov.sequences: sumber nomor ber-urut ATOMIK
// per-tenant (nomor agenda surat, nomor SPM, dst), reset per tahun fiskal. Tidak ada logika
// domain di sini — generator nomor ditegakkan di driven adapter (infra/sequence) di atas
// port.SequenceGenerator; paket ini hanya sumber tunggal migrasi tabelnya (pola sama dengan
// core/idempotency).
package sequence

import "embed"

// MigrationsFS — sumber tunggal skema komponen sequence (lihat infra/schema.CoreMigrations).
//
//go:embed migrations/*.sql
var MigrationsFS embed.FS

// MigrationModule adalah nama modul untuk tracking migrasi. Harus unik lintas komponen.
const MigrationModule = "sequence"
