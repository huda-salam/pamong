package config

import "embed"

// MigrationsFS adalah satu-satunya sumber skema komponen config: dijalankan otoritatif oleh
// `pamongctl migrate` (produksi, ter-track di gov.migration_history) dan oleh EnsureSchema
// infra/config sebagai bootstrap dev/test. Menggantikan const DDL paralel yang dulu dekoratif.
//
//go:embed migrations/*.sql
var MigrationsFS embed.FS

// MigrationModule adalah nama modul untuk tracking migrasi. Harus unik lintas komponen.
const MigrationModule = "config"
