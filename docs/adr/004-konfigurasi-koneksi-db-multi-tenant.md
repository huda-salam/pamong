# ADR-004: Model konfigurasi koneksi DB untuk multi-tenant (registry-based routing)

## Status
Accepted

## Konteks
CLAUDE.md memuat dua bagian yang tidak selaras soal koneksi database:

- Bagian **"Tenant tier & portabilitas"** (otoritatif) menyatakan: *"tenant resolver
  membaca `tenant_registry` di identity DB untuk menentukan di mana DB tenant berada."*
  Ini model yang benar untuk DB-per-tenant dengan ratusan tenant: lokasi DB tiap tenant
  bersifat data runtime di registry, bukan konfigurasi.
- Bagian **"Konfigurasi"** (blok env) tidak diselaraskan dengan keputusan itu:
  - `GOV_DB_*` ditulis sebagai satu koneksi tenant penuh (`HOST/PORT/NAME=pamong/USER/
    PASSWORD/POOL`) — seolah ada *satu* tenant DB. Sisa asumsi single-tenant.
  - `GOV_IDENTITY_DB_*` hanya `HOST`+`NAME` — tidak lengkap untuk benar-benar connect,
    padahal identity DB justru koneksi sentral yang paling butuh lengkap.

Kode (`config.DBConfig` "tenant aktif", `IdentityDBConfig` hanya Host+Name) menyalin blok
config yang inkonsisten itu. Tidak mungkin menaruh koneksi tiap tenant di env/file config.

ADR ini **menyelaraskan** model konfigurasi dengan bagian tenant-tier yang sudah benar —
bukan keputusan arah baru, melainkan perbaikan konsistensi.

## Keputusan
Koneksi DB dimodelkan tiga lapis:

1. **Dari config/env — hanya yang sentral & bootstrap:**
   - **Identity DB** (`GOV_IDENTITY_DB_*`): koneksi PENUH (host, port, name, user,
     password, pool). Satu-satunya koneksi yang wajib dari config; registry hidup di sini.
   - **Default koneksi tenant DB** (`GOV_DB_*`): parameter BERSAMA untuk menjangkau tenant
     DB — host shared-tier, user, password, port, pool. `GOV_DB_NAME` hanya fallback
     single-tenant/dev; nama DB per-tenant TIDAK dari sini.

2. **Dari `id.tenant_registry` (runtime):** `db_host`, `db_name`, `tier`, `db_schema`
   per-tenant. Bukan kredensial.

3. **Runtime — `TenantConnManager`:** `tenant_id` → lookup registry (host, dbname) →
   gabung dengan kredensial bersama dari config → get-or-create pool, di-cache per
   `(host, dbname)`. Inilah routing yang dipakai tenant resolver gateway (PRD gateway F5).

`GOV_TENANT_ID` hanya relevan untuk deployment single-tenant atau konteks CLI
(`pamongctl migrate --tenant=x`); di server multi-tenant tenant berasal dari request.

## Konsekuensi
- `IdentityDBConfig` dilengkapi (port/user/password/pool). `DBConfig` di-reframe sebagai
  default koneksi tenant DB (dokumentasi & makna berubah; field hampir sama).
- Validasi production: wajibkan kredensial identity DB + default tenant DB; `tenant_id`
  TIDAK lagi wajib (multi-tenant tak punya satu tenant).
- Provisioning (PR-2.2.3) connect ke host target dengan kredensial bersama, `CREATE
  DATABASE`, migrate, lalu tulis registry. Routing per request lewat `TenantConnManager`.
- Blok env di CLAUDE.md diperjelas agar konsisten dengan bagian tenant-tier.

## Alternatif yang dipertimbangkan
- **Koneksi tiap tenant di env/file config.** Tidak scale untuk ratusan tenant; mustahil
  dikelola; bertentangan dengan tenant-tier (registry sebagai sumber lokasi DB). Ditolak.
- **Simpan kredensial tenant di tabel registry.** Untuk Tier 1/2 kredensial bersama sudah
  cukup; menaruh password mentah di registry menambah permukaan risiko. Ditolak untuk
  sekarang — lihat keputusan tertunda.

## Keputusan tertunda
- **Tier 3 (server VPS milik pemda)** bisa pakai kredensial berbeda dari kredensial
  bersama. Saat itu dibutuhkan, registry menyimpan *referensi* kredensial (atau integrasi
  secret manager), bukan password mentah. Tidak dibangun sekarang.
- Siapa yang berhak `CREATE DATABASE` saat provisioning (kredensial admin vs app) —
  diputuskan di PR-2.2.3.
