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
- **Enkripsi field selektif transparan** (ADR-009/015, PR-3.8.3/3.8.4): kolom ber-class
  `personal_id`/`specific` dienkripsi & didekripsi OTOMATIS di sini, plus enkripsi nilai
  sensitif pada diff audit

## File kunci
- repository.go — BaseRepository impl
- pool.go — connection pool per tenant/host
- migration.go — runner (up/down/status), tracking
- querybuilder.go — generic CRUD query untuk Tier 1
- ddl.go — generator DDL dari EntityDef (termasuk kolom `_enc`/`_bidx`)
- field_crypto.go — terjemahan kolom logis ↔ kolom fisik terenkripsi + `decryptingScanner`
- audited_repository.go — factory `NewRepository` (auto-attach audit + kripto), enkripsi diff

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

## Enkripsi field transparan (ADR-009/015)
Satu field logis ber-class terenkripsi = DUA kolom fisik: `{f}_enc` (ciphertext) dan
`{f}_bidx` (blind index, bila `Searchable`). Kolom plaintext `{f}` TIDAK PERNAH ada.

Alur & aturan yang menopangnya:
- Spec kripto DITURUNKAN dari `EntityDef` (`FieldCryptoFromEntity`) lalu diserahkan ke repo
  lewat `db.WithCrypto`. `Mapper[T]` sengaja TIDAK tahu apa-apa soal kripto — menaruh
  kebijakan keamanan di mapper tulis-tangan Tier 3 = tempat paling mudah lupa.
- `decryptingScanner` menyisip SEBELUM `Mapper.Scan`: ia menukar pointer tujuan pada posisi
  kolom terenkripsi, mendekripsi, lalu menulis hasilnya ke pointer asli.
- Equality filter dialihkan ke `_bidx`. Sort & search (ILIKE) atas kolom terenkripsi DITOLAK
  lantang — bukan dibiarkan mengembalikan hasil yang tampak sah.
- Setiap pembacaan memeriksa `PurposeOf(ciphertext)` = purpose kolom (ADR-015). AAD hanya
  mengikat tenant, jadi tanpa pemeriksaan ini blob bisa dipindah antar kolom DALAM satu
  tenant lewat satu `UPDATE` dan tetap terbaca sebagai nilai yang sah.
- `NewRepository` MENOLAK entity ber-field terenkripsi bila `CryptoPort` tak diberikan, dan
  enkripsi gagal keras bila tenant tak ada di context (`port.WithTenant`).
- Diff audit ikut terenkripsi (`auditedRepo.snapshot` → `sealPair`) — snapshot diambil dari
  ENTITY (plaintext), jadi mengenkripsi kolom saja hanya MEMINDAHKAN kebocoran ke
  `audit_logs.diff`. Sisi bacanya: `core/audit.Reader` + permission `audit:sensitive:baca`.
- Penyegelan diff membandingkan plaintext **sebelum** mengenkripsi, dan memproses kedua sisi
  bersama. Ciphertext tidak deterministik, jadi perbandingan apa pun setelah enkripsi salah.
- Pengikatan `PurposeOf` berlaku per-KOLOM, bukan per-baris: menukar `_enc`+`_bidx` antar
  baris masih lolos (ADR-015 §Konsekuensi, ditutup PR-3.8.9).

## Pitfall umum
- JOIN lintas-schema modul lain (dilarang) [linter: no-cross-schema-join].
- Asumsi satu DB untuk semua tenant.
- Lupa soft delete filter (deleted_at IS NULL) di query default.
- Memegang mutex manajer selama membuka pool (mengembalikan head-of-line blocking antar
  tenant). Buka pool HANYA di bawah lock per-entry.
- Menambah jalur tulis baru (bulk insert, upsert, COPY) tanpa melewati `writeCols`/`writeVals`
  — pengenal akan mendarat plaintext tanpa satu pun test yang gagal.
- **Mengenkripsi nilai diff audit per sisi (before dan after masing-masing).** Nonce acak
  membuat nilai yang SAMA menghasilkan blob berbeda, sehingga `audit.Diff` melaporkan setiap
  kolom terenkripsi sebagai berubah pada setiap update — jejak audit mengarang perubahan
  pengenal dan supresi no-op update mati. Kebalikannya sama berbahaya: penanda kegagalan yang
  identik di kedua sisi membuat perubahan nyata tampak tidak berubah lalu TERBUANG. Aturannya:
  bandingkan plaintext dulu, segel sesudahnya, penanda gagal per sisi harus berbeda.
- Menyusun `FieldCryptoSpec` dengan tangan alih-alih dari `EntityDef`: spec yang menyimpang
  dari deklarasi field tidak menimbulkan gejala apa pun, hanya kolom yang diam-diam mentah.
- Mengira unit test cukup untuk perubahan di jalur ini. Ia memakai koneksi palsu; kecocokan
  dengan DDL nyata hanya terbukti lewat `field_crypto_integration_test.go`.

## Test
- Integration (Postgres nyata): CRUD, optimistic lock conflict, migration up/down, soft
  delete, dan enkripsi field ujung-ke-ujung (tabel dibangun dari `GenerateMigration`, bukan
  DDL tulis tangan — yang diuji justru SAMBUNGAN generator DDL ↔ repo).
- `PAMONG_TEST_DB_DSN=... go test ./infra/db/... -tags=integration -p 1`

## Rujukan
- PRD.md (root: Migration strategy, DB-per-tenant), port/repository.go
- ADR-009 (klasifikasi & enkripsi field), ADR-015 (`PurposeOf`/pengikatan kolom), ADR-002 (audit diff)
