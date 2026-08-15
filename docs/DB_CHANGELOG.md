# DB_CHANGELOG.md — Riwayat Perubahan Struktur Database

Setiap perubahan struktur database Pamong dicatat di sini, **satu entri per PR**, di PR yang sama
dengan perubahannya. Keadaan **akhir** skema (bentuk berlaku sekarang, lengkap dengan penjelasan)
ada di `docs/DB_SCHEMA.md` — dokumen ini hanya riwayat "apa berubah, kapan, kenapa, apakah
reversibel".

**Yang wajib dicatat** — bukan hanya file `.sql`:
- migrasi ber-file (`core/*/migrations/`, `modules/*/migrations/`, `identity/migrations/`)
- DDL ensure-schema-on-write di kode Go (mis. `tenantrole/adapter/db/schema.go`) — termasuk
  penambahan kolom lewat `ALTER TABLE ... ADD COLUMN IF NOT EXISTS`
- perubahan pada **generator** DDL (`infra/db/ddl.go`) yang mengubah bentuk tabel yang dihasilkan
- perubahan index, constraint, CHECK, dan default — bukan hanya tabel/kolom
- perubahan **cara** skema diterapkan (registrasi migrasi, runner) meski DDL-nya tidak berubah

**Format entri.** Entri baru ditambahkan **di atas** (terbaru dulu). Judul:
`### YYYY-MM-DD · PR-X.Y.Z · commit`, lalu baris metadata, lalu daftar perubahan dengan penanda
`+` (tambah), `~` (ubah), `-` (hapus), diakhiri catatan kompatibilitas.

```markdown
### 2026-01-31 · PR-9.9.9 · `abc1234`
**DB:** tenant | identity · **Jalur:** A (migrasi ber-file) | B (identity) | C (ensure-on-write)
· **Down:** ada | tidak (jalur C)

- `+ gov.contoh` — tabel baru: alasan singkat keberadaannya.
- `~ gov.lain` — `+ kolom_baru TEXT NOT NULL DEFAULT ''`; index `idx_x` diganti `idx_y`.

**Kompatibilitas:** additive, kode lama tetap jalan / butuh backfill / breaking (dua rilis).
```

Jalur A/B/C merujuk tiga cara pembuatan skema yang dijelaskan di `DB_SCHEMA.md` §2.

---

### 2026-08-15 · PR-W4a (runtime workflow: dispatch, instance, engine per-tenant) · `HEAD`
**DB:** tenant · **Jalur:** A (`core/workflow/migrations/004`+`005`) · **Down:** ada

- `+ gov.workflow_instances` — satu baris per jalannya alur untuk satu entitas bisnis. Sebelumnya
  engine sudah lengkap tapi tak punya tempat menyimpan instance sama sekali: transisi hanya mungkin
  dalam satu request (instance hidup di memori pemanggil), dan guard race eskalasi SLA
  (`InstanceStateReader`) tak punya sumber data. Kolom: `id` (PK), `tenant_id`, `definition_id`,
  `definition_version` (versi DIKUNCI saat start — PRD F1/F7), `entity_id`, `current_state`,
  `role_bindings JSONB DEFAULT '{}'` (salinan BEKU pilihan tenant saat StartFromTemplate, ADR-012),
  `history JSONB DEFAULT '[]'` (append-only), `version INT DEFAULT 0` (optimistic locking),
  `started_at`, `updated_at`. Index: `uq_wfinst_entity_definition UNIQUE (definition_id, entity_id)`
  — pagar otorisasi (tanpanya alur yang sudah selesai bisa dimulai ulang lalu action-nya dijalankan
  lagi), dan `idx_wfinst_state (definition_id, current_state)` untuk papan kerja per state.

  Riwayat sengaja JSONB pada baris yang sama, bukan tabel terpisah: ia selalu dibaca UTUH bersama
  instance-nya dan tak pernah di-query lintas instance. Immutabilitasnya dijaga jalur tulis (hanya
  append) + `gov.audit_logs`, bukan constraint DDL.

- `+ gov.workflow_instance_locks` — kunci transisi per instance, ber-SEWA (ADR-022 Keputusan 5):
  `instance_id` (PK), `token UUID NOT NULL` (release hanya oleh pemegang), `locked_until TIMESTAMPTZ
  NOT NULL` (jam DATABASE), index `idx_wfinst_lock_expiry (locked_until)`. Acquire atomik lewat
  `INSERT .. ON CONFLICT DO UPDATE ... WHERE locked_until < now() RETURNING token` — bentuk yang
  sama dengan `gov.job_locks`.

  Bentuk pertama W4a memakai `pg_try_advisory_xact_lock` tanpa tabel tambahan, dan itu ditinggalkan
  sebelum rilis: kunci tingkat sesi menuntut transaksinya tetap terbuka selama transisi, sementara
  action di bawah kunci memakai POOL YANG SAMA — setiap transisi menyandera satu koneksi selama use
  case bekerja, dan transisi bersamaan sebanyak ukuran pool menggantungkan seluruh request tenant
  itu. Harga yang dibayar: kunci baris tak ikut mati bersama koneksinya, jadi ia butuh batas sewa
  (5 menit) agar proses yang mati tak menyandera instance selamanya.

- `~ gov.workflow_definitions`, `gov.tenant_workflow_configs` — bentuk tabel TIDAK berubah, tapi
  keduanya kini benar-benar diisi & dibaca di server hidup: `cmd/server` merakit satu tumpukan
  workflow per tenant (`workflowFactory`) yang menjalankan `EnsureSchema` + seed definisi baseline
  modul (dari FS ter-embed) ke DB tenant saat tenant itu pertama memakai workflow.

**Kompatibilitas:** additive. Tabel baru belum pernah dipakai produksi, jadi tak ada backfill.

---

### 2026-08-11 · PR-W3b (wiring Authority + permukaan IAM tenant) · `HEAD`
**DB:** tenant · **Jalur:** C (ensure-on-write) · **Down:** tidak (jalur C)

