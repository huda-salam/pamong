# PRD: pamongctl — CLI Framework

## Tujuan
Menyediakan toolchain developer yang menegakkan konvensi sejak lapis paling awal
(scaffolding) dan mengotomasi operasi berulang (migrasi, eject, validasi, tutup buku,
manajemen tenant). CLI adalah Lapis 1 enforcement dari CODING_PHILOSOPHY #1: pelanggaran
dicegah dengan menghasilkan struktur yang sudah benar.

## Konteks & batasan
### Jadi tanggung jawab
- Scaffolding modul & entity; progressive eject; validasi manifest/DAG/permission
- Generate migrasi dari EntityDef; menjalankan migrasi per-tenant
- Menjalankan linter; tutup buku fiskal; manajemen tenant tier; verifikasi audit
### BUKAN tanggung jawab
- Runtime serving (itu binary server)
- Logika bisnis modul

## Perintah & kontrak

### Scaffolding
- `pamongctl new module --name=X [--domain=Y]`
  Menghasilkan struktur hexagonal lengkap: manifest.go, bootstrap.go, domain/, usecase/,
  adapter/{http,db,event}/, workflows/, migrations/. Mengikuti pola modules/surat_masuk.
  DoD: modul baru langsung lulus `validate` & kompilasi (skeleton kosong yang valid).
- `pamongctl define entity --module=X --name=Y`
  Membuat EntityDef Tier 1 (YAML). Tidak ada Go code.
- `pamongctl eject hooks --entity=X.Y`
  Menambah file hooks (Tier 1→2). Tidak menyentuh definisi yang ada.
- `pamongctl eject usecase --entity=X.Y`
  Menambah struktur hexagonal (Tier 2/1→3). YAML tetap source of truth schema.

### Validasi & kualitas
- `pamongctl validate modules`
  Validasi manifest semua modul, DAG dependency (deteksi siklus), permission
  export/import konsisten, event schema terdaftar.
  DoD: siklus dependency / permission tak terdaftar / event tanpa schema → exit non-zero
  dengan pesan menyebut pelanggaran konkret.
- `pamongctl lint [path]`
  Menjalankan seluruh custom analyzer (lihat tools/linter).

### Migrasi
- `pamongctl generate migration --entity=X.Y`
  DDL up/down dari EntityDef (CREATE TABLE + kolom standar + index).
- `pamongctl migrate run --tenant=T | --all [--parallel=N]`
- `pamongctl migrate status` — versi tiap tenant (dari tracking sentral).
- `pamongctl migrate rollback --tenant=T --to=V`
- `pamongctl migrate canary --tenant=T --then=all`
  DoD: migrasi tercatat di gov.migration_history (tenant) + id.tenant_migrations (sentral);
  gagal di satu tenant tidak menghalangi tenant lain.

### Operasional
- `pamongctl fiscal close --tahun=Y --tenant=T [--hard]`
  Soft/hard close + (bila cutoff) buat schema tahun baru + carry-forward + aggregation.
  DoD: idempoten; aman di-resume bila gagal di tengah.
- `pamongctl tenant list [--show-tier]`
- `pamongctl tenant upgrade --tenant=T --to=dedicated_db|dedicated_server --target-host=H`
  pg_dump → restore → update tenant_registry. DoD: kode aplikasi tak berubah; verifikasi
  integritas pasca-migrasi.
- `pamongctl tenant verify --tenant=T` — cek integritas pasca-migrasi tier.
- `pamongctl rule create|preview|activate` — manajemen rule (preview = backtest).
- `pamongctl audit verify [--tenant=T] [--periode=...]` — verifikasi hash chain.

## Kebutuhan non-fungsional
- Scaffolding deterministik (output stabil untuk diff).
- Operasi destruktif (migrate rollback, fiscal close --hard, tenant upgrade) meminta
  konfirmasi eksplisit / flag --yes.
- Semua operasi yang mengubah state mencetak ringkasan & exit code yang jelas.

## Anti-pattern / yang harus dihindari
- Scaffolding yang menghasilkan kode tak lulus linter sendiri.
- Operasi tenant yang mengasumsikan satu DB bersama.
- Migrasi tanpa pasangan down.

## Acceptance criteria
- [ ] `new module` → modul lulus `validate` & kompilasi tanpa edit manual.
- [ ] `eject hooks/usecase` menambah tanpa merusak definisi existing.
- [ ] `validate modules` mendeteksi siklus DAG, permission tak terdaftar, event tanpa schema.
- [ ] `generate migration` menghasilkan up & down yang konsisten.
- [ ] `migrate run --all` tercatat ganda; gagal satu tenant tak menghalangi lain.
- [ ] `fiscal close` idempoten & resumable.
- [ ] `tenant upgrade` memindah tenant tanpa ubah kode; `verify` mengecek integritas.
- [ ] `audit verify` mendeteksi chain putus.
