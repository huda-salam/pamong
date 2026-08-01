# DB_SCHEMA.md — Struktur Database Pamong (keadaan saat ini)

**Satu file, satu kebenaran.** Dokumen ini adalah gambaran **akhir/berlaku sekarang** dari
seluruh struktur database Pamong: topologi, setiap schema, setiap tabel, setiap kolom, plus
alasan di balik bentuknya. Riwayat *bagaimana* struktur ini sampai ke sini ada di
`docs/DB_CHANGELOG.md` — dokumen ini tidak memuat riwayat, hanya keadaan.

Aturan pemeliharaan (DOCUMENTATION_CONVENTION §7): **setiap PR yang mengubah struktur DB wajib
memperbarui file ini dan menambah entri di `DB_CHANGELOG.md`, di PR yang sama.**

> Status terakhir disinkronkan: PR-3.8.3/3.8.4 (`e22506c`). Perubahan struktur terakhir:
> PR-3.8.2 (`fa6e51c`).

---

## Daftar isi

1. Topologi: DB-per-tenant + identity sentral
2. Tiga jalur pembuatan skema (dan mengapa ada tiga)
3. Identity DB — schema `id`
4. Tenant DB — schema `gov` (tabel framework)
5. Tenant DB — schema modul (contoh: `surat_masuk`)
6. Konvensi kolom yang berlaku lintas tabel
7. Tabel yang disebut konvensi tapi BELUM ada
8. Peta relasi

---

## 1. Topologi: DB-per-tenant + identity sentral

```
Postgres (satu atau banyak instance — tergantung tier tenant)
│
├── db: gov_identity                        ← SENTRAL, satu-satunya yang shared
│   └── schema: id                          persons, employments, credentials,
│                                           tenant_registry, tenant_assignments,
│                                           central_roles*, revoked_tokens, otps,
│                                           data_keys, audit_logs
│
├── db: gov_pemkot_surabaya                 ← TENANT, isolasi penuh (ADR-004)
│   ├── schema: gov                         tabel framework (audit, workflow, config,
│   │                                       notification, scheduler, dst)
│   └── schema: surat_masuk                 tabel modul bisnis
│
└── db: gov_pemkot_malang                   ← tenant lain, DB terpisah total
    ├── schema: gov
    └── schema: surat_masuk
```

Konsekuensi bentuk ini yang harus selalu diingat saat membaca skema di bawah:

- **Tabel di tenant DB umumnya tidak butuh kolom `tenant_id`** — isolasi sudah struktural
  (DB-nya sendiri). `gov.sequences` adalah contoh murni ini. Tabel yang *tetap* memakai
  `tenant_id` melakukannya karena alasan spesifik: partisi hash chain audit, scope resolver
  config, atau agar tabel bisa dipakai juga pada deployment single-DB/dev.
- **Tidak ada FK lintas DB.** Relasi identity → tenant diwujudkan lewat *event + clone*
  (`gov.user_profiles`), bukan foreign key.
- **Tidak ada JOIN lintas schema modul.** Butuh data modul lain → lewat port (CLAUDE.md).
- Lokasi fisik DB tiap tenant dibaca runtime dari `id.tenant_registry`, sehingga kenaikan
  tier (shared → dedicated server) tidak menyentuh kode.

---

## 2. Tiga jalur pembuatan skema (dan mengapa ada tiga)

Skema tidak semuanya lahir dari file `.sql`. Saat mendokumentasikan perubahan, kenali dulu
jalur mana yang dipakai — ketiganya sama-sama "perubahan struktur DB" dan sama-sama wajib
masuk `DB_CHANGELOG.md`.

| # | Jalur | Sumber | Dijalankan oleh | Tracking |
|---|---|---|---|---|
| A | Migrasi ber-file, tenant DB | `core/{komponen}/migrations/*.sql`, `modules/{modul}/migrations/*.sql` (di-`go:embed`) | `pamongctl migrate` (via `infra/schema.CoreMigrations`) **dan** `db.ApplyEmbeddedSchema` saat store pertama dipakai | `gov.migration_history` |
| B | Migrasi ber-file, identity DB | `identity/migrations/*.sql` | **belum di-wire ke runner** — dijalankan manual/ops; e2e test menerapkannya dari direktori. Sengaja: perubahan identity butuh review ekstra (lihat ROADMAP) | belum |
| C | Ensure-schema-on-write (DDL di Go) | konstanta DDL di file Go, mis. `tenantrole/adapter/db/schema.go` | dieksekusi idempoten (`IF NOT EXISTS`) di awal operasi baca/tulis atau saat boot | tidak ada |

Jalur C dipakai untuk tabel framework `gov.*` yang lahir sebelum runner migrasi framework-gov
formal ada. Itu **utang teknis yang disadari** (DEFERRED, lihat ROADMAP): konsekuensinya tabel
tersebut tidak punya down-migration dan tidak tercatat di `gov.migration_history`. Karena
DDL-nya `IF NOT EXISTS`, **penambahan kolom** pada tabel jalur C harus ditulis eksplisit
sebagai `ALTER TABLE ... ADD COLUMN IF NOT EXISTS` (pola `gov.user_profiles`, `gov.outbox_events`)
— `CREATE TABLE IF NOT EXISTS` tidak akan pernah menambah kolom ke tabel yang sudah terlanjur ada.

Kolom di bawah ini ditandai jalurnya: **(A)**, **(B)**, atau **(C)**.

---

## 3. Identity DB — schema `id`

DB sentral `gov_identity`. Berisi identitas manusia, kepegawaian, kredensial, registry tenant,
role sentral, dan kunci enkripsi ter-wrap. Modul bisnis **dilarang** menyentuh DB ini —
aksesnya lewat port `UserResolver` dan event sinkronisasi.

### 3.1 `id.persons` — master identitas *(B, `001_create_identity`)*

Satu baris per manusia; anchor identitas adalah **NIK** (setiap warga punya, tidak hanya ASN).

| Kolom | Tipe | Catatan |
|---|---|---|
| `id` | UUID PK | app-generated (bukan `gen_random_uuid()`) — id dibuat di domain agar event bisa memuatnya sebelum commit |
| `nik` | VARCHAR(16) UNIQUE NOT NULL | anchor global |
| `nama_lengkap` | VARCHAR(255) NOT NULL | perubahannya (termasuk gelar) hanya lewat use case identity → event → clone |
| `tgl_lahir` | DATE | |
| `no_hp` | VARCHAR(15) | |
| `email` | VARCHAR(255) | |
| `is_active` | BOOLEAN NOT NULL DEFAULT true | |
| `created_at` / `updated_at` | TIMESTAMPTZ NOT NULL DEFAULT now() | |

