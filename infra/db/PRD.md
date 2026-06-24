# PRD: Database Adapter

## Tujuan
Mengimplementasi persistensi di Postgres dengan model DB-per-tenant + schema-per-module,
menyediakan repository generik (termasuk untuk Tier 1 CRUD), dan menjalankan migrasi
per-tenant dengan tracking ganda.

## Kebutuhan fungsional
- F1: BaseRepository[T]: FindByID, Save, Update (optimistic lock), SoftDelete, List
  (filter/sort/search/paginate). Soft delete default (deleted_at IS NULL).
- F2: Connection management dari tenant_registry: shared (search_path) atau dedicated
  (pool ke host/db tenant). Pool per unique host.
- F3: Migration runner: kumpulkan migrasi semua modul (urut by DependsOn), jalankan di
  DB tenant target; up/down; tracking di gov.migration_history (tenant) +
  id.tenant_migrations (sentral). Backward-compatible migration; breaking butuh dua rilis.
- F4: Query builder Tier 1: generic CRUD dari EntityDef tanpa kode modul.
- F5: Cegah JOIN lintas-schema modul lain (linter + guard di builder).

## Kebutuhan non-fungsional
- Connection pool: konfigurabel (max, idle) per config.
- Migrasi per-tenant idempoten; gagal di satu tenant tidak menghalangi tenant lain.
- Query Tier 1: gunakan prepared statement; introspeksi EntityDef di-cache.

## Dependency
- port/repository.go; tenant_registry (identity); config (pool, DSN).

## Anti-pattern
- Satu DB untuk banyak tenant. JOIN lintas-schema modul. Lupa soft-delete filter.

## Acceptance criteria
- [ ] CRUD ke Postgres (testcontainers) lulus; soft delete menyembunyikan record.
- [ ] Optimistic lock: update version basi → conflict (0 rows affected → ErrConflict).
- [ ] Migration up & down bersih; tracking tercatat di tenant DB & sentral.
- [ ] Tenant berbeda → koneksi/DB berbeda (isolasi).
- [ ] Migrasi gagal di satu tenant tidak menghalangi tenant lain.