Tak ada tabel/kolom/index yang berubah bentuknya. Yang berubah adalah **cara skema diterapkan**,
dan itu masuk lingkup dokumen ini.

- `~ gov.audit_logs` — kini dipastikan ada **saat penulisan audit pertama per tenant**
  (`AuditRepo.EnsureSchema` + `db.SchemaMemo`, memo per proses ber-kunci KONEKSI lewat
  `db.DBKeyer` — `*Pool` konstan, `*TenantRoutingConn` per tenant), bukan hanya lewat `EnsureSchema`
  saat boot. Seluruh DDL ensure-on-write (`gov.audit_logs`, `gov.tenant_roles`, `gov.delegations`,
  `gov.org_units`) kini berjalan di bawah `pg_advisory_xact_lock` lewat `db.EnsureSchemaLocked`,
  karena `IF NOT EXISTS` TIDAK atomik dan jalur ini kini dipicu dari jalur request. Alasannya
  struktural: audit tenant baru punya penulis produksi di PR ini (mutasi `/admin/iam/*`), dan
  tenant ditemukan saat request sehingga boot tak bisa membuat tabelnya untuk semua tenant.
  DDL dijalankan DI LUAR transaksi chain agar tak memperpanjang `pg_advisory_xact_lock`.
- `~ gov.tenant_roles`, `gov.tenant_role_permissions` — DDL-nya tidak berubah, tapi penulisnya
  kini bisa dirutekan per-tenant: `TenantRoleRepo` menerima `db.TxConn` (dipenuhi `*db.Pool` DAN
  `*db.TenantRoutingConn`) alih-alih `*db.Pool`. Sebelumnya repo ber-transaksi terikat satu DB
  yang dipilih saat boot — yaitu tak bisa dipakai di jalur request multi-tenant sama sekali.

**Kompatibilitas:** additive & tak reversibel-perlu — tak ada bentuk tabel yang berubah, tak ada
backfill. Tenant lama yang tabel auditnya sudah ada tidak terpengaruh (DDL `IF NOT EXISTS`).

---

### 2026-08-10 · PR-W2 (handler admin identity) · `HEAD`
**DB:** identity · **Jalur:** B (`identity/migrations/010`) · **Down:** ada

- `+ id.persons` **satu baris seed** — person SENTINEL `00000000-0000-0000-0000-000000000001`
  (`nama_lengkap = 'SYSTEM (sentinel)'`, `is_active = false`). Bentuk tabel TIDAK berubah; yang
  bertambah data. Dicatat di sini karena ia menjadi **prasyarat struktural**: dua FK
  (`id.tenant_assignments.assigned_by`, `id.central_role_assignments.assigned_by`) NOT NULL ke
  `id.persons(id)` membuat penugasan PERTAMA mustahil — admin pertama tak punya siapa pun yang
  bisa menugaskannya. Alternatifnya (FK dilonggarkan / kolom dibuat NULL-able) menghapus
  ketelusuran SELURUH baris demi satu baris pertama. Padanan Go: `identity/domain.SystemActorID`.
- `nik_enc`/`nik_bidx` baris sentinel diisi **bytea zero-length**, bukan NULL (kolomnya NOT NULL
  dan migrasi tak punya akses KeyProvider). Sifat itu diandalkan, bukan ditoleransi:
  `crypto.FieldSealer.Open` memetakan kolom kosong → string kosong (jadi `FindByID` tak error),
  sementara `FindByNIK("")` TIDAK menemukannya karena blind index dari `""` adalah HMAC — bukan
  bytes kosong. `uq_persons_nik_bidx` tetap utuh: person nyata selalu ber-NIK 16 digit.
- `INSERT ... ON CONFLICT (id) DO NOTHING` — identity DB belum punya runner ber-tracking (jalur B
  dijalankan ops), jadi migrasi ini harus aman dijalankan ulang.

**Kompatibilitas:** additive murni (satu baris data, nol DDL). Down menghapus baris sentinel dan
akan DITOLAK FK bila sudah ada penugasan yang merujuknya — perilaku yang benar: menghapusnya
diam-diam memutus ketelusuran baris tersebut.

**Catatan jalur C:** PR ini juga menjadi pemanggil produksi PERTAMA
`identity/adapter/db.AuditStore.EnsureSchema` (`id.audit_logs`, jalur C, DDL tak berubah). Sampai
sekarang tabel itu hanya pernah lahir di test — mutasi identity lewat `/admin/identity/*` adalah
penulis produksi pertamanya.

---

### 2026-08-03 · PR-3.8.5b (payload event + cache idempotency) · `13e5860`
**DB:** tenant · **Jalur:** — (tanpa DDL; bentuk tabel tidak berubah) · **Down:** tidak berlaku

Menutup dua jalur samping terakhir yang masih menyimpan pengenal plaintext (ADR-009 §6 butir 2 &
3). Tak ada satu pun kolom bertambah, berubah tipe, atau hilang — yang berubah **isi** dua kolom
yang sudah ada. Dicatat di sini justru karena itu: bentuk tabel yang tak berubah membuat
perubahannya tak terlihat dari `\d`, sementara arti kolomnya bergeser total.

- `~ gov.outbox_events.payload` — **tanpa DDL**: JSONB-nya berhenti memuat pengenal karena
  payload event identity kehilangan field-nya (`PersonDibuatPayload.NIK`,
  `EmploymentDibuatPayload.NIP`, `EmploymentDitugaskanPayload.{NIK,NIP,Email,NoHP}`). Payload
  TIDAK disegel — nilainya DIHAPUS; `identity/sync` memintanya lewat `CloneSource` ke identity DB
  saat event ditangani. Alasan lengkap: **ADR-018**.