`nik`, `no_hp`, `email` berkelas data `personal_id` (ADR-009) → kandidat enkripsi + blind index.
Kolomnya **masih plaintext**: mesin enkripsi transparan sudah ada di lapis repository
(PR-3.8.3/3.8.4), tapi tabel identity ditulis lewat migrasi tangan — bukan di-generate dari
`EntityDef` — sehingga migrasi ke bentuk `_enc`/`_bidx` belum dilakukan.

### 3.2 `id.employments` — relasi kepegawaian *(B, `001_create_identity`)*

Opsional dan bisa lebih dari satu sepanjang waktu: tidak semua person adalah pegawai.

| Kolom | Tipe | Catatan |
|---|---|---|
| `id` | UUID PK | |
| `person_id` | UUID NOT NULL → `id.persons(id)` | |
| `status` | VARCHAR(10) NOT NULL | CHECK `('asn','non_asn')` |
| `nip` | VARCHAR(18) UNIQUE | wajib bila `asn`, NULL bila `non_asn` (CHECK gabungan) |
| `instansi_asal` | VARCHAR(255) | instansi induk pegawai |
| `is_active` | BOOLEAN NOT NULL DEFAULT true | |
| `valid_from` / `valid_until` | TIMESTAMPTZ | `valid_until` NULL = berlaku terus |
| `created_at` | TIMESTAMPTZ NOT NULL DEFAULT now() | |

Index: `idx_employments_person (person_id)`.

CHECK gabungan `(status='asn' AND nip IS NOT NULL) OR (status='non_asn' AND nip IS NULL)` menegakkan
invariant "NIP hanya milik ASN" di level DB, bukan hanya di kode.

### 3.3 `id.credentials` — jalur login *(B, `001_create_identity`)*

Banyak credential per person; semuanya resolve ke person yang sama (ASN yang login lewat portal
publik tetap orang yang sama, hanya personanya berbeda).

| Kolom | Tipe | Catatan |
|---|---|---|
| `id` | UUID PK | |
| `person_id` | UUID NOT NULL → `id.persons(id)` | |
| `cred_type` | VARCHAR(20) NOT NULL | CHECK `('nip','nik','email','no_hp','oauth')` |
| `cred_value` | VARCHAR(255) NOT NULL | UNIQUE bersama `cred_type` |
| `secret_hash` | VARCHAR(255) | bcrypt; NULL bila SSO/OTP-only |
| `is_primary` | BOOLEAN NOT NULL DEFAULT false | |
| `last_used_at` | TIMESTAMPTZ | |
| `created_at` | TIMESTAMPTZ NOT NULL DEFAULT now() | |

Constraint: `UNIQUE (cred_type, cred_value)`. Index: `idx_credentials_person (person_id)`.

### 3.4 `id.tenant_registry` — registry tenant *(B, `002` + `008`)*

Harus sentral: resolver butuh tabel ini untuk tahu di DB mana tenant hidup (chicken-and-egg bila
disimpan di tenant DB).

| Kolom | Tipe | Catatan |
|---|---|---|
| `tenant_id` | VARCHAR(100) PK | mis. `pemkot-surabaya` |
| `nama` | VARCHAR(255) NOT NULL | |
| `tier` | SMALLINT NOT NULL DEFAULT 1 | CHECK `(1,2,3)` — shared / dedicated DB / dedicated server |
| `db_host` | VARCHAR(255) NOT NULL | lokasi fisik; kenaikan tier = UPDATE kolom ini |
| `db_name` | VARCHAR(100) NOT NULL | |
| `db_schema` | VARCHAR(100) NOT NULL DEFAULT '' | umumnya kosong (DB-per-tenant, bukan schema-per-tenant) |
| `migration_version` | VARCHAR(50) NOT NULL DEFAULT '' | monitoring rollout lintas tenant |
| `is_active` | BOOLEAN NOT NULL DEFAULT true | |
| `key_custody` | VARCHAR(10) NOT NULL DEFAULT 'platform' | CHECK `('platform','tenant')` — ADR-010 |
| `created_at` / `updated_at` | TIMESTAMPTZ NOT NULL DEFAULT now() | |

Index: `idx_tenant_registry_active (is_active)`.

`key_custody='tenant'` **ditolak lantang** oleh resolver saat ini (driver KeyProvider pemda baru
hadir di PR-3.8.8) — sengaja, agar tidak diam-diam jatuh ke platform dan memberi jaminan
kedaulatan kunci yang tidak benar.

### 3.5 `id.tenant_assignments` — penugasan employment ke tenant *(B, `003`)*

Menempel pada **employment**, bukan person: persona `citizen` tidak perlu baris di sini.

| Kolom | Tipe | Catatan |
|---|---|---|
| `id` | UUID PK | |
| `employment_id` | UUID NOT NULL → `id.employments(id)` | |
| `tenant_id` | VARCHAR(100) NOT NULL | tanpa FK ke registry (lihat 3.7 soal token region) |
| `is_home_tenant` | BOOLEAN NOT NULL DEFAULT true | `false` = penugasan cross-tenant (PJ Bupati, PLT) |
| `assigned_by` | UUID NOT NULL → `id.persons(id)` | wajib ber-permission `identity:assignment:cross_tenant` bila `is_home_tenant=false` (ditegakkan di use case) |
| `valid_from` / `valid_until` | TIMESTAMPTZ | |
| `created_at` | TIMESTAMPTZ NOT NULL DEFAULT now() | |

Constraint: `UNIQUE (employment_id, tenant_id, valid_from)`.
Index: `idx_tenant_assignments_employment`, `idx_tenant_assignments_tenant`.

### 3.6 `id.central_roles` — definisi role sentral *(B, `004`)*

| Kolom | Tipe | Catatan |
|---|---|---|
| `id` | UUID PK | |
| `name` | VARCHAR(100) UNIQUE NOT NULL | mis. `super_admin`, `regional_helpdesk` |
| `label` | VARCHAR(255) NOT NULL | |
| `scope_type` | VARCHAR(10) NOT NULL | CHECK `('global','scoped')` |
| `description` | TEXT | |
| `created_at` | TIMESTAMPTZ NOT NULL DEFAULT now() | |

