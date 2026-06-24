# infra/db — Database Adapter (Postgres/pgx)

Driven adapter: implementasi port.BaseRepository untuk Postgres. DB-per-tenant,
schema-per-module. Migration runner. Koneksi dikelola berdasarkan tenant_registry.

## Bergantung pada
- port/repository.go; pustaka pgx

## Tanggung jawab
- Implementasi BaseRepository (CRUD + optimistic lock + soft delete + list)
- Connection management: pool per tenant (shared search_path / dedicated host)
- Migration runner: versioned up/down, tracking di tenant DB + sentral
- Query builder untuk Tier 1 generic CRUD
- Penegakan no-cross-schema-join saat membangun query

## File kunci
- repository.go — BaseRepository impl
- pool.go — connection pool per tenant/host
- migration.go — runner (up/down/status), tracking
- querybuilder.go — generic CRUD query untuk Tier 1

## Konvensi khusus
- DB-per-tenant: tiap tenant DB sendiri. Schema-per-module di dalamnya.
- Tabel: {schema}.{entity_plural}. Kolom standar: id, version, created_at, updated_at,
  deleted_at.
- Optimistic lock: UPDATE ... WHERE version = $expected; 0 rows -> conflict.
- Migration tracking: gov.migration_history (tenant DB) + id.tenant_migrations (sentral).

## Pitfall umum
- JOIN lintas-schema modul lain (dilarang) [linter: no-cross-schema-join].
- Asumsi satu DB untuk semua tenant.
- Lupa soft delete filter (deleted_at IS NULL) di query default.

## Test
- Integration (testcontainers Postgres): CRUD, optimistic lock conflict, migration
  up/down, soft delete.
- go test ./infra/db/... -tags=integration

## Rujukan
- PRD.md (root: Migration strategy, DB-per-tenant), port/repository.go
