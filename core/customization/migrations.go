package customization

import "embed"

// MigrationsFS adalah sumber tunggal skema komponen customization: dijalankan otoritatif oleh
// `pamongctl migrate` (produksi, ter-track di gov.migration_history) dan oleh EnsureSchema
// infra/customization sebagai bootstrap dev/test.
//
//go:embed migrations/*.sql
var MigrationsFS embed.FS

// MigrationModule adalah nama modul untuk tracking migrasi. Harus unik lintas komponen.
const MigrationModule = "customization"