### 3.7 `id.central_role_permissions` — grant role → permission *(B, `004`)*

| Kolom | Tipe | Catatan |
|---|---|---|
| `role_id` | UUID NOT NULL → `id.central_roles(id)` ON DELETE CASCADE | |
| `permission` | VARCHAR(150) NOT NULL | string `{modul}:{entity}:{aksi}` |

PK `(role_id, permission)`. Bentuk RBAC kanonik yang ditiru tenant role & manifest.
**Definisi** permission tidak disimpan di sini — sumbernya manifest modul (kode); tabel ini hanya
menyimpan *grant*.

### 3.8 `id.central_role_assignments` — assignment role sentral ke person *(B, `004`)*

| Kolom | Tipe | Catatan |
|---|---|---|
| `id` | UUID PK | |
| `person_id` | UUID NOT NULL → `id.persons(id)` | |
| `role_id` | UUID NOT NULL → `id.central_roles(id)` | |
| `tenant_scope` | VARCHAR(100)[] | daftar tenant tempat assignment *scoped* berlaku |
| `assigned_by` | UUID NOT NULL → `id.persons(id)` | |
| `valid_from` / `valid_until` | TIMESTAMPTZ | `valid_until` NULL = permanen |
| `created_at` | TIMESTAMPTZ NOT NULL DEFAULT now() | |

Index: `idx_central_role_assignments_person`, `idx_central_role_assignments_role`.

Dua keputusan yang mudah salah dibaca:
- **Otoritas global vs scoped ditentukan `central_roles.scope_type`, bukan kekosongan
  `tenant_scope`.** Role scoped dengan `tenant_scope` kosong berlaku *di mana pun tidak* —
  resolver fail-closed, mencegah eskalasi diam-diam.
- **Sengaja tanpa FK ke `id.tenant_registry`**, agar kelak token region (mis. `prov:jatim`)
  untuk wildcard provinsi bisa masuk tanpa mengubah skema.

### 3.9 `id.revoked_tokens` — denylist token *(B, `005`)*

| Kolom | Tipe | Catatan |
|---|---|---|
| `jti` | UUID PK | id token; verifikasi menolak bila ada di sini |
| `person_id` | UUID NOT NULL → `id.persons(id)` | |
| `expires_at` | TIMESTAMPTZ NOT NULL | = `exp` token; setelah lewat, baris boleh dipurge |
| `revoked_at` | TIMESTAMPTZ NOT NULL DEFAULT now() | |
| `reason` | TEXT | |

Index: `idx_revoked_tokens_expires (expires_at)`.

Denylist **per-jti**. "Cabut semua token satu person" (epoch `tokens_valid_after`) belum ada —
DEFERRED sampai event pencabutan role di-wire.

### 3.10 `id.otps` — OTP login citizen *(B, `006`)*

| Kolom | Tipe | Catatan |
|---|---|---|
| `id` | UUID PK | |
| `credential_id` | UUID NOT NULL → `id.credentials(id)` ON DELETE CASCADE | |
| `code_hash` | VARCHAR(255) NOT NULL | bcrypt — kode **tidak pernah** disimpan plaintext |
| `expires_at` | TIMESTAMPTZ NOT NULL | TTL pendek (default 5 menit) |
| `consumed_at` | TIMESTAMPTZ | non-NULL = sudah dipakai/dihanguskan (sekali-pakai) |
| `attempts` | INT NOT NULL DEFAULT 0 | cap tebakan di `domain.MaxOTPAttempts` |
| `created_at` | TIMESTAMPTZ NOT NULL DEFAULT now() | |

Index: `idx_otps_credential_created (credential_id, created_at DESC)` — verifikasi selalu mengambil
OTP terbaru per credential.

### 3.11 `id.data_keys` — DEK ter-wrap *(B, `007`)*

Sentral **secara sengaja** (ADR-010 §2): dump satu tenant DB berisi ciphertext saja, tanpa kunci
apa pun untuk membukanya.

| Kolom | Tipe | Catatan |
|---|---|---|
| `tenant_id` | VARCHAR(100) NOT NULL | isolasi per-tenant: satu tenant bocor tak membuka tenant lain |
| `purpose` | VARCHAR(50) NOT NULL | konteks kunci (`nik`, `no_rekening`, …) — membatasi blast radius |
| `kind` | VARCHAR(10) NOT NULL | CHECK `('enc','bidx')` |
| `key_version` | INTEGER NOT NULL | CHECK `> 0` |
| `wrapped_dek` | BYTEA NOT NULL | DEK dibungkus KEK; format self-describing milik driver |
| `kek_driver` | VARCHAR(30) NOT NULL | driver yang membungkus versi ini (diagnosa saat KMS diganti) |
| `custody` | VARCHAR(10) NOT NULL DEFAULT 'platform' | CHECK `('platform','tenant')` |
| `is_active` | BOOLEAN NOT NULL DEFAULT true | versi aktif = dipakai untuk **tulis** |
| `created_at` / `rotated_at` | TIMESTAMPTZ | |

PK `(tenant_id, purpose, kind, key_version)`.
Unique partial: `uq_data_keys_active (tenant_id, purpose, kind) WHERE is_active` — tepat satu versi
aktif, agar "versi aktif" tak pernah ambigu saat dua proses menulis paralel.

`kind` sengaja memisahkan kunci enkripsi dari kunci blind index (bukan diturunkan dari satu DEK):
rotasi kunci `enc` murah & lazy, sedangkan rotasi `bidx` memaksa reindex seluruh baris. Menurunkan
keduanya dari satu DEK akan menyeret reindex mahal setiap rotasi (ADR-009 §2).
KEK sendiri **tidak pernah** masuk DB — ia hidup di KeyProvider (KMS/master key ops).

### 3.12 `id.audit_logs` — audit mutasi identity *(C, `infra/db/audit.go`)*

Struktur **identik** dengan `gov.audit_logs` (lihat §4.2); yang berbeda hanya schema-nya dan
partisi hash chain-nya (identity memakai partisi konstan → satu chain tunggal, ADR-003).

---

## 4. Tenant DB — schema `gov` (tabel framework)

Ada di **setiap** tenant DB. Semua tabel di sini milik framework; modul bisnis membacanya lewat
port, bukan query langsung.

