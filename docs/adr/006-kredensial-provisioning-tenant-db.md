# ADR-006: Kredensial & privilege boundary provisioning tenant DB

## Status
Accepted

## Konteks
ADR-004 menetapkan model koneksi DB multi-tenant: kredensial runtime BERSAMA
(`GOV_DB_*`) untuk menjangkau tenant DB, lokasi (host, dbname) dari `id.tenant_registry`,
routing lewat `TenantConnManager`. ADR-004 secara eksplisit **menunda** satu keputusan
ke PR-2.2.3: *siapa yang berhak `CREATE DATABASE` saat provisioning tenant baru —
kredensial admin terpisah vs kredensial app bersama.*

`CREATE DATABASE` di Postgres butuh role ber-`CREATEDB` (atau superuser). Provisioning
tenant adalah operasi **admin yang jarang** (onboarding), berbeda sifat dari steady-state
melayani request. Pertanyaannya: apakah kredensial runtime app (`GOV_DB_*`) yang dipakai
melayani setiap request harus diberi wewenang membuat database?

## Keputusan
**Kredensial provisioning dipisah dari kredensial runtime (least privilege).**

1. **Kredensial admin provisioning** — blok config baru `GOV_PROVISION_DB_*`
   (`USER`, `PASSWORD`, `MAINTENANCE`). Role ini ber-`CREATEDB`. Dipakai **hanya** oleh
   jalur provisioning (`pamongctl tenant provision`), tidak pernah oleh runtime.
2. **Kredensial runtime app** (`GOV_DB_*`) tetap **least-privilege**: tidak perlu
   `CREATEDB`, tidak perlu superuser. Koneksi yang melayani request tidak pernah membawa
   wewenang membuat/menghapus database.
3. **Pembagian wewenang di dalam DB tenant.** Admin menjalankan
   `CREATE DATABASE "<dbname>" OWNER "<app_user>"`: admin hanya membuat database dan
   menetapkan app user sebagai **owner**. Setelah itu **migrasi dijalankan sebagai app
   user** (kredensial runtime), bukan admin. Hasilnya app user memiliki penuh objek di
   tenant DB-nya sendiri, tanpa pernah punya privilege global `CREATEDB`.
4. **Koneksi maintenance.** `CREATE DATABASE` tidak bisa berjalan di dalam transaksi dan
   butuh koneksi ke database lain pada host target. Admin connect ke
   `GOV_PROVISION_DB_MAINTENANCE` (default `postgres`) pada host target, eksekusi
   `CREATE DATABASE`, tutup, lalu beralih ke DB baru sebagai app user untuk migrasi.
5. **Host target & port** berasal dari `id.tenant_registry` (host) + `GOV_DB_PORT`
   (port bersama), konsisten ADR-004. Provisioning idempoten: bila database sudah ada,
   `CREATE DATABASE` dilewati, migrasi tetap dijalankan (aman, ter-track
   `gov.migration_history`).

## Konsekuensi
- `config.ProvisionDBConfig` ditambahkan; `GOV_PROVISION_DB_*` tidak wajib saat boot
  (operasi admin, bukan steady-state) — hanya wajib saat menjalankan provisioning.
- `infra/db.Provisioner` mengeksekusi `CREATE DATABASE ... OWNER` dengan kredensial admin,
  lalu `Migrator.Up` dengan kredensial app. Identifier (dbname, owner) divalidasi terhadap
  pola aman + di-quote — provisioning menerima nama dari registry, bukan dari input bebas.
- `pamongctl tenant provision --tenant <id>` connect ke identity DB untuk membaca registry
  (menutup gap "pamongctl belum bisa target identity DB" — kini identity DB ter-wire untuk
  provisioning).
- Operator wajib menyiapkan dua role Postgres: app role (least-privilege) + provisioning
  role (`CREATEDB`). Trade-off operasional ini sepadan dengan menghapus permukaan
  eskalasi pada koneksi runtime.

## Alternatif yang dipertimbangkan
- **Kredensial app bersama diberi `CREATEDB`, dipakai provisioning sekaligus runtime.**
  Lebih sedikit knob config, tapi setiap koneksi runtime membawa wewenang membuat/menghapus
  database — eskalasi tak perlu bila kredensial runtime bocor. Ditolak: melanggar least
  privilege, tidak sepadan untuk konteks pemerintahan.
- **Migrasi dijalankan sebagai admin (admin owner DB).** Membuat objek tenant dimiliki
  role admin; app user lalu perlu GRANT eksplisit. Menambah langkah & permukaan salah
  konfigurasi. `CREATE DATABASE ... OWNER app_user` lebih sederhana dan langsung benar.

## Keputusan tertunda
- **Tier 3 (server VPS milik pemda)** dapat memakai kredensial admin & app berbeda
  per-host. Sejalan dengan keputusan tertunda ADR-004 (registry menyimpan *referensi*
  kredensial / integrasi secret manager). Tidak dibangun sekarang — `GOV_PROVISION_DB_*`
  kini berlaku untuk host shared (Tier 1/2).
- **Pencatatan `migration_version` & transisi status registry pasca-provisioning** (mis.
  `is_active` di-set setelah schema siap) — lifecycle ber-audit tersendiri, di luar scope
  PR-2.2.3.