- `~ gov.user_profiles.nama_lengkap` — **tanpa DDL**: yang bergeser ARTINYA. Nilainya kini
  dibaca dari identity saat event DITANGANI (bersama keempat pengenal), bukan disalin dari
  payload event saat terbit. Sebelumnya satu baris clone mencampur dua basis waktu; sekarang
  seluruh baris seumur. `NamaLengkap` tetap ada di payload event, tapi hanya informasional
  (keterbacaan operator saat memeriksa outbox/DLQ) — bukan sumber kebenaran clone (ADR-018 #2).
- `~ gov.idempotency_keys.response` — **tanpa DDL** (sudah BYTEA): kini ciphertext, realm
  **tenant**, purpose `idempotency_response`, **tanpa blind index** (nilai ini tak pernah dicari;
  bidx atasnya hanya oracle kesamaan antar respons). Koordinat AAD baris (ADR-016) diturunkan
  deterministik dari KEDUA bagian PK — `uuid.NewSHA1(person_id, "gov.idempotency_keys\0"+key)` —
  karena tabel ini tak punya kolom UUID; dari `person_id` saja respons bisa dipindah antar key
  milik orang yang sama dan tetap terbuka.
- `~ id.data_keys` — **tanpa DDL**: bertambah baris realm tenant untuk purpose
  `idempotency_response` (dibuat otomatis saat purpose dipakai pertama kali).

**Kompatibilitas:** additive di kawat, **breaking pada data yang sudah ada**. Baris
`gov.idempotency_keys` yang ditulis SEBELUM rilis ini memuat respons plaintext dan akan DITOLAK
saat dibaca (`PurposeOf` gagal pada non-ciphertext). Yang terdampak **hanya cabang replay**
(entri selesai DAN fingerprint sama): di sana `Reserve` gagal lantang → middleware 503 → klien
retry. Verdict yang tak butuh badan respons — key dipakai-ulang untuk request berbeda (422) dan
kembar in-flight (409) — tetap terbit apa adanya, karena `Reserve` tak membuka blob di luar
cabang replay. Dampak nyata nihil: TTL entri completed 24 jam dan replay bukan jalur kebenaran.
Bila tak ingin ada 503 sama sekali, `DELETE FROM gov.idempotency_keys` saat deploy.

**Sisa plaintext yang TIDAK dibersihkan mesin ini** (runbook, bukan kode): pesan yang terlanjur
mengendap di stream NATS retensi dan baris `gov.outbox_events` lama tetap memuat pengenal —
kelas yang sama dengan catatan `DROP COLUMN`/heap-tuple di PR-3.8.5a. Nol dampak sekarang:
`NewOutboxStore` belum punya pemanggil produksi dan clone nol baris di semua env.

### 2026-08-01 · PR-3.8.5a (jalur clone) · `7485fc8`
**DB:** tenant · **Jalur:** C (`identity/sync/writer_tenantdb.go`) · **Down:** tidak (jalur C)

PR-3.8.6 mengenkripsi pengenal di identity DB, tapi clone `gov.user_profiles` masih menyalinnya
**plaintext ke setiap tenant DB** tempat person itu ditugaskan (fat event, PR-N3b/ADR-013). Entri
ini menutup jalur samping itu (ADR-009 §6): NIK yang terlindungi di pusat tapi terbaca di dump
tenant bukan NIK yang terlindungi.

- `~ gov.user_profiles` — `- nik`, `- nip`, `- email`, `- no_hp` (kolom plaintext **DI-DROP**,
  pendirian sama dengan migrasi 009); `+ nik_enc BYTEA NOT NULL`, `+ nik_bidx BYTEA NOT NULL`,
  `+ nip_enc/nip_bidx BYTEA`, `+ email_enc/email_bidx BYTEA`, `+ no_hp_enc/no_hp_bidx BYTEA`.
  `+ idx_user_profiles_nik_bidx`, `+ idx_user_profiles_nip_bidx` (**non-unik** — keunikan
  ditegakkan di sumber, bukan di proyeksi).
- `~ id.data_keys` — **tanpa DDL**: bertambah baris realm **tenant** untuk purpose
  `nik`/`nip`/`email`/`no_hp`. Realm clone adalah TENANT-nya, bukan `_central` (ADR-017 §1 hanya
  berlaku untuk data identity yang memang tak punya tenant).

**Kompatibilitas: BREAKING untuk tenant yang sudah punya baris clone** — dan itu disengaja.
Dua akibat yang harus dibaca utuh:

1. `ADD COLUMN` lewat ALTER menghasilkan kolom **nullable** (NOT NULL menuntut backfill; backfill
   bukan pekerjaan ensure-on-write). Hanya tenant baru yang mendapat NOT NULL dari `CREATE TABLE`.
2. `DROP COLUMN` **menghilangkan nilai plaintext lama tanpa memindahkannya** ke kolom `_enc`.
   Pemulihannya adalah **menerbitkan ulang event penugasan** (`identity.employment.ditugaskan`)
   agar writer menulis ulang clone — bukan membaca balik kolom yang justru harus hilang. Clone
   adalah proyeksi; sumbernya utuh di identity DB.

Seperti PR-3.8.6, urutan ini murah **hanya selama belum ada tenant berisi**: nol baris clone di
seluruh env saat ini (tak ada manifest deploy di repo, dev & CI membangun tenant DB dari nol tiap
run). Sesudah ada data produksi, langkah yang sama menuntut backfill terjadwal per tenant.

**Window rollout — yang menerapkan ALTER hanyalah PENULIS.** `identity/sync.TenantDBWriter`
meng-ensure skema saat write pertama; pembacanya (`infra/user.DBResolver`, `infra/notification`)
tidak pernah menulis DDL, dan writer itu sendiri **belum di-wire di `cmd/server`** (subscriber
sync belum dipasang — gap terpisah). Akibatnya, pada tenant yang tabel clone-nya dibuat versi
lama, `DBResolver` gagal lantang (`column "nik_enc" does not exist`) sampai ada write clone.
Itu **perilaku yang dipilih, bukan kelalaian**: resolver sengaja tak memprobe
`information_schema` seperti jalur kontak, karena probe akan mengubah "skema tertinggal" menjadi
"user tidak ditemukan" — kegagalan senyap pada jalur otorisasi. Urutan onboarding yang benar:
sync jalan lebih dulu, baru resolver melayani.

---

### 2026-08-01 · PR-3.8.6 + E2 · `05875c4`
**DB:** identity · **Jalur:** B (identity, `009_encrypt_identity_identifiers`) · **Down:** ada
(bentuk saja — lihat catatan)

Pengenal identity berpindah ke `_enc` + `_bidx`, dan UNIQUE-nya ikut pindah ke blind index
(ADR-009 §2, ADR-017). Kunci berasal dari **realm sentral** `_central`, bukan dari tenant mana
pun: data identity tak punya tenant, dan `UNIQUE(nik)` berlaku global se-identity-DB sehingga
kunci blind index per-tenant akan membuatnya berhenti menangkap duplikat.

- `~ id.persons` — `- nik`, `- no_hp`, `- email` (kolom plaintext DI-DROP, bukan dibiarkan
  berdampingan); `+ nik_enc BYTEA NOT NULL`, `+ nik_bidx BYTEA NOT NULL`,
  `+ no_hp_enc/no_hp_bidx BYTEA`, `+ email_enc/email_bidx BYTEA`.
  `UNIQUE (nik)` → `uq_persons_nik_bidx (nik_bidx)`;
  `+ idx_persons_no_hp_bidx`, `+ idx_persons_email_bidx` (non-unik, seperti sebelumnya).
- `~ id.employments` — `- nip`; `+ nip_enc BYTEA`, `+ nip_bidx BYTEA`.
  `UNIQUE (nip)` → `uq_employments_nip_bidx (nip_bidx)`.
  CHECK tanpa nama `employments_check` diganti `employments_nip_status_check`, kini menuntut
  **kedua** kolom konsisten dengan `status` — agar tak ada baris ber-indeks tanpa nilai.
- `~ id.credentials` — `- cred_value`; `+ cred_value_enc BYTEA NOT NULL`,
  `+ cred_value_bidx BYTEA NOT NULL`.
  `UNIQUE (cred_type, cred_value)` → `uq_credentials_type_value_bidx (cred_type, cred_value_bidx)`.
  `cred_type` **tetap plaintext** — jenis kredensial, bukan pengenal orang; itulah yang menjaga
  keunikan tetap per-tipe dan `FindByTypeValue` tetap satu query.
- `~ id.data_keys` — **tanpa DDL**: kolom `tenant_id` kini juga memuat identitas realm non-tenant
  `_central` (ADR-017 §1). Kolom itu memang sejak awal tanpa FK ke `id.tenant_registry`.
- `~ id.audit_logs` — **tanpa DDL**: (a) partisi hash chain identity berubah nilai dari
  `"central"` menjadi `_central`; (b) nilai `personal_id` di kolom `diff` kini tersimpan
  terenkripsi (base64 ciphertext) untuk `identity.Person` & `identity.Employment` — penutupan
  REVIEW_BACKLOG E2.

**Kompatibilitas:** **breaking**, dan sengaja tanpa jalur backfill. Diverifikasi 1 Agu 2026
bahwa identity DB kosong di SELURUH environment (tak ada deployment staging/production; dev &
CI tanpa schema `id`), jadi tak ada satu baris pun untuk dimigrasi. Sesudah ada data, perubahan
yang sama menuntut pipeline bertahap (tambah kolom → backfill ber-kunci → tukar constraint →
drop) dengan window kompatibilitas dua rilis, DAN migrasi hash chain audit karena nilai partisi
ikut berubah.

**Penggantian sentinel chain audit `"central"` → `_central` ikut bersandar pada premis kosong
itu — dan itu perlu dinyatakan tersendiri.** Premis "identity DB kosong" di atas ditulis untuk
dua hal lain: backfill pengenal dan down-migration. Penggantian sentinel adalah perubahan
KETIGA yang menumpang padanya, tanpa DDL dan karena itu tanpa file migrasi yang menandainya.
`id.audit_logs` dipartisi hash chain per `tenant_id`, dan identity memakai satu nilai sentinel
karena datanya tak ber-tenant; mengganti nilai itu memulai chain BARU dan meninggalkan chain
lama menggantung. Selama tabelnya kosong, "chain lama" tidak ada — tak ada baris, tak ada head,
tak ada yang putus. Aman karena FAKTA (nol baris), bukan karena penggantian sentinel itu sendiri
tak berbahaya.

Pada identity DB BERISI, urutannya terbalik: repartisi chain lebih dulu, baru pengenal. Menjalankan
migrasi ini apa adanya akan membuat verifikasi chain melaporkan diskontinuitas pada batas
`"central"`→`_central` — persis gejala yang dirancang untuk berarti "audit log dirusak", sehingga
kegagalannya bukan hanya teknis tapi merusak arti sinyal integritasnya. Alasan nilainya harus
`_central` dan bukan `"central"` ada di ADR-017 §1 (token ber-`_` mustahil bertabrakan dengan
tenant nyata, yang wajib cocok `^[a-z]…`); yang tak boleh dilakukan adalah menggantinya diam-diam
di atas data yang sudah ada.

**Down mengembalikan BENTUK, bukan DATA.** Kolom plaintext dibuat ulang kosong; nilai di `_enc`
tak dipulihkan (memulihkannya butuh kunci dari KeyProvider — bukan urusan migrasi SQL). Pada DB
berisi data ia gagal sejak awal karena `NOT NULL`. Aman hanya selama identity DB kosong, yaitu
kondisi yang membuat migrasi up ini murah sejak awal.

**Perubahan semantik yang menumpang di sini** (bukan struktur, tapi menentukan siapa bisa
login): purpose kunci kredensial diturunkan dari `cred_type`, sehingga kredensial email masuk
tabel normalisasi framework → **login lewat email menjadi case-insensitive** dan `UNIQUE` mulai
menangkap `Budi@x.id` vs `budi@x.id` sebagai duplikat (ADR-017 §4). `oauth` tidak ikut di-fold.

### 2026-08-01 · PR-3.8.9 · `4f5ee87`
**DB:** tenant · **Jalur:** — (tidak ada DDL) · **Down:** tidak berlaku

Dicatat meski **bukan perubahan struktur**: tak ada tabel, kolom, index, constraint, maupun
generator DDL yang berubah. Yang berubah adalah **isi** kolom yang sudah ada, secara tidak
kompatibel — dan operator yang menemukan blob lama berhenti terbuka harus bisa menemukan
sebabnya di sini, bukan menebak antara "kunci hilang" dan "data rusak".

- `~` isi `{field}_enc` (semua schema modul) dan nilai sensitif di `gov.audit_logs.diff` —
  format ciphertext naik `0x01` → `0x02` dan AAD kini mengikat **identitas baris**
  (`record_id`) di samping tenant/purpose/versi kunci (ADR-016). `record_id` ada di AAD saja,
  tidak disimpan di dalam blob.
- `~` `{field}_bidx` **tidak berubah** — blind index wajib row-independent, kalau tidak
  `WHERE {f}_bidx = $1` berhenti cocok dan `UNIQUE` berhenti menangkap duplikat.

**Kompatibilitas:** **breaking untuk data yang sudah tertulis.** Blob format `0x01` dikenali
parser (agar `PurposeOf` menjawab dan jalur baca audit menampilkan penanda, bukan blob mentah)
tapi ditolak `Decrypt` dengan pesan yang menyebut re-enkripsi. Tidak ada backfill yang
disediakan karena belum ada tenant produksi — itulah gerbang keras ROADMAP 3.8 untuk PR ini.
Bila ada DB dev berisi entity ber-field terenkripsi, buang/isi ulang datanya.

**Radius kegagalannya se-HALAMAN, bukan se-baris:** satu blob yang tak terbuka menggagalkan
seluruh panggilan `List`, bukan hanya baris itu — jadi satu baris v1 yang tertinggal membuat
daftarnya kosong total, tanpa halaman yang bisa dilewati. Itu perilaku yang disengaja (lihat
`SQLRepository.List`: melewati baris akan menjadikan perusakan ciphertext sebagai alat
penyembunyian). Errornya menyebut id baris agar bisa ditindak.

---

### 2026-07-30 · PR-3.8.2 · `fa6e51c`
**DB:** identity · **Jalur:** B · **Down:** ada

- `+ id.data_keys` — DEK ter-wrap untuk enkripsi field selektif. PK
  `(tenant_id, purpose, kind, key_version)`; unique partial `uq_data_keys_active … WHERE is_active`
  agar tepat satu versi aktif per (tenant, purpose, kind). Sentral secara sengaja: dump satu tenant
  DB tidak memuat kunci apa pun (ADR-010 §2).
- `~ id.tenant_registry` — `+ key_custody VARCHAR(10) NOT NULL DEFAULT 'platform'`
  CHECK `('platform','tenant')`. Custody KEK adalah kebijakan per-tenant, bukan env global (ADR-010 §3).

**Kompatibilitas:** additive. Nilai `key_custody='tenant'` sengaja ditolak lantang oleh resolver
sampai driver KeyProvider pemda hadir (PR-3.8.8) — tidak diam-diam jatuh ke `platform`.

---

### 2026-07-28 · PR-3.8.1 · `fdb1a78`
**DB:** tenant (schema modul) · **Jalur:** generator DDL · **Down:** mengikuti migrasi yang di-generate

- `~ infra/db.GenerateMigration` — field ber-`DataClass` `personal_id`/`specific` kini
  menghasilkan **dua kolom fisik**: `{field}_enc` (ciphertext) dan `{field}_bidx` (blind index HMAC)
  bila Searchable/Unique. Nilai plaintext tidak lagi dihasilkan sebagai kolom.
- `~` `UNIQUE` untuk field terenkripsi dipindah ke kolom `_bidx` (nonce GCM acak membuat `_enc`
  tak pernah sama untuk nilai yang sama), plus index untuk `_bidx` Searchable non-Unique.

**Kompatibilitas:** hanya memengaruhi migrasi yang **di-generate setelah** PR ini; tidak ada tabel
existing yang berubah (belum ada entity ber-field terenkripsi saat itu). Lapis enkripsi di
repository menyusul (ROADMAP 3.8.3+).

---

### 2026-07-28 · PR-5.1.x · `aada7a2`
**DB:** tenant · **Jalur:** A (`core/sequence/migrations/001`) · **Down:** ada

- `+ gov.sequences` — sumber nomor berurut atomik per-tenant. PK `(name, tahun)`; `tahun` masuk PK
  sehingga reset per tahun fiskal bersifat intrinsik (tanpa job reset). Sengaja **tanpa** kolom
  `tenant_id`: isolasi sudah struktural dari DB-per-tenant.

**Kompatibilitas:** additive.

---

### 2026-07-27 · PR-N3b · `edaf3e2`
**DB:** tenant · **Jalur:** C (`identity/sync/writer_tenantdb.go`) · **Down:** tidak

- `~ gov.user_profiles` — `+ email VARCHAR(255)`, `+ no_hp VARCHAR(15)` untuk routing notifikasi
  email/SMS (ADR-013). Ditulis sebagai `ALTER TABLE … ADD COLUMN IF NOT EXISTS` **selain**
  `CREATE TABLE IF NOT EXISTS`, karena jalur C tidak akan menambah kolom ke tabel yang sudah ada.

**Kompatibilitas:** additive & idempoten. Kontak berkelas `personal_id`; enkripsi DEFERRED (ROADMAP 3.8).
Menyentuh `identity/sync` → butuh review ekstra.

---

### 2026-07-27 · PR-5.1.2b · `b7a4d0e`
**DB:** tenant · **Jalur:** A (`core/idempotency/migrations/001`) · **Down:** ada

- `+ gov.idempotency_keys` — PK **gabungan** `(person_id, key)`: idempotency di-scope ke principal
  agar satu user tak bisa membaca/menimpa respons user lain dengan menebak key. `fingerprint`
  mendeteksi key yang dipakai ulang untuk request berbeda. Index `idx_idempotency_expires` untuk
  pembersihan.

**Kompatibilitas:** additive.

---

### 2026-07-26 · PR-3.4.1 · `d84e852`
**DB:** tenant · **Jalur:** A (`core/customization/migrations/001`,`002`) · **Down:** ada

- `+ gov.tenant_custom_fields` — custom field per-tenant; kolom pencarian eksplisit + ter-index,
  hanya bentuk varian per-tipe yang masuk JSONB `field_def`. Index parsial `WHERE is_active`.
- `+ gov.tenant_capability_overrides` — hanya override eksplisit; ketiadaan baris = pakai
  `DefaultEnabled`, bukan "nonaktif".

**Kompatibilitas:** additive. Catatan: kedua migrasi ini diterapkan lewat `db.ApplyEmbeddedSchema`
saat store pertama dipakai, dan **belum** terdaftar di `infra/schema.coreComponents` — sehingga
belum ikut `pamongctl migrate`.

---

### 2026-07-25 · `b6fd5e2`
**DB:** tenant · **Jalur:** perubahan cara penerapan (tanpa perubahan DDL) · **Down:** —

- `~` Migrasi komponen `core/*` disatukan ke migrator lewat `go:embed`
  (`infra/schema.CoreMigrations`), sehingga tercatat di `gov.migration_history` dan bisa di-`down`.
  Sebelumnya `pamongctl migrate` hanya memuat direktori `modules/`. `EnsureSchema` di store
  di-dedup agar tidak menjadi sumber skema kedua.

**Kompatibilitas:** tidak ada perubahan struktur; hanya tracking & reversibilitas yang membaik.

---

### 2026-07-23 · PR-3.6.1 · `13d31c0`
**DB:** tenant · **Jalur:** A (`core/notification/migrations/001`) · **Down:** ada

- `+ gov.notification_templates` — `tenant_id=''` berarti template global; pemilihan
  "paling cocok" ada di TemplateEngine, tabel hanya menyimpan kandidat.
- `+ gov.notification_inapp` — kotak masuk in-app, index `(tenant_id, person_id, created_at DESC)`.
- `+ gov.notification_deliveries` — satu baris per upaya kirim per channel (audit "kenapa tak sampai").

**Kompatibilitas:** additive. Yang tersimpan hanya konten & data — channel tetap kode Go ter-registry.

---

### 2026-07-23 · PR-3.5.2 · `3af2353`
**DB:** tenant · **Jalur:** A (`core/scheduler/migrations/002`) · **Down:** ada

- `+ gov.job_locks` — lock ber-**sewa** (`locked_until`), bukan lock permanen: baris kedaluwarsa
  boleh diambil alih sehingga instance yang mati tidak menyebabkan deadlock abadi. `token` menjaga
  hanya pemegang saat ini yang boleh release.

**Kompatibilitas:** additive.

---

### 2026-07-23 · PR-3.5.1 · `c718a11`
**DB:** tenant · **Jalur:** A (`core/scheduler/migrations/001`) · **Down:** ada

- `+ gov.scheduled_jobs` — menyimpan `job_key` (rujukan handler ter-registry), cron, dan payload;
  bukan logika. `cron_expr` kosong = one-shot (bentuk yang dipakai deadline SLA workflow).
  Index parsial `(next_run_at) WHERE enabled`.
- `+ gov.job_runs` — riwayat eksekusi; `schedule_id` ON DELETE SET NULL agar riwayat ad-hoc/yatim
  tetap tersimpan.

**Kompatibilitas:** additive.

---

### 2026-07-23 · PR-3.3.2b · `9964d26`
**DB:** tenant · **Jalur:** A (`core/workflow/migrations/003`) · **Down:** ada

- `~ gov.tenant_workflow_configs` — `+ version INT NOT NULL DEFAULT 1`,
  `+ effective_from TIMESTAMPTZ NOT NULL DEFAULT now()`; PK lama `(tenant_id, slot)` di-drop dan
  diganti `uq_twc_version (tenant_id, slot, version)` + index `idx_twc_lookup`. Pilihan template
  menjadi append-only (sebelumnya `Set` = UPSERT yang menghapus pilihan lama).
- Backfill: baris lama menjadi versi 1 dengan `effective_from = set_at`.

**Kompatibilitas:** ALTER idempoten (`IF NOT EXISTS`/`IF EXISTS`), aman di atas skema PR-3.2.4.
Menutup utang PR-3.2.4 butir (a): pilihan lama kini bisa dibaca & di-rollback.

---

### 2026-07-23 · PR-3.3.3 · `8f5313b`
**DB:** tenant · **Jalur:** A (`core/config/migrations/002`) · **Down:** ada

- `~ gov.tenant_configs` — `+ version`, `+ effective_from`; `uq_tenant_config_scope` di-drop,
  diganti `uq_tenant_config_version (…, version)`. Pilihan config jadi append-only & non-retroaktif
  (titik ekstensi #7).
- Backfill: baris lama menjadi versi 1 dengan `effective_from = set_at`.

**Kompatibilitas:** ALTER idempoten, aman di atas skema PR-3.3.2.

---

### 2026-07-23 · PR-3.3.2 · `28c2d05`
**DB:** tenant · **Jalur:** A (`core/config/migrations/001`) · **Down:** ada

- `+ gov.tenant_configs` — config ber-scope bertingkat (tenant → unit kerja → resource) dengan
  resolusi "paling spesifik menang". CHECK `ck_tenant_config_scope` melarang resource tanpa unit;
  keunikan memakai `UNIQUE NULLS NOT DISTINCT` (Postgres 15+) agar dua baris scope-tenant untuk key
  sama dianggap konflik.

**Kompatibilitas:** additive. Menggantikan `MemorySelectionSource` sebagai sumber pilihan strategy.
Skema sengaja kaya sejak awal agar scope bisa diperdalam tanpa migrasi.

---

### 2026-07-20 · PR-3.2.4 · `8e0fb59`
**DB:** tenant · **Jalur:** A (`core/workflow/migrations/002`) · **Down:** ada

- `+ gov.tenant_workflow_configs` — pilihan template workflow per-tenant + `role_bindings` (JSONB).
  Disimpan utuh satu baris, bukan dilebur ke KV config (role binding adalah map, bukan skalar).
  Tanpa FK ke `gov.workflow_definitions` karena template bisa dibuat setelah config ditetapkan.

**Kompatibilitas:** additive. PK `(tenant_id, slot)` saat itu berarti `Set` = UPSERT — di-versi-kan
di PR-3.3.2b.

---

### 2026-06-28 · PR-3.2.3 · `9422868`
**DB:** tenant · **Jalur:** A (`core/workflow/migrations/001`) · **Down:** ada

- `+ gov.workflow_definitions` — PK `(workflow_id, version)`: setiap perubahan definisi menambah
  baris baru, baris lama tidak pernah dihapus agar instance berjalan tetap mengacu ke versi saat
  ia mulai. `states`/`transitions` JSONB = struktur & kondisi, bukan kode.
  Index `idx_wfdef_lookup (workflow_id, version DESC)`.

**Kompatibilitas:** additive.

---

### 2026-06-28 · PR-3.1.4 · `0e64d83`
**DB:** tenant · **Jalur:** C (`infra/eventbus/outbox.go`) · **Down:** tidak

- `~ gov.outbox_events` — `+ next_retry_at TIMESTAMPTZ`, `+ failed_at TIMESTAMPTZ` (DLQ + backoff),
  ditulis sebagai `ALTER … ADD COLUMN IF NOT EXISTS`.
- `~` index `idx_outbox_pending` di-drop & dibuat ulang menjadi
  `(next_retry_at NULLS FIRST, created_at) WHERE dispatched_at IS NULL AND failed_at IS NULL`,
  agar relay hanya men-scan baris yang benar-benar siap kirim.

**Kompatibilitas:** additive & idempoten.

---

### 2026-06-28 · PR-3.1.2 · `408e97f`
**DB:** tenant · **Jalur:** C (`infra/eventbus/outbox.go`) · **Down:** tidak

- `+ gov.outbox_events` — event ditulis dalam transaksi bisnis yang sama; relay mengirim setelah
  commit. Inilah yang membuat "operasi gagal tidak mempublish event" benar secara struktural.

**Kompatibilitas:** additive.

---

### 2026-06-28 · PR-2.4.4 · `9223296`
**DB:** identity · **Jalur:** B (`identity/migrations/006`) · **Down:** ada

- `+ id.otps` — OTP login citizen; `code_hash` bcrypt (tidak pernah plaintext), sekali-pakai
  (`consumed_at`), berumur pendek (`expires_at`), tebakan dibatasi (`attempts`).
  Index `(credential_id, created_at DESC)` karena verifikasi selalu mengambil OTP terbaru.

**Kompatibilitas:** additive. Rate-limit per-kredensial ada di infra (memory), bukan tabel.

---

### 2026-06-27 · PR-2.4.1 · `98982f0`
**DB:** identity · **Jalur:** B (`identity/migrations/005`) · **Down:** ada

- `+ id.revoked_tokens` — denylist per-`jti`; `expires_at` = `exp` token sehingga baris boleh
  dipurge setelahnya. "Cabut semua token satu person" (epoch) belum ada — DEFERRED.

**Kompatibilitas:** additive.

---

### 2026-06-27 · PR-2.3.5 · `cf82d98`
**DB:** tenant · **Jalur:** C (`tenantrole/adapter/db/hierarchy.go`, `delegation/adapter/db/schema.go`,
`tenantrole/adapter/db/schema.go`) · **Down:** tidak

- `+ gov.org_units` — hierarki OPD adjacency list (`parent_id`). Adjacency dipilih ketimbang
  closure/`ltree`: tree OPD dangkal & jarang bermutasi, dan `ltree` butuh ekstensi yang tak terjamin
  di Tier 3. Placeholder untuk modul OPD penuh yang akan memilikinya lewat port `Hierarchy` sama.
- `+ gov.delegations` — delegasi/PLT berwaktu; `valid_until` **NOT NULL** menegakkan invariant
  "delegasi selalu berbatas waktu" di level DB.
- `~ gov.user_role_assignments` — `+ unit_kerja_id UUID`, `+ include_subtree BOOLEAN` untuk scope ABAC.

**Kompatibilitas:** additive. Catatan jalur C: penambahan kolom pada `user_role_assignments`
ditulis di dalam `CREATE TABLE IF NOT EXISTS` **tanpa** `ALTER` pendamping — DB yang sudah
terlanjur memiliki tabel versi PR-2.3.3 tidak memperoleh kolom baru (belum ada deployment saat itu;
pola `ALTER … IF NOT EXISTS` baru dibakukan sejak PR-3.1.4).

---

### 2026-06-27 · PR-2.3.3 · `77b1bf6`
**DB:** tenant · **Jalur:** C (`tenantrole/adapter/db/schema.go`) · **Down:** tidak

- `+ gov.tenant_roles`, `+ gov.tenant_role_permissions`, `+ gov.user_role_assignments` — role
  tenant (Lapisan 2) hidup di tenant DB, tidak dikenal di luar tenant. Cermin bentuk RBAC kanonik
  `id.central_role_*`.
- FK `user_role_assignments.user_id → gov.user_profiles` **sengaja tidak dipasang**: keduanya
  ensure-on-write tanpa jaminan urutan pembuatan. Resolver tidak butuh JOIN ke `user_profiles`.

**Kompatibilitas:** additive.

---

### 2026-06-26 · PR-2.3.2 · `dce7c57`
**DB:** identity · **Jalur:** B (`identity/migrations/004`) · **Down:** ada

- `+ id.central_roles` — `scope_type` CHECK `('global','scoped')`.
- `+ id.central_role_permissions` — join table grant role→permission (hanya string permission;
  definisinya tetap di manifest modul).
- `+ id.central_role_assignments` — `tenant_scope VARCHAR(100)[]`, sengaja **tanpa FK** ke
  `id.tenant_registry` agar token region (mis. `prov:jatim`) bisa masuk kelak tanpa ubah skema.

**Kompatibilitas:** additive.

---

### 2026-06-26 · PR-2.2.4 · `c57a113`
**DB:** identity + tenant · **Jalur:** B (`identity/migrations/003`) + C
(`identity/sync/writer_tenantdb.go`) · **Down:** ada (identity) / tidak (tenant)

- `+ id.tenant_assignments` — penugasan menempel pada **employment**, bukan person; persona
  `citizen` tidak butuh baris di sini. `is_home_tenant=false` menandai penugasan cross-tenant.
  `UNIQUE (employment_id, tenant_id, valid_from)`.
- `+ gov.user_profiles` (tenant DB) — clone read-only hasil sinkronisasi event; `id` = `person_id`
  = `id.persons.id`. Tanpa kolom credential/password — secret tetap di `id.credentials`.

**Kompatibilitas:** additive. Relasi identity→tenant lewat event + clone, bukan FK lintas DB.

---

### 2026-06-26 · PR-2.2.1 · `cd6e3c1`
**DB:** identity · **Jalur:** B (`identity/migrations/002`) · **Down:** ada

- `+ id.tenant_registry` — lokasi DB tiap tenant (`db_host`, `db_name`, `tier`). Harus sentral:
  resolver butuh tabel ini untuk tahu di DB mana tenant hidup (chicken-and-egg bila di tenant DB).
  Kenaikan tier = UPDATE baris, tanpa perubahan kode aplikasi.

**Kompatibilitas:** additive.

---

### 2026-06-26 · PR-2.1.3 · `5397052`
**DB:** identity · **Jalur:** C (`infra/db/audit.go`, DDL berparameter schema) · **Down:** tidak

- `+ id.audit_logs` — struktur identik `gov.audit_logs`; yang berbeda hanya schema dan partisi hash
  chain (identity memakai partisi konstan → satu chain tunggal, ADR-003).

**Kompatibilitas:** additive; reuse penuh engine & hash chain, tanpa DDL kedua.

---

### 2026-06-25 · PR-2.1.1 · `b2f5a53`
**DB:** identity · **Jalur:** B (`identity/migrations/001`) · **Down:** ada

- `+ id.persons` — anchor identitas di **NIK** (setiap warga punya), bukan NIP.
- `+ id.employments` — kepegawaian opsional; CHECK gabungan menegakkan "NIP hanya milik ASN"
  di level DB. Index `(person_id)`.
- `+ id.credentials` — banyak credential per person, semua resolve ke person yang sama;
  `UNIQUE (cred_type, cred_value)`.

**Kompatibilitas:** skema awal identity DB.

---

### 2026-06-25 · PR-1.3.2 · `cd6b65f`
**DB:** tenant · **Jalur:** C (`infra/db/audit.go`) · **Down:** tidak

- `~ gov.audit_logs` — `+ prev_hash TEXT NOT NULL`, `+ hash TEXT NOT NULL` (hash chain deteksi
  tamper), `+ seq BIGSERIAL` & index `idx_audit_tenant_seq (tenant_id, seq)` untuk verifikasi
  berurut per partisi. Penulisan diserialisasi per partisi lewat `pg_advisory_xact_lock` agar chain
  tidak putus oleh penulisan paralel.

**Kompatibilitas:** additive pada tabel yang belum dipakai produksi.

---

### 2026-06-25 · PR-1.3.1 · `6e5cfab`
**DB:** tenant · **Jalur:** C (`infra/db/audit.go`) · **Down:** tidak

- `+ gov.audit_logs` — append-only; `diff` JSONB field-level dengan masking field sensitif
  (ADR-002). `tenant_id` dipertahankan sebagai kolom partisi meski tenant DB terpisah.

**Kompatibilitas:** additive.

---

### 2026-06-25 · PR-1.2.3 · `9167a58`
**DB:** tenant · **Jalur:** C (`infra/db/migration.go`) · **Down:** tidak

- `+ gov.migration_history` — tracking migrasi per tenant; `UNIQUE (module, version)` dan `checksum`
  untuk mendeteksi file migrasi yang diubah setelah diterapkan.

**Kompatibilitas:** additive; tabel ini prasyarat runner, jadi ia sendiri tidak bisa dilacak olehnya.

---

### 2026-06-25 · Phase 0 · `e92c0cf`
**DB:** tenant (schema modul) · **Jalur:** A (`modules/surat_masuk/migrations/001`) · **Down:** ada

- `+ surat_masuk.surat_masuks` — entity referensi dengan kolom standar framework
  (`id`, `version`, `created_at`, `updated_at`, `deleted_at`); `UNIQUE (nomor_agenda)`;
  index untuk field Searchable.
- `+ surat_masuk.disposisis` — FK `surat_id` ke tabel di schema yang sama (FK intra-modul diizinkan,
  lintas modul tidak).

**Kompatibilitas:** skema awal modul referensi.