### 4.1 `gov.migration_history` — tracking migrasi *(C, `infra/db/migration.go`)*

Sumber kebenaran migrasi apa yang sudah jalan di tenant ini.

| Kolom | Tipe | Catatan |
|---|---|---|
| `id` | BIGSERIAL PK | |
| `module` | TEXT NOT NULL | nama komponen/modul migrasi (unik lintas komponen) |
| `version` | TEXT NOT NULL | nomor urut file |
| `name` | TEXT NOT NULL | |
| `checksum` | TEXT NOT NULL | deteksi file migrasi yang diubah setelah diterapkan |
| `applied_at` | TIMESTAMPTZ NOT NULL DEFAULT now() | |

Constraint: `UNIQUE (module, version)`.

### 4.2 `gov.audit_logs` — jejak audit ber-hash-chain *(C, `infra/db/audit.go`)*

Append-only. Hanya INSERT; tidak ada UPDATE/DELETE dari kode.

| Kolom | Tipe | Catatan |
|---|---|---|
| `seq` | BIGSERIAL | urutan tulis; dipakai verifikasi chain |
| `id` | UUID PK | |
| `tenant_id` | TEXT NOT NULL | **partisi hash chain** — inilah alasan kolom ini tetap ada di tenant DB |
| `entity` | TEXT NOT NULL | |
| `entity_id` | UUID NOT NULL | |
| `action` | TEXT NOT NULL | |
| `actor_id` | UUID NOT NULL | |
| `actor_ip` | TEXT NOT NULL DEFAULT '' | |
| `diff` | JSONB NOT NULL | field-level diff; field sensitif di-mask (ADR-002) |
| `workflow_from` / `workflow_to` | TEXT NOT NULL DEFAULT '' | transisi state bila mutasi berasal dari workflow |
| `created_at` | TIMESTAMPTZ NOT NULL | |
| `prev_hash` | TEXT NOT NULL | hash entry sebelumnya dalam partisi yang sama |
| `hash` | TEXT NOT NULL | hash entry ini — putusnya rantai = indikasi tamper |

Index: `idx_audit_entity (entity, entity_id)`, `idx_audit_actor (actor_id)`,
`idx_audit_tenant_seq (tenant_id, seq)`.

Penulisan diserialisasi per partisi lewat `pg_advisory_xact_lock` agar chain tidak putus oleh
penulisan paralel.

### 4.3 `gov.outbox_events` — outbox pattern *(C, `infra/eventbus/outbox.go`)*

Event ditulis dalam **transaksi bisnis yang sama**; relay mengirimnya setelah commit. Ini yang
membuat "operasi gagal tidak mempublish event" benar secara struktural, bukan by convention.

| Kolom | Tipe | Catatan |
|---|---|---|
| `id` | UUID PK DEFAULT gen_random_uuid() | |
| `event_name` | TEXT NOT NULL | |
| `payload` | JSONB NOT NULL | divalidasi terhadap schema registry sebelum tulis |
| `tenant_id` | TEXT NOT NULL DEFAULT '' | |
| `caused_by` | TEXT NOT NULL DEFAULT '' | korelasi sebab-akibat antar event |
| `idempotency_key` | TEXT NOT NULL DEFAULT '' | |
| `created_at` | TIMESTAMPTZ NOT NULL DEFAULT now() | |
| `dispatched_at` | TIMESTAMPTZ | NULL = belum terkirim |
| `attempts` | INT NOT NULL DEFAULT 0 | |
| `next_retry_at` | TIMESTAMPTZ | backoff |
| `failed_at` | TIMESTAMPTZ | non-NULL = masuk DLQ, berhenti di-retry |

Index parsial: `idx_outbox_pending (next_retry_at NULLS FIRST, created_at) WHERE dispatched_at IS NULL
AND failed_at IS NULL` — relay hanya men-scan baris yang benar-benar siap kirim.

### 4.4 `gov.user_profiles` — clone read-only identity *(C, `identity/sync/writer_tenantdb.go`)*

Hasil sinkronisasi event dari identity DB. Hanya person ber-persona *employee* (punya tenant
assignment) yang di-clone ke sini.

| Kolom | Tipe | Catatan |
|---|---|---|
| `id` | UUID PK | **sama persis** dengan `id.persons.id` |
| `person_id` | UUID NOT NULL | = `id` (eksplisit untuk kejelasan) |
| `employment_status` | VARCHAR(10) NOT NULL | `asn` \| `non_asn` |
| `nip` | VARCHAR(18) | |
| `nik` | VARCHAR(16) NOT NULL | |
| `nama_lengkap` | VARCHAR(255) NOT NULL | |
| `assignment_id` | UUID NOT NULL | rujukan ke `id.tenant_assignments` (tanpa FK — beda DB) |
| `is_cross_tenant` | BOOLEAN NOT NULL DEFAULT false | |
| `email` | VARCHAR(255) | kontak untuk routing notifikasi (ADR-013) |
| `no_hp` | VARCHAR(15) | idem |
| `synced_at` | TIMESTAMPTZ NOT NULL | |
| `jabatan_lokal` | VARCHAR(255) | kolom spesifik tenant, boleh diisi modul kepegawaian |
| `unit_kerja_id` | UUID | scope ABAC; FK ke `gov.org_units` ditunda (lihat §2 jalur C) |

**Larangan keras:** jangan pernah menambahkan kolom credential/password di sini. Secret tetap di
`id.credentials`.

Tabel ini adalah clone **hidup** (selalu nilai terbaru) untuk menjawab "siapa user ini sekarang".
Ia **bukan** sumber untuk dokumen historis — nama/jabatan pada dokumen bisnis wajib di-snapshot
saat dokumen dibuat.

### 4.5 `gov.tenant_roles` — role tenant *(C, `tenantrole/adapter/db/schema.go`)*

| Kolom | Tipe | Catatan |
|---|---|---|
| `id` | UUID PK | |
| `name` | VARCHAR(100) UNIQUE NOT NULL | `snake_case`, mis. `bendahara_pengeluaran` |
| `label` | VARCHAR(255) NOT NULL | |
| `description` | TEXT | |
| `created_at` | TIMESTAMPTZ NOT NULL DEFAULT now() | |

### 4.6 `gov.tenant_role_permissions` — grant role tenant → permission *(C)*

| Kolom | Tipe | Catatan |
|---|---|---|
| `role_id` | UUID NOT NULL → `gov.tenant_roles(id)` ON DELETE CASCADE | |
| `permission` | VARCHAR(150) NOT NULL | |

