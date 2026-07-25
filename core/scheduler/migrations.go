package scheduler

import "embed"

// MigrationsFS — sumber tunggal skema komponen scheduler (lihat core/config.MigrationsFS).
//
//go:embed migrations/*.sql
var MigrationsFS embed.FS

// MigrationModule adalah nama modul untuk tracking migrasi. Harus unik lintas komponen.
const MigrationModule = "scheduler"
