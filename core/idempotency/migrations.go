// Package idempotency memiliki skema tabel gov.idempotency_keys (framework-level data
// integrity, CLAUDE.md §Data integrity). Tidak ada logika domain di sini — idempotency
// ditegakkan di middleware gateway (driving adapter) di atas port.IdempotencyStore; paket
// ini hanya sumber tunggal migrasi tabelnya (pola sama dengan core/notification dll).
package idempotency

import "embed"

// MigrationsFS — sumber tunggal skema komponen idempotency (lihat infra/schema.CoreMigrations).
//
//go:embed migrations/*.sql
var MigrationsFS embed.FS

// MigrationModule adalah nama modul untuk tracking migrasi. Harus unik lintas komponen.
const MigrationModule = "idempotency"