PK `(role_id, permission)`. Cermin `id.central_role_permissions`.

### 4.7 `gov.user_role_assignments` — assignment role tenant ke user *(C)*

| Kolom | Tipe | Catatan |
|---|---|---|
| `id` | UUID PK | |
| `user_id` | UUID NOT NULL | = `gov.user_profiles.id`; **tanpa FK** (lihat catatan di bawah) |
| `role_id` | UUID NOT NULL → `gov.tenant_roles(id)` | |
| `unit_kerja_id` | UUID | scope ABAC; NULL = seluruh tenant |
| `include_subtree` | BOOLEAN NOT NULL DEFAULT false | berlaku juga untuk unit turunan |
| `assigned_by` | UUID NOT NULL | |
| `valid_from` / `valid_until` | TIMESTAMPTZ | |
| `created_at` | TIMESTAMPTZ NOT NULL DEFAULT now() | |

Index: `idx_user_role_assignments_user`, `idx_user_role_assignments_role`.

FK `user_id → gov.user_profiles` **sengaja belum dipasang**: kedua tabel sama-sama
ensure-on-write tanpa jaminan urutan pembuatan. Resolver tidak butuh JOIN ke `user_profiles`
(cukup `user_role_assignments` + `tenant_roles`), jadi integritas baca tetap terjaga. FK dipasang
saat runner migrasi framework-gov formal hadir.

### 4.8 `gov.org_units` — hierarki OPD *(C, `tenantrole/adapter/db/hierarchy.go`)*

| Kolom | Tipe | Catatan |
|---|---|---|
| `id` | UUID PK | |
| `parent_id` | UUID → `gov.org_units(id)` | adjacency list |
| `name` | VARCHAR(255) NOT NULL | |

**Placeholder** untuk modul OPD penuh (jabatan/eselon, CRUD, UI) yang menyusul; saat itu modul
tersebut menjadi pemilik tabel ini lewat port `Hierarchy` yang sama (non-breaking).

Adjacency dipilih ketimbang closure table/`ltree` karena tree OPD dangkal & jarang bermutasi, dan
`ltree` butuh ekstensi Postgres yang tak terjamin tersedia di Tier 3. Penelusuran memakai recursive
CTE ke **atas** (dari unit ke ancestor), bukan mengembang subtree.

### 4.9 `gov.delegations` — delegasi/PLT intra-tenant *(C, `delegation/adapter/db/schema.go`)*

| Kolom | Tipe | Catatan |
|---|---|---|
| `id` | UUID PK | |
| `from_user_id` | UUID NOT NULL | pemberi delegasi |
| `to_user_id` | UUID NOT NULL | penerima |
| `permissions` | TEXT[] NOT NULL | **subset** permission pemberi |
| `unit_kerja_id` | UUID | scope |
| `include_subtree` | BOOLEAN NOT NULL DEFAULT false | |
| `reason` | TEXT | |
| `valid_from` | TIMESTAMPTZ NOT NULL DEFAULT now() | |
| `valid_until` | TIMESTAMPTZ **NOT NULL** | menegakkan invariant "delegasi selalu berbatas waktu" di level DB |
| `assigned_by` | UUID NOT NULL | |
| `created_at` | TIMESTAMPTZ NOT NULL DEFAULT now() | |

Index: `idx_delegations_to_user (to_user_id)`.

### 4.10 `gov.workflow_definitions` — definisi workflow ber-versi *(A, `core/workflow/001`)*

| Kolom | Tipe | Catatan |
|---|---|---|
| `workflow_id` | TEXT NOT NULL | bagian PK |
| `version` | INT NOT NULL | bagian PK — perubahan = baris baru, bukan UPDATE |
| `entity` | TEXT NOT NULL DEFAULT '' | |
| `initial_state` | TEXT NOT NULL | |
| `authoring_source` | TEXT NOT NULL DEFAULT 'developer' | `developer` (template) vs penulis tenant kelak |
| `states` | JSONB NOT NULL | struktur, bukan kode |
| `transitions` | JSONB NOT NULL | termasuk guard expression (DSL boolean tanpa side-effect) |
| `effective_from` | TIMESTAMPTZ NOT NULL | |
| `created_by` | UUID | NULL untuk seed developer |
| `prev_version` | INT | NULL bila versi pertama |
| `created_at` | TIMESTAMPTZ NOT NULL DEFAULT now() | |

PK `(workflow_id, version)`. Index: `idx_wfdef_lookup (workflow_id, version DESC)`.

Baris lama tidak pernah dihapus: instance yang sedang berjalan tetap mengacu ke versi saat ia mulai.

### 4.11 `gov.tenant_workflow_configs` — pilihan template + role binding *(A, `core/workflow/002`+`003`)*

| Kolom | Tipe | Catatan |
|---|---|---|
| `tenant_id` | TEXT NOT NULL | |
| `slot` | TEXT NOT NULL | tipe workflow, mis. `surat_masuk.disposisi` |
| `template_id` | TEXT NOT NULL | `WorkflowDefinition.ID` yang dipilih |
| `role_bindings` | JSONB NOT NULL DEFAULT '{}' | peran generik → role konkret tenant |
| `version` | INT NOT NULL DEFAULT 1 | append-only: `max+1` per (tenant, slot) |
| `effective_from` | TIMESTAMPTZ NOT NULL DEFAULT now() | |
| `set_at` | TIMESTAMPTZ NOT NULL DEFAULT now() | |
| `set_by` | UUID | NULL = ditetapkan seed/framework |

Constraint: `uq_twc_version UNIQUE (tenant_id, slot, version)`. Index: `idx_twc_lookup (tenant_id, slot)`.

Disimpan **utuh satu baris** (`template_id` + `role_bindings`), bukan dilebur ke KV
`gov.tenant_configs`: `role_bindings` adalah map, sedangkan resolver KV dirancang untuk nilai
skalar ber-scope. Tanpa FK ke `gov.workflow_definitions` karena template bisa selesai dibuat
setelah config ditetapkan.

### 4.12 `gov.tenant_configs` — config ber-scope & ber-versi *(A, `core/config/001`+`002`)*

