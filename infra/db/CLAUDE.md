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
- TenantConnManager (conn_manager.go): routing central vs tenant pool per residency
  (ADR-005); pool di-cache per (host, dbname). Lihat catatan "Pool cache & concurrency".
- Provisioning (provision.go, ADR-006): CREATE DATABASE pakai kredensial admin terpisah
  ber-CREATEDB; OWNER = app user; migrasi dijalankan sebagai app user.

## Pool cache & concurrency (TenantConnManager)
Pool koneksi bersifat per-proses (memegang socket TCP), jadi cache pool-per-(host,dbname)
adalah satu instance per app process — BUKAN global singleton package-level. Saat app
di-cluster, tiap instance punya cache pool sendiri; tidak ada state dibagi lintas node.
Yang harus dijaga operasional: total koneksi ke DB shared (central + identity) =
jumlah_instance × pool_max — set pool_max konservatif dan/atau pasang PgBouncer.

Penguncian: mutex manajer dipegang SINGKAT hanya untuk akses map entry; pembukaan pool
(dial jaringan) di bawah lock PER-ENTRY. Konsekuensi sengaja:
- Key berbeda dibuka paralel (tidak ada head-of-line blocking antar tenant saat cold start).
- Key sama diserialisasi → tepat satu pool per (host,dbname).
- Kegagalan open TIDAK di-cache: entry dibiarkan kosong agar pemanggilan berikutnya retry
  (mis. DB sempat tak terjangkau). Entry kosong tetap di map (bounded oleh jumlah tenant).
- Ordering kunci selalu mu → entry.mu (Close mengikuti urutan ini) → bebas deadlock.

## Pitfall umum
- JOIN lintas-schema modul lain (dilarang) [linter: no-cross-schema-join].
- Asumsi satu DB untuk semua tenant.
- Lupa soft delete filter (deleted_at IS NULL) di query default.
- Memegang mutex manajer selama membuka pool (mengembalikan head-of-line blocking antar
  tenant). Buka pool HANYA di bawah lock per-entry.

## Test
- Integration (testcontainers Postgres): CRUD, optimistic lock conflict, migration
  up/down, soft delete.
- go test ./infra/db/... -tags=integration

## Rujukan
- PRD.md (root: Migration strategy, DB-per-tenant), port/repository.go