| Kolom | Tipe | Catatan |
|---|---|---|
| `tenant_id` | TEXT NOT NULL | |
| `unit_kerja_id` | UUID | NULL = level tenant |
| `resource_id` | UUID | NULL = level unit/tenant |
| `config_key` | TEXT NOT NULL | mis. `keuangan.persediaan` (= decision point strategy) |
| `value` | TEXT NOT NULL | string; pemakai yang menafsirkan (mis. strategy key) |
| `version` | INT NOT NULL DEFAULT 1 | append-only |
| `effective_from` | TIMESTAMPTZ NOT NULL DEFAULT now() | non-retroaktif |
| `set_at` | TIMESTAMPTZ NOT NULL DEFAULT now() | |
| `set_by` | UUID | NULL = seed/framework |

Constraints: `ck_tenant_config_scope CHECK (resource_id IS NULL OR unit_kerja_id IS NOT NULL)` —
resource ber-nested di bawah unit; `uq_tenant_config_version UNIQUE NULLS NOT DISTINCT
(tenant_id, config_key, unit_kerja_id, resource_id, version)`.
Index: `idx_tenant_config_lookup (tenant_id, config_key)`.

`NULLS NOT DISTINCT` (Postgres 15+) penting: tanpa itu dua baris scope-tenant untuk key yang sama
tidak dianggap konflik karena NULL ≠ NULL.

Skema ini sengaja kaya sejak awal (titik ekstensi #2): scope bisa diperdalam ke unit kerja/resource
tanpa migrasi, meski hari ini hampir semua baris hanya mengisi `tenant_id`.

### 4.13 `gov.tenant_custom_fields` — custom field per-tenant *(A, `core/customization/001`)*

| Kolom | Tipe | Catatan |
|---|---|---|
| `tenant_id` | TEXT NOT NULL | |
| `module` | TEXT NOT NULL | |
| `entity` | TEXT NOT NULL | nama `EntityDef` inti yang di-extend |
| `field_name` | TEXT NOT NULL | |
| `field_def` | JSONB NOT NULL | `domain.FieldDef` ter-serialisasi (bagian yang varian per-tipe) |
| `data_class` | TEXT NOT NULL DEFAULT 'internal' | klasifikasi data; default aman (ADR-009) |
| `insert_after` | TEXT NOT NULL DEFAULT '' | urutan tampil; '' = akhir |
| `is_active` | BOOLEAN NOT NULL DEFAULT true | soft-deactivate; data lama tetap |
| `created_by` | UUID NOT NULL | |
| `created_at` | TIMESTAMPTZ NOT NULL DEFAULT now() | |

PK `(tenant_id, module, entity, field_name)`.
Index parsial: `idx_tenant_custom_field_lookup (tenant_id, module, entity) WHERE is_active`.

Kolom pencarian dibuat **eksplisit & ter-index**; hanya bentuk field yang memang varian per-tipe
(Options, LinkTo, Precision, …) yang masuk JSONB — bukan seluruh baris jadi blob polimorfik.

### 4.14 `gov.tenant_capability_overrides` — capability flag *(A, `core/customization/002`)*

| Kolom | Tipe | Catatan |
|---|---|---|
| `tenant_id` | TEXT NOT NULL | |
| `capability` | TEXT NOT NULL | `{modul}.{fitur}` |
| `enabled` | BOOLEAN NOT NULL | |
| `set_by` | UUID | NULL = seed/framework |
| `set_at` | TIMESTAMPTZ NOT NULL DEFAULT now() | |

PK `(tenant_id, capability)`.

Hanya override **eksplisit** yang tersimpan: ketiadaan baris berarti "pakai `DefaultEnabled`",
**bukan** "nonaktif". Definisi capability tetap di kode.

### 4.15 `gov.idempotency_keys` — idempotency *(A, `core/idempotency/001`)*

| Kolom | Tipe | Catatan |
|---|---|---|
| `person_id` | UUID NOT NULL | bagian PK — di-scope ke principal |
| `key` | TEXT NOT NULL | bagian PK |
| `fingerprint` | TEXT NOT NULL | hash(method+path+body); key dipakai-ulang untuk request beda → 422 |
| `status` | INT | NULL selama reservasi pending |
| `response` | BYTEA | idem |
| `completed` | BOOLEAN NOT NULL DEFAULT false | |
| `created_at` | TIMESTAMPTZ NOT NULL DEFAULT now() | |
| `expires_at` | TIMESTAMPTZ NOT NULL | pending: pendek; completed: diperpanjang ke replay window (mis. 24 jam) |

PK `(person_id, key)`. Index: `idx_idempotency_expires (expires_at)`.

PK gabungan dengan `person_id` adalah kontrol keamanan: satu user tidak bisa membaca atau menimpa
respons user lain dengan menebak nilai key.

### 4.16 `gov.sequences` — penomoran atomik *(A, `core/sequence/001`)*

| Kolom | Tipe | Catatan |
|---|---|---|
| `name` | TEXT NOT NULL | = pola nomor yang diminta caller, mis. `{tahun}/AG/{nomor:5}` |
| `tahun` | INT NOT NULL | bagian PK → reset tahunan bersifat intrinsik, tanpa job reset |
| `current` | BIGINT NOT NULL | increment atomik lewat `UPDATE ... RETURNING` |
| `updated_at` | TIMESTAMPTZ NOT NULL DEFAULT now() | |

PK `(name, tahun)`. **Tanpa `tenant_id`** — isolasi sudah dari DB-per-tenant.

### 4.17 `gov.scheduled_jobs` — jadwal job *(A, `core/scheduler/001`)*

| Kolom | Tipe | Catatan |
|---|---|---|
| `id` | UUID PK DEFAULT gen_random_uuid() | |
| `tenant_id` | TEXT NOT NULL DEFAULT '' | kosong = job level-platform |
| `name` | TEXT NOT NULL | nama deskriptif |
| `job_key` | TEXT NOT NULL | **rujukan** handler di `scheduler.Registry` — bukan kode |
| `cron_expr` | TEXT NOT NULL DEFAULT '' | kosong = one-shot |
| `payload` | BYTEA | argumen opaque |
| `enabled` | BOOLEAN NOT NULL DEFAULT true | |
| `next_run_at` | TIMESTAMPTZ NOT NULL | |
| `last_run_at` | TIMESTAMPTZ | |
| `created_by` | UUID | |
| `created_at` | TIMESTAMPTZ NOT NULL DEFAULT now() | |

Index parsial: `idx_scheduled_jobs_due (next_run_at) WHERE enabled`.

Bentuk one-shot (`cron_expr` kosong) inilah yang dipakai deadline SLA workflow — tak perlu
mekanisme penjadwalan terpisah.

### 4.18 `gov.job_runs` — riwayat eksekusi *(A, `core/scheduler/001`)*

| Kolom | Tipe | Catatan |
|---|---|---|
| `id` | UUID PK DEFAULT gen_random_uuid() | |
| `schedule_id` | UUID → `gov.scheduled_jobs(id)` ON DELETE SET NULL | NULL = ad-hoc |
| `tenant_id` | TEXT NOT NULL DEFAULT '' | |
| `job_key` | TEXT NOT NULL | |
| `payload` | BYTEA | snapshot untuk replay konteks-sama |
| `status` | TEXT NOT NULL | `success` \| `failed` |
| `started_at` / `finished_at` | TIMESTAMPTZ NOT NULL | |
| `error` | TEXT NOT NULL DEFAULT '' | |
| `attempt` | INT NOT NULL DEFAULT 1 | |

Index: `idx_job_runs_schedule (schedule_id, started_at DESC)`.

### 4.19 `gov.job_locks` — lock terdistribusi ber-sewa *(A, `core/scheduler/002`)*

| Kolom | Tipe | Catatan |
|---|---|---|
| `lock_key` | TEXT PK | |
| `token` | TEXT NOT NULL | pemegang unik; guard agar hanya pemegang saat ini boleh release |
| `locked_until` | TIMESTAMPTZ NOT NULL | batas sewa; `< now` = bebas, boleh diambil alih |

Lease (bukan lock permanen) mencegah deadlock abadi bila instance pemegang mati.

### 4.20 `gov.notification_templates` *(A, `core/notification/001`)*

| Kolom | Tipe | Catatan |
|---|---|---|
| `tenant_id` | TEXT NOT NULL DEFAULT '' | **'' = template global** (default framework/modul) |
| `key` | TEXT NOT NULL | `{modul}.{kejadian}` |
| `locale` | TEXT NOT NULL DEFAULT 'id' | |
| `subject` | TEXT NOT NULL DEFAULT '' | |
| `body` | TEXT NOT NULL | konten, bukan logika |
| `updated_at` | TIMESTAMPTZ NOT NULL DEFAULT now() | |

Constraint: `uq_notif_template UNIQUE (tenant_id, key, locale)`.
Index: `idx_notif_template_lookup (tenant_id, key)`.

Pemilihan "paling cocok" (tenant > global, locale sama > default) dilakukan di TemplateEngine —
tabel hanya menyimpan kandidat.

### 4.21 `gov.notification_inapp` — kotak masuk *(A, `core/notification/001`)*

| Kolom | Tipe | Catatan |
|---|---|---|
| `id` | UUID PK DEFAULT gen_random_uuid() | |
| `tenant_id` | TEXT NOT NULL DEFAULT '' | |
| `person_id` | UUID NOT NULL | |
| `template_key` | TEXT NOT NULL DEFAULT '' | |
| `subject` | TEXT NOT NULL DEFAULT '' | |
| `body` | TEXT NOT NULL | |
| `is_read` | BOOLEAN NOT NULL DEFAULT false | |
| `created_at` | TIMESTAMPTZ NOT NULL DEFAULT now() | |

Index: `idx_notif_inapp_recipient (tenant_id, person_id, created_at DESC)`.

### 4.22 `gov.notification_deliveries` — jejak pengiriman *(A, `core/notification/001`)*

| Kolom | Tipe | Catatan |
|---|---|---|
| `id` | UUID PK DEFAULT gen_random_uuid() | |
| `tenant_id` | TEXT NOT NULL DEFAULT '' | |
| `person_id` | UUID NOT NULL | |
| `channel` | TEXT NOT NULL | |
| `template_key` | TEXT NOT NULL DEFAULT '' | |
| `status` | TEXT NOT NULL | `delivered` \| `failed` \| `read` |
| `error` | TEXT NOT NULL DEFAULT '' | |
| `delivered_at` | TIMESTAMPTZ NOT NULL DEFAULT now() | |

Index: `idx_notif_delivery_recipient (tenant_id, person_id, delivered_at DESC)`.

Satu baris per upaya kirim per channel — menjawab pertanyaan audit "kenapa notif tidak sampai".

---

## 5. Tenant DB — schema modul

Satu schema Postgres per modul, namanya = nama modul. Nama tabel: `{schema}.{entity_plural}`.

### 5.1 `surat_masuk.surat_masuks` *(A, `modules/surat_masuk/001`)*

| Kolom | Tipe | Catatan |
|---|---|---|
| `id` | UUID PK | app-generated |
| `nomor_agenda` | VARCHAR(64) NOT NULL | UNIQUE (`uq_surat_nomor_agenda`) |
| `nomor_surat` | VARCHAR(128) NOT NULL | |
| `tanggal_surat` | DATE NOT NULL | |
| `tanggal_agenda` | DATE NOT NULL | |
| `pengirim` | VARCHAR(255) NOT NULL | |
| `perihal` | TEXT NOT NULL | |
| `sifat` | VARCHAR(16) NOT NULL | |
| `status` | VARCHAR(32) NOT NULL | |
| `version` | INT NOT NULL DEFAULT 1 | optimistic locking (framework) |
| `created_at` / `updated_at` | TIMESTAMPTZ NOT NULL DEFAULT now() | |
| `deleted_at` | TIMESTAMPTZ | soft delete |

Index: `idx_surat_perihal (perihal)`, `idx_surat_tanggal_agenda (tanggal_agenda)`.

### 5.2 `surat_masuk.disposisis` *(A, `modules/surat_masuk/001`)*

| Kolom | Tipe | Catatan |
|---|---|---|
| `id` | UUID PK | |
| `surat_id` | UUID NOT NULL → `surat_masuk.surat_masuks(id)` | FK **intra-modul** — diizinkan |
| `dari_jabatan` / `kepada_jabatan` | VARCHAR(128) NOT NULL | |
| `instruksi` | TEXT NOT NULL | |
| `tanggal` | TIMESTAMPTZ NOT NULL | |
| `version` | INT NOT NULL DEFAULT 1 | |
| `created_at` / `updated_at` | TIMESTAMPTZ NOT NULL DEFAULT now() | |
| `deleted_at` | TIMESTAMPTZ | |

Index: `idx_disposisi_surat (surat_id)`.

---

## 6. Konvensi kolom yang berlaku lintas tabel

**Kolom sistem entity modul** (di-generate `infra/db.GenerateMigration` dari `EntityDef`, jadi
seragam di semua modul):

```
id          UUID PRIMARY KEY              -- app-generated
{field...}                                 -- dari FieldDef
version     INT NOT NULL DEFAULT 1        -- optimistic locking, dicek framework
created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
deleted_at  TIMESTAMPTZ                   -- soft delete
```

`created_by` / `updated_by` sudah **di-reserve** di `domain.reservedFieldNames` tapi belum
di-generate — namanya dilindungi lebih dulu agar penambahan nanti tidak bentrok dengan field modul.

**Field terenkripsi** (`DataClass` = `personal_id` atau `specific`, ADR-009) menjadi **dua kolom
fisik**, dan nilai plaintext tidak pernah disimpan:

```
{field}_enc   BYTEA   -- ciphertext AES-256-GCM
{field}_bidx  BYTEA   -- blind index HMAC; ada bila field Searchable atau Unique
```

`UNIQUE` menempel pada `_bidx`, bukan `_enc` (nonce GCM acak → ciphertext tak pernah sama untuk
nilai sama). Field yang butuh `LIKE`/range search **tidak boleh** diklasifikasi terenkripsi —
kolom terenkripsi kehilangan kemampuan itu. Repo Tier 3 dilarang menyentuh kolom `_enc` mentah
tanpa helper framework (linter `encrypted-field-no-raw-query`).

Pemetaan kolom logis → fisik ditegakkan **transparan di lapis repository** (`infra/db/field_crypto.go`,
PR-3.8.3/3.8.4), bukan dipanggil use case: filter equality dialihkan ke `_bidx`, sort atas kolom
terenkripsi ditolak, dan diff audit memakai bentuk yang sudah di-mask.

Isi `_enc` **terikat pada barisnya**: id baris ikut ke AAD ciphertext (ADR-016), sehingga blob
yang dipindah ke baris lain lewat `UPDATE` langsung GAGAL dibuka. `record_id` itu ada di AAD
saja — tidak disimpan di dalam blob, karena blob yang membawa serta identitasnya sendiri akan
tetap terbuka di mana pun ia dipasang. Format ciphertext yang berlaku adalah `0x02`; blob `0x01`
(pra-pengikatan baris) ditolak dan butuh re-enkripsi. `_bidx` sengaja TIDAK terikat baris —
kalau ikut, `WHERE {f}_bidx = $1` berhenti cocok dan `UNIQUE` berhenti menangkap duplikat.

**`tenant_id` di tenant DB**: hanya dipakai bila ada alasan spesifik (partisi hash chain audit,
scope resolver, kompatibilitas single-DB). Default: tidak perlu.

**`version` + `effective_from`**: pola berulang untuk apa pun yang "berubah seiring waktu dengan
audit" (`tenant_configs`, `tenant_workflow_configs`, `workflow_definitions`). Append-only, bisa
rollback, non-retroaktif. Komponen baru yang butuh sifat ini memakai pola yang sama, bukan
membuat mekanisme sendiri (titik ekstensi #7).

---

## 7. Tabel yang disebut konvensi tapi BELUM ada

CLAUDE.md menyebut beberapa tabel sebagai bagian dari rancangan. Berikut yang **belum** ada di
kode, agar tidak ada yang mencarinya sia-sia:

| Tabel | Status |
|---|---|
| `gov.workflow_instances` | belum — instance workflow belum dipersistensi (store definisi & template selection sudah) |
| `gov.rule_versions` | belum — sub-phase rules (ROADMAP) |
| `gov.fiscal_periods` | belum — `core/fiscal` menyusul bersama modul keuangan |
| `gov.tenants`, `gov.modules`, `gov.permissions` | belum — registry modul & permission hidup di kode (manifest), bukan DB |
| `gov.tenant_customizations` | terwujud sebagai `gov.tenant_custom_fields` + `gov.tenant_capability_overrides` |
| `id.tenant_migrations` | belum — tracking lintas tenant untuk admin platform; `gov.migration_history` per-tenant sudah ada |

---

## 8. Peta relasi

```
IDENTITY DB (gov_identity, schema id)
──────────────────────────────────────
persons ─┬─< employments ─< tenant_assignments ─── tenant_id ┐
         ├─< credentials ─< otps                             │
         ├─< central_role_assignments >─ central_roles       │  (string, tanpa FK
         │                                   └─< central_role_permissions
         └─< revoked_tokens                                  │   — beda DB)
                                                             │
tenant_registry (tenant_id PK, db_host, tier, key_custody) ──┤
data_keys (tenant_id, purpose, kind, key_version) ───────────┤
audit_logs (chain tunggal)                                   │
                                                             │
        event sinkronisasi identity.* ─────────────┐         │
                                                   ▼         ▼
TENANT DB (gov_{tenant}, schema gov)
──────────────────────────────────────
user_profiles ──(user_id, tanpa FK)──< user_role_assignments >── tenant_roles
     │                                          │                    └─< tenant_role_permissions
     │                                          └── unit_kerja_id ─▷ org_units (self-parent)
     ├──< delegations (from/to user, berbatas waktu)
     │
workflow_definitions (workflow_id, version)   ◁── tenant_workflow_configs (slot → template_id)
tenant_configs (scope: tenant/unit/resource, ber-versi)
tenant_custom_fields · tenant_capability_overrides
scheduled_jobs ─< job_runs · job_locks
notification_templates · notification_inapp · notification_deliveries
outbox_events · idempotency_keys · sequences · audit_logs · migration_history

TENANT DB, schema modul (mis. surat_masuk)
──────────────────────────────────────
surat_masuks ─< disposisis          (FK hanya intra-modul; lintas modul lewat port)
```

---

## Rujukan

- `docs/DB_CHANGELOG.md` — riwayat setiap perubahan struktur DB
- ADR-004 (koneksi DB multi-tenant), ADR-005 (data residency), ADR-006 (provisioning tenant DB)
- ADR-003 (audit identity sentral), ADR-002 (masking field sensitif audit)
- ADR-009 (klasifikasi data & enkripsi field), ADR-010 (key management & custody)
- ADR-013 (contact seam pada clone `user_profiles`)
- CLAUDE.md — §Konvensi penamaan (database), §Migration strategy, §Klasifikasi data
