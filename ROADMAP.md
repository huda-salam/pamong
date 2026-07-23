# ROADMAP.md — Pamong

Rencana pembangunan framework dari nol sampai fungsional, dipecah menjadi
**Phase → Sub-phase → Jobs/PR**.

Konvensi, aturan arsitektur, dan standar coding ada di `CLAUDE.md`.
File ini hanya mengatur *urutan* dan *batas* tiap pekerjaan agar bisa dikerjakan
inkremental, satu PR satu unit yang reviewable.

---

## Prinsip penyusunan roadmap

- **Satu job = satu PR.** Tiap job dirancang agar bisa di-review dalam satu sesi,
  idealnya < 600 baris perubahan inti (tidak termasuk generated code & test).
- **Dependency eksplisit.** Job tidak boleh dimulai sebelum dependensinya merged.
- **Setiap job menghasilkan sesuatu yang bisa di-test.** Tidak ada job yang hanya
  "menyiapkan" tanpa output yang bisa diverifikasi.
- **Hexagonal dari awal.** Bahkan job paling awal mengikuti pemisahan port/adapter.
- **Definition of Done (DoD) seragam** untuk semua job — lihat bagian akhir.

Penanda dependency: `←` berarti "bergantung pada".

---

## Ringkasan phase

| Phase | Nama | Tujuan | Estimasi |
|---|---|---|---|
| 0 | Bootstrap & fondasi | Repo, config, logging, error, tooling dasar | 2–3 minggu |
| 1 | Domain engine & persistence | Inti framework: registry, entity, DB, audit | 3–4 minggu |
| 2 | Identity, tenancy & auth | Person, employment, persona, role, login | 4–5 minggu |
| 3 | Core services & fleksibilitas | Event bus, workflow (DB), strategy, kustomisasi, scheduler, notif | 5–7 minggu |
| 4 | Rule engine & governance | Tiered constraint, versioning regulasi | 2–3 minggu |
| 5 | Gateway, API & DX | Router, middleware, pamongctl, linter lengkap | 3–4 minggu |
| 6 | Admin UI web | Scaffolding tenant, meta-def, observability | 4–5 minggu |
| 7 | Modul referensi & validasi | surat_masuk + modul publik + E2E | 3–4 minggu |

Total: ~25–34 minggu (6–8 bulan) untuk framework fungsional penuh.
Minimum viable framework (Phase 0–3 + sebagian 5) bisa dicapai di ~14–18 minggu.

---

## Phase 0 — Bootstrap & fondasi

Tujuan: kerangka repo yang bisa di-build, di-test, dan di-lint sejak commit pertama.

### Sub-phase 0.1 — Repo & build system

- **PR-0.1.1** Inisialisasi monorepo
  - Struktur direktori sesuai CLAUDE.md (core, port, infra, gateway, dst)
  - `go.mod`, `go.work` jika perlu workspace, `.gitignore`, `.editorconfig`
  - `Makefile`: target `build`, `test`, `lint`, `run`, `migrate`
  - DoD: `make build` dan `make test` jalan (meski kosong)

- **PR-0.1.2** CI skeleton ← 0.1.1
  - Pipeline: lint → test → build (lihat CLAUDE.md CI/CD gates)
  - Branch protection di `main` dan `staging`
  - DoD: PR dummy memicu pipeline dan lulus

### Sub-phase 0.2 — Fondasi runtime

- **PR-0.2.1** Config loader ← 0.1.1
  - Baca env `GOV_*`, file YAML berlapis, precedence sesuai CLAUDE.md
  - `config.AppConfig` struct + validasi
  - DoD: unit test precedence env > local > env-file > default

- **PR-0.2.2** Structured logging ← 0.2.1
  - Logger JSON dengan correlation ID, level dari config
  - Interface `Logger` (port) + adapter slog/zap
  - DoD: log keluar dengan correlation ID, format JSON

- **PR-0.2.3** Error types & HTTP mapping ← 0.1.1
  - `core.ErrNotFound`, `ErrPermissionDenied`, `ErrValidation`, `ErrConflict`
  - Mapping ke HTTP status code
  - DoD: unit test setiap error type ke status code yang benar

### Sub-phase 0.3 — Tooling dasar

- **PR-0.3.1** pamongctl skeleton ← 0.1.1
  - CLI dengan cobra, perintah kosong: `new`, `validate`, `generate`, `lint`, `migrate`
  - DoD: `pamongctl --help` menampilkan semua perintah

- **PR-0.3.2** Custom linter skeleton ← 0.3.1
  - Kerangka Go `analysis.Analyzer`, satu rule contoh berjalan
  - DoD: `pamongctl lint` mendeteksi pelanggaran rule contoh di file uji

- **PR-0.3.3** testkit base ← 0.2.1, 0.2.2
  - `testkit.Ctx()`, helper assert, mock logger
  - DoD: dipakai di minimal satu test yang sudah ada

---

## Phase 1 — Domain engine & persistence

Tujuan: developer bisa mendefinisikan entity dan framework mengelola persistensi + audit.

### Sub-phase 1.1 — Domain engine

- **PR-1.1.1** Manifest contract & module registry ← 0.2.1
  - Interface `Module`, struct `Manifest`, `Register()`, auto-discovery
  - DoD: dua modul dummy ter-register, registry bisa list keduanya

- **PR-1.1.2** Entity definition & field types ← 1.1.1
  - `EntityDef`, `FieldDef`, tipe field (Text, Date, Enum, Link, File, dll)
  - Validasi struktural via struct tag
  - DoD: entity dummy tervalidasi, field invalid ditolak

- **PR-1.1.3** Lifecycle hooks ← 1.1.2
  - `before_save`, `after_save`, `before_submit`, `after_submit`
  - DoD: hook terpanggil sesuai urutan dalam test

### Sub-phase 1.2 — Persistence & migration

- **PR-1.2.1** DB adapter (Postgres/pgx) ← 0.2.1
  - Connection pool dari config, health check
  - Implementasi `port.Repository` generics
  - DoD: integration test CRUD ke Postgres via testcontainers

- **PR-1.2.2** Table naming enforcement ← 1.1.2, 1.2.1
  - Generate nama tabel `{schema}.{entity_plural}` (schema = nama modul) dari entity def
  - DoD: entity def menghasilkan nama tabel yang benar; nama manual yang salah ditolak

- **PR-1.2.3** Migration runner ← 1.2.1
  - Versioned up/down, multi-tenant aware, rollback
  - `pamongctl migrate up|down|status`
  - DoD: migration jalan & rollback bersih di test DB

- **PR-1.2.4** Auto-generate migration dari entity def ← 1.2.2, 1.2.3
  - `pamongctl generate migration {modul}`
  - DoD: entity baru menghasilkan file migration up+down yang valid

### Sub-phase 1.3 — Audit engine

- **PR-1.3.1** Audit writer & field diff ← 1.2.1
  - Catat before/after, actor, timestamp untuk entity `Auditable`
  - DoD: mutasi entity auditable menghasilkan audit log dengan diff benar

- **PR-1.3.2** Hash chain tamper detection ← 1.3.1
  - Tiap entry menyimpan hash entry sebelumnya
  - `pamongctl audit verify` mendeteksi manipulasi
  - DoD: test memodifikasi log → verifikasi gagal terdeteksi

- **PR-1.3.3** Auto-attach audit ke domain engine ← 1.3.1, 1.1.3
  - Hook audit otomatis untuk semua entity auditable, tanpa kode modul
  - DoD: entity auditable ter-audit tanpa kode tambahan di modul

---

## Phase 2 — Identity, tenancy & auth

Tujuan: model person/employment/persona, multi-tenant, role berlapis, tiga alur login.

### Sub-phase 2.1 — Identity core (central DB)

- **PR-2.1.1** Skema & repo person + employment + credential ← 1.2.3
  - Tabel `id.persons`, `id.employments`, `id.credentials`
  - Repository + port di `identity/domain`
  - DoD: integration test buat person, tambah employment ASN, tambah credential

- **PR-2.1.2** Person resolver & use case dasar ← 2.1.1
  - Create person, attach employment, resolve by NIK/NIP
  - DoD: unit test resolve by NIK & NIP, validasi NIP wajib untuk ASN

### Sub-phase 2.2 — Tenancy

- **PR-2.2.1** Tenant registry ← 1.2.3
  - Tabel `id.tenant_registry` (identity DB sentral — resolver butuh lokasi DB tenant
    sebelum connect), CRUD tenant, status aktif
  - DoD: buat tenant, list tenant, nonaktifkan tenant

- **PR-2.2.2** Tenant resolver middleware ← 2.2.1, 0.2.x
  - Resolusi tenant dari token/subdomain/header, inject ke context
  - DoD: request dengan tenant berbeda terisolasi dalam test

- **PR-2.2.3** Schema-per-tenant provisioning ← 2.2.1, 1.2.3
  - Buat schema + jalankan migration saat tenant baru dibuat
  - DoD: tenant baru otomatis punya schema lengkap

- **PR-2.2.4** Identity sync engine (clone ke tenant) ← 2.1.2, 2.2.3, 3.1.1
  - Subscribe event identity, clone person ke `gov.user_profiles`
  - DoD: event `identity.employment.ditugaskan` menghasilkan clone di tenant tujuan
  - Catatan: bergantung event bus (3.1.1) — bisa pakai memory driver dulu

### Sub-phase 2.3 — Role & permission

- **PR-2.3.1** Permission engine RBAC ← 2.1.1
  - Definisi permission, role, assignment, evaluasi dasar
  - DoD: cek permission untuk role tertentu lulus/tolak sesuai harapan

- **PR-2.3.2** Central roles global & scoped ← 2.3.1
  - Tabel `id.central_roles`, `id.central_role_assignments` + `tenant_scope[]`
  - DoD: global role berlaku semua tenant; scoped hanya di scope-nya

- **PR-2.3.3** Tenant roles ← 2.3.1, 2.2.3
  - Tabel `gov.tenant_roles`, `gov.user_role_assignments`, scope unit kerja
  - DoD: role tenant hanya berlaku di tenant-nya

- **PR-2.3.4** Permission export/import antar modul ← 2.3.1, 1.1.1
  - Bagian `Exports`/`Imports` di manifest, registrasi saat bootstrap
  - DoD: modul A pakai permission export modul B; tanpa import → linter tolak

- **PR-2.3.5** ABAC + hierarki OPD + delegasi/PLT ← 2.3.1, 2.3.3 ✅
  - Atribut unit kerja, tree OPD, delegasi berwaktu
  - DoD: data-level permission per unit kerja; delegasi kedaluwarsa otomatis
  - Selesai: `core/permission.ScopedEngine` (2-tahap), `gov.org_units` (adjacency+CTE),
    `delegation/` (orang→orang, expiry lazy). Wiring Authority live + emitter central→Grant = 2.4.

### Sub-phase 2.4 — Auth flow

- **PR-2.4.1** JWT issue & verify ← 2.1.2 ✅
  - Issue token dengan claim sesuai CLAUDE.md, verifikasi, revocation via jti
  - DoD: token valid diverifikasi; token revoked ditolak
  - Selesai: `port.TokenIssuer/Verifier` + `port.Claims` (seam, gateway tak import identity);
    codec HS256 `identity/adapter/token` (golang-jwt/v5, pin alg); revocation durable
    `id.revoked_tokens` (migrasi 005) + `RevokedTokenStore`; `core.ErrUnauthorized` (401). ADR-007.
    Live wiring middleware + alur login = 2.4.2/2.4.3.

- **PR-2.4.2** gateway.Context / AuthContext ← 2.4.1, 2.3.x
  - Carrier auth+tenant+trace, implementasi `AuthContext`
  - DoD: `RequirePermission`, `IsCitizen`, `HasCentralRole` berfungsi di test

- **PR-2.4.3** Alur login employee (sentral & daerah) ← 2.4.2, 2.2.2 ✅
  - Resolusi tenant, pemilihan tenant, scoped token
  - DoD: user 1-tenant langsung masuk; cross-tenant memilih tenant
  - Selesai: `identity/usecase` `LoginEmployee` (credential NIP/NIK + password → employment aktif →
    penugasan tenant; tunggal=token final, >1=token sementara+daftar) & `SelectTenant`
    (person_id dari klaim tersigning, validasi penugasan aktif). **Login citizen juga di PR ini**
    (`LoginCitizen`, NIK/email/no_hp, tanpa cek employment & tanpa role internal — DoD 2.4.4
    sebagian tertutup). `port.PasswordVerifier` + adapter bcrypt `identity/adapter/auth`.
    INVARIANT: role disaring per-tenant saat mint token (CentralRoleResolver/TenantRoleResolver).

- **PR-2.4.4** Alur login citizen (portal publik) ← 2.4.1 — SELESAI (password 2.4.3 + OTP 2.4.4)
  - Login NIK/email/HP, OTP, persona citizen tanpa cek employment
  - DoD: ASN bisa login publik → token persona=citizen tanpa role internal ✅ (`LoginCitizen`
    password, PR-2.4.3; jalur OTP `VerifyOTP`, PR-2.4.4; diuji `TestLoginCitizen_*` +
    `TestVerifyOTP_Success_IssuesCitizenToken_NoInternalRoles`)
  - Jalur OTP (no_hp/email tanpa password): `RequestOTP`+`VerifyOTP`, crypto/rand+bcrypt,
    sekali-pakai, cap tebak; rate-limit per-kredensial via `port.RateLimiter` (Opsi B). ADR-008,
    REVIEW_BACKLOG A6. Live wiring HTTP/messaging/ratelimit konkret → Phase 5.1.1.

- **PR-2.4.5** Cross-tenant assignment ← 2.4.3, 2.3.2 — SELESAI
  - Penugasan lintas tenant dengan otorisasi admin sentral
  - DoD: assignment cross-tenant butuh permission khusus; PLT bisa pilih 2 tenant ✅
    (`validateAssignment`: employment aktif + tenant aktif di registry + anti-duplikat;
    `TenantRegistry` diinject ke `AssignEmploymentToTenant`; 3 error domain baru;
    5 test baru + 4 test lama diupdate. Security review: tidak ada temuan.)

---

## Phase 3 — Core services & fleksibilitas

Tujuan: event-driven, workflow yang bisa diubah, scheduler, notifikasi, storage, metrics.

### Sub-phase 3.1 — Event bus

- **PR-3.1.1** Port event bus + memory driver ← 0.2.1
  - Interface publish/subscribe, schema registry, driver memory untuk test
  - DoD: publish/subscribe lokal lulus; event tanpa schema ditolak

- **PR-3.1.2** Outbox pattern ← 3.1.1, 1.2.1 — SELESAI
  - Event tersimpan transaksional, dikirim setelah commit
  - DoD: rollback transaksi → event tidak terkirim ✅
    (`gov.outbox_events` + `EnsureOutboxSchema`; `OutboxStore` implements
    `port.EventPublisher` — INSERT dalam tx bisnis; `OutboxRelay` poll
    SELECT FOR UPDATE SKIP LOCKED + dispatch via driver + mark dispatched_at;
    `SchemaRegistry.Unmarshal` reconstruct typed payload. Security review: clear.)

- **PR-3.1.3** Driver NATS/Redis Streams ← 3.1.1 — SELESAI
  - Driver produksi, dipilih via config
  - DoD: integration test publish/subscribe lintas proses ✅
    (NATS Core driver: `wire.go` serialisasi JSON lintas transport; `nats.go`
    `NATSDriver` Subscribe/Dispatch; `factory.go` `NewFromConfig` switch by config;
    4 integration test embed NATS server. Redis DEFERRED. Security review: clear.)

- **PR-3.1.4** DLQ & retry ← 3.1.3 — SELESAI
  - Retry backoff, dead letter queue, alert
  - DoD: handler gagal → masuk DLQ setelah N retry ✅
    (`retry.go` RetryPolicy eksponensial + cap; `outbox.go` DDL tambah
    next_retry_at+failed_at, SELECT filter DLQ+backoff, relay mark DLQ via
    slog.Error dlq=true; `nats.go` log structured handler error; `config/schema.go`
    3 field retry GOV_EVENTBUS_RETRY_*; 7 unit test RetryPolicy + 2 integration test
    DLQ+backoff. Security review: clear.)

### Sub-phase 3.2 — Workflow engine

- **PR-3.2.1** State machine core ← 1.1.1 — SELESAI
  - State, transition, action hook. Action HANYA boleh memanggil use case.
  - DoD: transisi valid jalan; transisi ilegal ditolak; action tanpa use case ditolak ✅
    (definition.go WorkflowDefinition/State/Transition/NotifySpec; instance.go
    WorkflowInstance+TransitionRecord; ports.go ActionDispatcher/GuardEvaluator/
    DefinitionStore; store.go MemoryStore dengan validasi saat Register; engine.go
    Engine.Start+Execute — guard AND, action dispatch, history; 14 unit test DoD.
    Security review: clear.)

- **PR-3.2.2** YAML seed loader + schema validation ← 3.2.1 — SELESAI
  - Muat definisi workflow dari YAML (baseline) → validasi struktur
  - DoD: YAML valid termuat; YAML invalid ditolak dengan pesan jelas
  - `ParseYAML([]byte)`, `SeedYAML(data, store)` (idempoten: skip jika sudah ada),
    `LoadYAML(path, store)`. states YAML map → sort+konversi ke []State; validasi
    struktural didelegasikan ke validateDefinition. 9 unit test + security review: clear.
    (commit `5a46539`)

- **PR-3.2.3** Workflow definition store (DB) ← 3.2.2, 1.2.3 — SELESAI
  - Simpan definisi ke DB, seed di-load saat bootstrap, override per-tenant
  - Versioned + effective date + audit siapa-mengubah-apa
  - DoD: definisi dari DB dieksekusi; perubahan ber-versi & ter-audit ✅
    (Validate() diekspor dari core/workflow/store.go; migration SQL
    core/workflow/migrations/001_create_workflow_definitions.{up,down}.sql;
    infra/workflow/db_store.go — DBStore: EnsureSchema, Register, RegisterAsActor,
    Get, GetVersion; SeedYAML idempoten via Get-sebelum-Register di loader;
    6 integration test lulus; security review: clear.)

- **PR-3.2.4** Template selection per-tenant ← 3.2.3 — SELESAI
  - Tenant memilih template ber-key + parameter binding peran→jabatan
  - DoD: tenant A & B jalan dengan template berbeda, use case identik ✅
    (port `TemplateStore` + `TenantWorkflowConfig` + `ApplyBindings` +
    `MemoryTemplateStore` di core/workflow/template.go; `DBTemplateStore` di
    infra/workflow/template_store.go — UPSERT pada (tenant_id, slot);
    migration core/workflow/migrations/002_create_tenant_workflow_configs.{up,down}.sql;
    15 unit test + 4 integration test; security review: clear.)
  - CATATAN: pilihan template belum ber-versi/ter-audit dan `template_id`+`role_bindings`
    belum divalidasi saat tulis — lihat backlog "[PR-3.3.2] Rekonsiliasi penyimpanan
    template selection" butir (a)-(d) dan "[PR-3.6.x] Konsumsi role binding".
    Belum ada use case admin / handler HTTP: store baru dipakai dari kode bootstrap.

- **PR-3.2.5** Guard expression DSL ← 3.2.3, 2.4.2
  - Evaluator ekspresi boolean, di-compile saat load, tanpa side-effect
  - DoD: guard mengevaluasi konteks actor & entity; syntax error ketahuan saat load

- **PR-3.2.6** SLA, deadline & eskalasi ← 3.2.1, 3.6.1
  - Batas waktu per state, eskalasi otomatis saat lewat
  - DoD: state lewat SLA memicu eskalasi & notifikasi

- **PR-3.2.7** Workflow history & instance versioning ← 3.2.3, 1.3.1
  - Riwayat transisi immutable; instance berjalan pakai versi definisi saat mulai
  - DoD: perubahan definisi tidak mengubah instance yang sedang berjalan

### Sub-phase 3.3 — Strategy registry & tenant config

- **PR-3.3.1** Strategy registry + key resolution ← 1.1.1 ✅
  - Interface + registry ber-key + `Register()`; tolak key tak terdaftar
  - DoD: dua strategy dummy ter-register; use case memilih via key dari config

- **PR-3.3.2** Tenant config ber-scope + resolver ← 3.3.1, 2.2.1 ✅ (inti)
  - Skema config `tenant[/unit/resource]`, resolusi paling-spesifik-menang
  - DoD: config tenant terbaca; scope unit kerja meng-override tenant
  - SELESAI: `gov.tenant_configs` + `core/config.Resolver` + `infra/config` store +
    `strategy.ConfigSelectionSource` (ganti MemorySelectionSource). SISA: rekonsiliasi
    template selection versi/effective-date/validasi SELESAI di PR-3.3.2b store-level; sisa hanya use case admin + permission (butir c).

- **PR-3.3.3** Strategy choice versioning + non-retroaktif ← 3.3.2, 1.3.1 ✅
  - Pilihan ber-versi + effective date; periode terkunci tak berubah
  - DoD: ganti metode → periode lama tetap, periode baru pakai metode baru
  - SELESAI: `tenant_configs` jadi append-only ber-versi (version + effective_from);
    `Resolver.ResolveAsOf` (spesifisitas > kebaruan); `ChoiceManager.SetChoice` (set_by
    sebagai jejak + gerbang periode via `port.FiscalChecker` seam). Versi append-only itu
    sendiri = jejak "siapa-mengubah-apa" (pola workflow_definitions), bukan audit_logs terpisah.
    Gerbang fiskal REAL menunggu impl `FiscalChecker` (DEFERRED modul keuangan); seam + baca
    non-retroaktif sudah jalan. **Utang template selection a–d TIDAK ditutup di sini** (tabel
    `tenant_workflow_configs` beda) → tetap di 3.3.2b, kini tinggal meniru pola ini.

- **PR-3.3.4** Opsi = irisan developer ∩ rule tier ← 3.3.1, 4.1.3
  - Opsi tersedia ke tenant difilter rule tiered constraint
  - DoD: strategy yang dilarang rule nasional tak muncul sebagai opsi
  - Catatan: butuh rule engine (4.1.3) — bisa stub dulu, lengkapi setelah Phase 4

- **PR-3.3.5** Hook validator koherensi kombinasi ← 3.3.1 ✅
  - Titik daftar validator lintas-pilihan (belum tentu dipakai, titiknya disiapkan)
  - DoD: kombinasi tak koheren yang didaftarkan terdeteksi & ditolak
  - SELESAI: `core/strategy/coherence.go` `CoherenceRegistry` (Register nama-unik + Validate
    jalankan semua, urutan deterministik). Titik ekstensi #5 disiapkan; belum di-wire ke write
    path (dipanggil use case admin saat tenant ubah pilihan — menyusul bersama 3.3.2b).

### Sub-phase 3.4 — Tenant customization layer

- **PR-3.4.1** Custom field & label override ← 1.1.2, 2.2.1
  - Layer terpisah dari definisi modul; upgrade-safe
  - DoD: tenant menambah field tanpa mengubah modul; upgrade tak menimpa

- **PR-3.4.2** Capability flags per-tenant ← 2.2.1
  - Gate fitur dormant tanpa percabangan kode menyebar
  - DoD: fitur ber-flag aktif/nonaktif per-tenant tanpa rilis terpisah

### Sub-phase 3.5 — Scheduler

- **PR-3.5.1** Cron & job queue ← 0.2.1 ✅
  - Penjadwalan, eksekusi, riwayat job
  - DoD: job terjadwal jalan tepat waktu di test
  - Impl: parser cron 5-field murni (no lib), Registry handler ber-key (titik ekstensi #1),
    Runner (RunDue/Trigger/Replay/Start), JobStore port + MemoryJobStore + Postgres
    (gov.scheduled_jobs + gov.job_runs). One-shot (cron kosong) = seam deadline SLA (F2).
    Anti double-run multi-instance DITUNDA ke 3.5.2 (lock).

- **PR-3.5.2** Distributed lock ← 3.5.1, 3.1.x
  - Job tidak double-run di multi-instance
  - DoD: dua instance, job jalan sekali

### Sub-phase 3.6 — Notification & messaging

- **PR-3.6.1** Channel abstraction + template engine ← 3.1.1
  - Port channel, template per tenant, i18n
  - DoD: kirim notif in-app & email (mock) dengan template benar

- **PR-3.6.2** Routing by role/jabatan ← 3.6.1, 2.3.x
  - Notif ke role/jabatan, fallback ke PLT
  - DoD: notif ke "Kadis" jatuh ke PLT bila jabatan kosong

### Sub-phase 3.7 — Storage & metrics ports

- **PR-3.7.1** Storage port + MinIO/S3 adapter ← 0.2.1
  - Upload/download/delete, metadata
  - DoD: integration test simpan & ambil file dari MinIO

- **PR-3.7.2** Metrics port + Prometheus/OTEL adapter ← 0.2.2
  - Counter, gauge, histogram; tracing OTEL
  - DoD: metric tereskpos di endpoint; trace muncul di collector

### Sub-phase 3.8 — Klasifikasi data & enkripsi field (ADR-009/010)

Tujuan: enkripsi field selektif (pengenal + data spesifik) at-rest dengan blind index,
tanpa mematikan lookup/UNIQUE. WAJIB selesai sebelum tenant produksi pertama — biaya
migrasi naik seiring data & entity. Lihat ADR-009 (klasifikasi/enkripsi) & ADR-010 (KMS
pluggable + custody sebagai kebijakan per-tenant).

- **PR-3.8.1** `DataClass` di `FieldDef` + validasi + DDL multi-kolom ← 1.1.x (core/domain)
  - Tambah `Class`/`Searchable`; `Validate()` tolak kombinasi mustahil; `columnDef` → N kolom
    (`_enc`+`_bidx`) untuk field terenkripsi. **Murah sekarang** (satu-satunya konsumen
    EntityDef produksi = surat_masuk, tanpa pengenal).
  - DoD: entity dengan field `personal_id` meng-generate dua kolom; validasi menolak
    `Unique`+terenkripsi+`!Searchable`; entity lama tetap kompilasi & lulus test.

- **PR-3.8.2** `port/crypto.go` + `infra/crypto` (AES-256-GCM + blind index) ← 3.8.1
  - CryptoPort (Encrypt/Decrypt/BlindIndex); KeyProvider registry + envelope; DEK store
    `id.data_keys`; driver `static` (KMS-alike bawaan, master KEK ber-versi — **default
    produksi Tier 1/2**) + `local` (dev/test); format ciphertext self-describing. KMS eksternal
    (vault/aws-kms/bssn) di-plug kelak tanpa ubah kode.
  - DoD: roundtrip; ciphertext beda tiap panggilan; blind index deterministik; isolasi per
    tenant; `static` menolak start tanpa master key valid + rotasi V1→V2 jalan.

- **PR-3.8.3** Enkripsi transparan di lapis repository ← 3.8.2, 1.2.1
  - infra/db enkripsi saat tulis + blind index + dekripsi saat baca; equality/UNIQUE → `_bidx`.
    Otomatis dari `FieldDef.Class`, bukan use case.
  - DoD: CRUD entity ber-`personal_id` bekerja; kolom `_enc` di DB bukan plaintext; lookup jalan.

- **PR-3.8.4** Enkripsi diff audit sensitif (tutup E2) ← 3.8.2, core/audit
  - Diff class `personal_id`/`specific` terenkripsi; raw tetap bukti; read-gated
    `audit:sensitive:baca`. Hash chain tetap verify.
  - DoD: dump `gov.audit_logs.diff` tak memuat NIK plaintext; verify integritas lulus.

- **PR-3.8.5** Tutup jalur kebocoran samping ← 3.8.3 (ADR-009 §6)
  - Payload event, `gov.idempotency_keys`, staging table migrasi, log/trace, clone
    `gov.user_profiles`.
  - DoD: tiap jalur tak membocorkan pengenal mentah (test per-jalur).

- **PR-3.8.6** Migrasi identity UNIQUE→blind index ← 3.8.3
  - `nik`/`nip`/`cred_value`/`no_hp`/`email` → `_enc`+`_bidx`; UNIQUE pindah; backfill (dev
    kosong = gratis). SENSITIF (identity) — review ekstra.
  - DoD: login & resolve by NIK/NIP/email tetap jalan lewat blind index; UNIQUE ditegakkan di `_bidx`.

- **PR-3.8.7** Generator `docs/contracts/data-inventory.md` (pamongctl) ← 3.8.1
  - Inventaris field ber-`Class` dari manifest — artefak kepatuhan UU PDP yang tak basi.
  - DoD: `pamongctl` regenerate; diff PR menampilkan perubahan klasifikasi.

> KMS = driver ber-registry (`GOV_CRYPTO_KMS_DRIVER`); custody = kebijakan per-tenant
> (`key_custody`) — ADR-010. Enkripsi jalan penuh dengan driver `local` (dev/test); driver
> produksi & nilai custody Tier 3 di-plug saat onboarding per-pemda, bukan blokir roadmap.
> Sub-phase ini bisa menambah **PR-3.8.8** (driver KMS produksi + resolver custody) saat
> pengadaan menentukan KMS.

---

## Phase 4 — Rule engine & governance

Tujuan: regulasi sebagai data, constraint bertingkat, bisa diubah tanpa redeploy.

### Sub-phase 4.1 — Rule engine

- **PR-4.1.1** Rule store (DB-backed) ← 1.2.1
  - Tabel `gov.rule_versions`, CRUD rule
  - DoD: rule tersimpan & terambil dengan effective date

- **PR-4.1.2** Expression evaluator ← 4.1.1
  - Evaluasi ekspresi rule terhadap konteks data
  - DoD: rule `belanja/total <= 0.30` dievaluasi benar

- **PR-4.1.3** Tiered constraint ← 4.1.2
  - Hierarki nasional > provinsi > kab/kota; tier bawah tak bisa langgar atas
  - DoD: kab/kota tak bisa set lebih longgar dari provinsi

- **PR-4.1.4** Versioning & effective date ← 4.1.1
  - Rule berlaku per tanggal, backtest, riwayat
  - DoD: rule lama & baru aktif sesuai tanggal transaksi

- **PR-4.1.5** Conflict detector ← 4.1.3
  - Deteksi dua rule bertentangan sebelum aktivasi
  - DoD: rule konflik ditolak saat aktivasi dengan pesan jelas

### Sub-phase 4.2 — Custom evaluator

- **PR-4.2.1** Registrasi Go custom evaluator ← 4.1.2
  - `rules.Register()` untuk logika yang tak bisa diekspresikan DSL
  - DoD: custom evaluator terpanggil engine dalam test

---

## Phase 5 — Gateway, API & DX

Tujuan: API gateway lengkap, pamongctl lengkap, linter lengkap, dokumentasi kontrak.

### Sub-phase 5.1 — API gateway

- **PR-5.1.1** Router aggregator ← 1.1.1, 2.4.2
  - Kumpulkan rute dari semua modul saat bootstrap
  - DoD: rute modul ter-register & dapat diakses

- **PR-5.1.2** Middleware stack ← 5.1.1, 2.2.2, 1.3.1
  - Auth, rate limit, tenant resolver, CORS, audit trail
  - DoD: request tanpa auth ditolak; rate limit aktif; audit tercatat

- **PR-5.1.3** Auto-generate CRUD endpoint ← 5.1.1, 1.1.2
  - Endpoint CRUD dasar dari entity def
  - DoD: entity baru otomatis punya endpoint GET/POST/PATCH/DELETE

### Sub-phase 5.2 — pamongctl lengkap

- **PR-5.2.1** Scaffold module ← 1.1.1, 0.3.1
  - `pamongctl new module` generate struktur hexagonal lengkap
  - DoD: modul hasil scaffold langsung lulus `validate` & `build`

- **PR-5.2.2** Validate & rule management ← 5.2.1, 4.1.x
  - `pamongctl validate module`, `pamongctl rule create|preview|activate`
  - DoD: manifest invalid terdeteksi; rule bisa dikelola via CLI

### Sub-phase 5.3 — Linter lengkap

- **PR-5.3.1** Semua analyzer rules ← 0.3.2, semua phase sebelumnya
  - 10+ rule sesuai CLAUDE.md (no-infra-import, must-check-permission, dll)
  - DoD: tiap rule punya test positif & negatif; terpasang di CI

### Sub-phase 5.4 — Dokumentasi kontrak

- **PR-5.4.1** OpenAPI generation ← 5.1.3
  - Generate spec OpenAPI dari rute & entity
  - DoD: spec tergenerate & valid

- **PR-5.4.2** Event topology & permission docs ← 3.1.1, 2.3.4
  - Generate diagram produce/consume & daftar permission ke `docs/contracts/`
  - DoD: dokumentasi tergenerate dari manifest

---

## Phase 6 — Admin UI web

Tujuan: scaffolding tenant & meta-definition lewat web, observability dashboard.

### Sub-phase 6.1 — Shell & auth

- **PR-6.1.1** Admin UI shell (Frappe UI + Go adapter) ← 5.1.2
  - Layout, integrasi auth, tenant switcher
  - DoD: login admin, pindah tenant, layout tampil

### Sub-phase 6.2 — Meta-definition UI

- **PR-6.2.1** Module & entity browser ← 6.1.1, 1.1.2
  - Lihat modul ter-register, entity, field, relasi
  - DoD: semua modul tampil dengan detail dari registry

- **PR-6.2.2** Entity definition editor ← 6.2.1, 1.2.4
  - Definisi/edit entity via form → generate migration
  - DoD: buat entity via UI menghasilkan migration valid

### Sub-phase 6.3 — Tenant scaffolding UI

- **PR-6.3.1** Tenant management ← 6.1.1, 2.2.x
  - Buat tenant, provisioning schema, status
  - DoD: tenant baru via UI otomatis ter-provision

- **PR-6.3.2** User & role management ← 6.3.1, 2.3.x
  - Assign person ke tenant, kelola role tenant, cross-tenant assignment
  - DoD: assign role & cross-tenant lewat UI, audit tercatat

### Sub-phase 6.4 — Observability dashboard

- **PR-6.4.1** Audit trail viewer ← 6.1.1, 1.3.x
  - Telusur audit log, filter, verifikasi hash chain
  - DoD: audit log tampil & dapat diverifikasi via UI

- **PR-6.4.2** Workflow & event monitor ← 6.1.1, 3.1.x, 3.2.x
  - Status workflow instance, topology event, DLQ
  - DoD: instance & event bus termonitor via UI

---

## Phase 7 — Modul referensi & validasi

Tujuan: buktikan framework usable end-to-end lewat modul nyata + interaksi internal-publik.

### Sub-phase 7.1 — Modul internal referensi

- **PR-7.1.1** surat_masuk — domain & use case ← Phase 1–5
  - Entity, port, use case create/disposisi, unit test
  - DoD: use case lulus unit test, coverage sesuai target

- **PR-7.1.2** surat_masuk — adapter & workflow ← 7.1.1, 3.2.x
  - Repository, handler, workflow disposisi YAML
  - DoD: alur disposisi jalan end-to-end di integration test

### Sub-phase 7.2 — Modul publik referensi

- **PR-7.2.1** Modul layanan publik (citizen-facing) ← 7.1.x, 2.4.4
  - Contoh: cek status surat oleh masyarakat via persona citizen
  - Interaksi ke surat_masuk lewat service port, bukan akses langsung
  - DoD: citizen bisa cek status; tidak ada akses langsung ke DB internal

### Sub-phase 7.3 — Validasi menyeluruh

- **PR-7.3.1** End-to-end test suite ← 7.1.x, 7.2.x
  - Skenario lengkap: buat surat → disposisi → notifikasi → cek publik
  - DoD: skenario E2E lulus di CI

- **PR-7.3.2** Contract test antar modul ← 7.3.1
  - Verifikasi schema event & port stabil
  - DoD: perubahan breaking terdeteksi test

### Sub-phase 7.4 — Onboarding

- **PR-7.4.1** Dokumentasi developer & walkthrough ← semua
  - Panduan "buat modul pertama", referensi ke surat_masuk
  - DoD: developer baru bisa ikuti panduan sampai modul jalan

---

## Definition of Done (berlaku semua job)

Sebuah PR dianggap selesai jika:

1. `go build ./...` dan `go test ./... -race` lulus
2. `pamongctl lint ./...` bersih (tanpa pengecualian baru)
3. Coverage layer sesuai target di CLAUDE.md
4. Unit test untuk happy path + minimal satu jalur gagal
5. Integration test bila menyentuh adapter (DB/event/storage)
6. Migration punya pasangan down (bila ada migration)
7. Event/permission baru terdaftar di manifest & terdokumentasi
8. ADR dibuat bila menyentuh interface publik core
9. PR description mengikuti template di CLAUDE.md
10. Tidak ada `TODO`/`FIXME` tanpa issue terkait

---

## Jalur kritis & paralelisasi

**Jalur kritis (tidak bisa diparalelkan):**
```
0.1 → 0.2 → 1.1 → 1.2 → 2.1 → 2.3 → 2.4 → 5.1 → 7.1 → 7.3
```

**Yang bisa dikerjakan paralel setelah Phase 1 selesai:**
- Phase 3 (event bus, workflow, scheduler, notif, storage) — tim A
- Phase 2 (identity, tenancy, auth) — tim B
- Phase 4 (rule engine) bisa mulai setelah 1.2 — tim C

**Catatan dependency lintas-phase:**
- 2.2.4 (identity sync) butuh 3.1.1 (event bus memory driver) — kerjakan 3.1.1 lebih awal
- 3.2.6 (SLA eskalasi) butuh 3.6.1 (notifikasi)
- 6.x (UI) butuh 5.1 (gateway) stabil
- 7.x (modul referensi) adalah validasi akhir — butuh hampir semua phase

**Minimum viable framework** (cukup untuk mulai bangun modul bisnis pertama):
Phase 0 + 1 + 2 + 3.1 + 3.2 + 3.3 + 5.1 + 5.2. Strategy registry (3.3) masuk MVP
karena modul keuangan butuh selectable policy (FIFO/average, aset/beban) sejak awal.
Customization layer (3.4), scheduler lanjutan, UI, dan notifikasi lengkap bisa
menyusul sambil modul bisnis pertama dikembangkan.

---

## Backlog teknis (utang yang ditemukan saat implementasi)

Item yang sengaja ditunda dengan pemetaan ke phase/PR tempat pengerjaannya. Bukan PR
baru tersendiri kecuali disebut — diselesaikan saat phase terkait dikerjakan.

Penundaan substantif yang ditandai di kode dengan `// DEFERRED(Phase-X.Y | PR-X.Y.Z): ...`
(CODE_CONVENTION §9) wajib punya entri padanan di sini, sehingga marker di kode dan
backlog ROADMAP selalu sinkron.

**Audit DEFERRED saat tutup fase.** Karena DEFERRED sah berumur panjang (tak ditagih
per-milestone seperti TODO/FIXME), saat menutup sebuah Phase/sub-phase jalankan
`grep -rn 'DEFERRED(' --include='*.go'` dan pastikan tak ada penanda yang Phase/PR
tujuannya sudah tiba/lewat tanpa dikerjakan. DEFERRED yang fasenya lewat = utang yang
harus ditutup atau dijadwalkan ulang secara eksplisit. Ini gerbang manusia (belum ada
rule linter `markerref`).

- **[PR-3.3.2 — bagian INTI SELESAI] Tenant config ber-scope + resolver.** `gov.tenant_configs`
  (KV ber-scope `tenant[/unit_kerja[/resource]]`) + `core/config.Resolver` "paling spesifik
  menang" + `core/config.TenantConfigStore` (Memory + Postgres `infra/config`) + migration
  `core/config/migrations/001`. `core/strategy` kini memakai `ConfigSelectionSource` di atas
  resolver ini sebagai jalur produksi (MemorySelectionSource tinggal untuk test). DoD terpenuhi:
  scope unit kerja meng-override tenant (unit-test + integration test). **SISA (belum dikerjakan):
  rekonsiliasi template selection SELESAI store-level di PR-3.3.2b; sisa hanya use case admin + permission (butir c, lihat backlog).**

- **[PR-3.3.2b] Rekonsiliasi penyimpanan template selection.** PRD workflow F4
  menyebut pilihan template "disimpan di gov.tenant_configs", tapi tabel/resolver itu baru hadir di
  PR-3.3.2 (tenant config ber-scope, kini SELESAI di atas). PR-3.2.4 tidak bergantung pada 3.3.2,
  jadi pilihan template + role binding disimpan di tabel khusus `gov.tenant_workflow_configs`
  (`infra/workflow/template_store.go`, migration `core/workflow/migrations/002`), PK natural
  `(tenant_id, slot)` — flat, belum ber-scope unit kerja. Alasan tabel terpisah: binding peran
  adalah data terstruktur (map), tidak pas di KV flat `tenant_configs`.
  **KEPUTUSAN 3.3.2 (final):** template selection **TETAP di tabel khusus** `gov.tenant_workflow_configs`
  secara **utuh** (`template_id` + `role_bindings` satu baris) — BUKAN dilebur ke KV `gov.tenant_configs`,
  DAN BUKAN di-split (`template_id`→KV, `role_bindings`→tabel khusus).
  Pertimbangan (ringkas):
  - **Bentuk mengikuti data.** `tenant_configs` adalah KV **skalar** ber-scope (satu string per key,
    resolusi paling-spesifik-menang) — cocok untuk strategy key/flag, tidak untuk `role_bindings` (map).
    Presenden ERP menaruh config terstruktur di tabel sendiri: iDempiere `AD_WF_Responsible` (tanggung
    jawab node terpisah dari KV `AD_Preference`), Odoo `tier.definition` (approval berlapis = record,
    bukan `ir.property`), Frappe transisi workflow di child table (bukan `tabSingles`).
  - **Ringan.** Satu pilihan logis = satu tulis atomik di satu tabel; tak ada konsistensi lintas-tabel,
    tak ada kopling baru workflow↔config. Opsi split (à la Odoo: skalar→config, struktur→record) memberi
    "template per-unit gratis" lewat resolver, tapi memecah satu pilihan ke dua tabel — ditolak karena
    per-unit template BUKAN kebutuhan sekarang dan jalur menambahkannya nanti murah.
  - **Adaptif.** Bila kelak butuh pilihan template per-unit-kerja: tambah kolom scope
    (`unit_kerja_id`/`resource_id`) ke `tenant_workflow_configs` + resolusi paling-spesifik di
    `TemplateStore` (pola yang sama dgn `tenant_configs`). Ini selaras arah **Odoo v17** yang justru
    MENINGGALKAN tabel KV vertikal (`ir.property`) demi "nilai dekat baris + scope" — memindah ke KV
    vertikal = langkah mundur. Dimensi scope tambahan (user/konteks, à la iDempiere `AD_Preference`)
    bila perlu = perluas `ConfigScope`, bukan tabel baru.
  `TemplateStore` (port di `core/workflow/ports.go`) sudah jadi seam — penyimpanan bisa diganti tanpa
  menyentuh engine/caller.

  **Riwayat & audit pilihan template — STORE-LEVEL SELESAI (PR-3.3.2b), sisa (c) menunggu use case.**
  `SetTenantTemplate` dulu UPSERT murni pada `(tenant_id, slot)` (pilihan lama hilang). Kini
  append-only ber-versi (migration `core/workflow/migrations/003`), meniru pola PR-3.3.3:
  - (a) ✅ `version` + `effective_from` — `TenantWorkflowConfig` += Version/EffectiveFrom;
    `SetTenantTemplate` append (Memory & DB); `GetTenantConfig`=terbaru; `GetTenantConfigVersions`
    baca seluruh versi (riwayat/rollback).
  - (b) ✅ (via pola workflow_definitions, bukan audit_logs terpisah) — versi append-only +
    `set_by` = jejak siapa-mengubah-apa-kapan. Konsisten keputusan PR-3.3.3.
  - (d) ✅ `TemplateChoiceManager.SetChoice` (core/workflow/template_choice.go) memvalidasi
    `template_id` terhadap `DefinitionStore` SAAT TULIS + stamp `set_by`; jalur seed
    `SetTenantTemplate` sengaja tetap tanpa validasi (template boleh diseed setelah config).
  - (c) ⏳ **BELUM**: permission check + use case admin. `TemplateChoiceManager` sudah jadi seam
    yang dipanggil use case, TAPI use case admin + permission string BELUM dibuat (scope 3.3.2b
    sengaja store-level; permission dibahas saat write path di-wire ke gateway). Sampai (c) ada,
    **jangan buka pilihan template ke UI admin tenant.** Slot validasi "template sah UNTUK slot itu"
    (cegah arahkan slot ke definisi modul lain) juga milik use case ini.

- **[PR-3.6.x] Konsumsi role binding saat notifikasi/eskalasi.** `ApplyBindings` (PR-3.2.4)
  mengganti peran generik → role konkret tenant pada `State.EscalateToRole` & `NotifySpec.ToRole`,
  tapi Engine sekarang mengambil definisi lewat `DefinitionStore` (unbound) dan belum menyentuh
  Notify/EscalateToRole sama sekali (notifikasi DEFERRED PR-3.6.x). Alur pemilihan: caller
  `TemplateStore.GetForTenant(tenant, slot)` → dapat def ber-binding → `Engine.Start(def.ID)`.
  Saat notifikasi di-wire (3.6.x), pengirim notif WAJIB me-resolve peran lewat binding tenant
  (pakai def hasil `GetForTenant`, bukan `DefinitionStore.Get` mentah), lalu resolusi role→orang
  (PLT fallback) di core/permission + kepegawaian. Engine tetap tenant-agnostik (bicara PERAN).

  **Prasyarat keamanan saat binding mulai dikonsumsi.** Selama notifikasi belum di-wire,
  `RoleBindings` tidak berdampak: ia hanya mengganti NAMA peran tujuan, tidak memberi permission,
  dan tidak ada yang membacanya. Begitu 3.6.x menyalakan notifikasi, binding berubah menjadi
  jalur yang menentukan SIAPA MENERIMA DOKUMEN — dan nilainya saat ini tidak divalidasi sama
  sekali (map string bebas). Maka di 3.6.x (atau di use case admin bila itu lebih dulu): nilai
  binding WAJIB dibatasi ke role yang benar-benar terdaftar di `gov.tenant_roles` milik tenant
  tersebut, agar notifikasi tidak bisa diarahkan ke nama role di luar tenant. Validasi ini
  dilakukan saat TULIS (use case admin), bukan saat baca — supaya config yang tersimpan selalu
  dalam keadaan sah.

- **[belum terjadwal] Isolasi integration test — semua paket berbagi satu database.**
  Tiap paket integration test me-reset schema-nya sendiri saat setup (`DROP TABLE` +
  `CREATE SCHEMA IF NOT EXISTS gov`/`id`) di DB yang sama. Saat `go test` menjalankan paket
  secara paralel (default `-p NumCPU`), mereka saling menimpa: `CREATE SCHEMA IF NOT EXISTS`
  BUKAN operasi atomik di Postgres — dua koneksi yang memeriksa bersamaan sama-sama lolos lalu
  satu kalah di unique index `pg_namespace_nspname_index` — dan satu paket bisa men-`DROP`
  schema yang baru dibuat paket lain (`schema "gov" does not exist` di tengah test).
  Ditambal sementara dengan `-p 1` di `.gitea/workflows/ci.yaml` (menyerialkan paket, semua
  hijau) — itu menyembunyikan gejala, bukan menyembuhkan, dan memperlambat CI.
  Perbaikan sebenarnya: tiap paket test memakai database sendiri (`pamong_test_<paket>`) atau
  schema bernama acak per-run, sehingga tidak ada state bersama sama sekali. Setelah itu
  `-p 1` dicabut.

  Kaitannya dengan butir `go:embed` di bawah: race `CREATE SCHEMA IF NOT EXISTS` yang sama
  akan muncul DI PRODUKSI begitu `EnsureSchema` dipanggil saat bootstrap aplikasi — dua replika
  server yang boot berbarengan terhadap satu tenant DB adalah persis kondisi yang meledak di
  test tadi. Saat ini belum terjadi karena `EnsureSchema` TIDAK punya pemanggil non-test
  (hanya setup test), tapi komentar di kodenya menyatakan niat memakainya untuk bootstrap.
  Karena itu DDL sebaiknya dipindah ke migrator (dijalankan sekali, sengaja) alih-alih
  dijalankan tiap proses saat boot. Bila `EnsureSchema` tetap dipertahankan untuk dev/test,
  bungkus dengan `pg_advisory_xact_lock` — idiom yang sudah dipakai `infra/db/audit.go:84`
  untuk melindungi hash chain audit.

- **[belum terjadwal] Migrasi core & identity tidak dijalankan migrator — satukan lewat
  `go:embed`.** `db.LoadMigrations(fs.FS)` (`infra/db/migration.go`) generik: ia mencari pola
  `*/migrations/*.sql` pada FS apa pun. Tapi `tools/pamongctl/migrate.go` mengunci akarnya ke
  flag `--modules` yang default-nya `"modules"`, sehingga HANYA `modules/*/migrations/` yang
  dimuat. Akibatnya `core/workflow/migrations/001-002` dan `identity/migrations/001-006` tidak
  pernah tersentuh `pamongctl migrate up`: tidak masuk `gov.migration_history`, tak punya
  status, tak bisa di-`down`. Yang benar-benar membuat tabelnya adalah `EnsureSchema` dengan
  DDL inline sebagai const Go — 9 file memakai pola ini (`infra/db/audit.go`,
  `infra/eventbus/outbox.go`, `infra/workflow/db_store.go`, `infra/workflow/template_store.go`,
  `identity/sync/writer_tenantdb.go`, `tenantrole/adapter/db/*`, `delegation/adapter/db/schema.go`).
  Jadi tiap komponen punya DDL ganda: const Go yang AKTIF + file `.sql` yang DEKORATIF.

  Arah perbaikan (PR tersendiri — menyentuh core + identity + CLI + urutan bootstrap, sengaja
  tidak digabung ke 3.2.4): tiap komponen mengekspor `//go:embed migrations/*.sql` sebagai
  `embed.FS`; `pamongctl migrate` menggabungkan FS `modules/` + core + identity; `EnsureSchema`
  mengeksekusi SQL embedded yang sama alih-alih const paralel. `LoadMigrations` sudah menerima
  `fs.FS` jadi migrator tidak perlu berubah, dan DDL yang ada sudah `CREATE ... IF NOT EXISTS`
  sehingga bisa dipakai ulang apa adanya. Keputusan yang perlu diambil saat itu: `EnsureSchema`
  tetap ada (dipakai test integrasi & bootstrap dev, tapi bersumber SQL embedded) atau dihapus
  agar migrator jadi satu-satunya jalur — condong ke opsi pertama karena test integrasi
  workflow/outbox/audit sekarang bergantung padanya.

- **[PR-2.4.5] Validasi bisnis penugasan tenant.** `usecase.AssignEmploymentToTenant`
  punya stub kosong `validateAssignment` (PR-2.2.4). Isi di 2.4.5: tenant tujuan ada &
  aktif (lewat `TenantRegistry`), employment masih aktif, cegah duplikat penugasan.
  Saat ini penugasan (termasuk cross-tenant) hanya dijaga gerbang permission.

- **[Phase 2 / identity, lanjutan PR-2.2.4] Propagasi perubahan nama/identitas.** Belum ada
  use case `UpdatePersonName` + handler sync untuk `identity.person.diperbarui` (update
  `gov.user_profiles`). Diperlukan saat penambahan gelar (D3→S1→S2) atau koreksi nama.
  Pemicu berasal dari kepegawaian (verifikasi ijazah) via command/event; mutasi tetap di
  identity (ber-permission & ter-audit). Non-retroaktif sudah terjamin oleh aturan snapshot
  dokumen (CLAUDE.md, "Aturan pengembangan terkait identity").

- **[ADR — saat desain integrasi modul kepegawaian] Model nama person.** Putuskan apakah
  `id.persons.nama_lengkap` (single VARCHAR) dipecah menjadi `gelar_depan` + `nama` +
  `gelar_belakang` agar render dengan/tanpa gelar dan riwayat penambahan gelar terlacak.
  Ini mengubah skema identity (sensitif) → wajib ADR sebelum diterapkan. **Belum dibuat.**

- **[Pipeline migrasi data / provisioning otomatis] Sentinel SYSTEM actor.** `assigned_by`
  pada `id.tenant_assignments` (juga aktor audit) saat ini `NOT NULL → id.persons`, sehingga
  aksi non-manusia tak punya aktor sah: migrasi legacy bulk-assign, provisioning otomatis,
  dan admin pertama (chicken-and-egg). Keputusan: pakai **sentinel** (baris person `SYSTEM`
  ber-UUID tetap, mis. konstanta `domain.SystemActorID`) — bukan kolom nullable — agar audit
  tetap punya aktor eksplisit & mudah difilter. Seed lewat migration identity baru +
  pakai konstanta saat flow non-manusia dibangun. (Migration 003 append-only, tidak diedit.)

- **[Phase-2.4] Event role sentral.** `usecase.CreateCentralRole` & `AssignCentralRole`
  (PR-2.3.2) belum menerbitkan event — ada marker `// DEFERRED(Phase-2.4)` di keduanya. Saat
  auth flow aktif, terbitkan `identity.central_role.diassign`/`.dicabut` untuk memicu refresh
  klaim token pada login berikutnya & revocation token aktif. Belum ada konsumen sekarang.

- **[Phase-2.4] Event role tenant.** `usecase.CreateTenantRole` & `AssignTenantRole`
  (PR-2.3.3) belum menerbitkan event — marker `// DEFERRED(Phase-2.4)` di keduanya. Sama
  seperti role sentral: saat auth flow aktif, terbitkan event penugasan untuk refresh/revoke
  token. Belum ada konsumen sekarang.

- **[Phase-2.4] Revocation per-person + use case revoke (lanjutan PR-2.4.1).** Denylist jti
  (`id.revoked_tokens`) sudah ada & teruji, tapi hanya bisa mencabut token yang jti-nya
  diketahui. "Cabut semua token person" (mis. saat central role dicabut) butuh epoch
  `tokens_valid_after` per person — token ber-`iat` lebih awal ditolak (additive: kolom/tabel
  + satu cek di codec `Verify`). Plus: bungkus `RevokedTokenStore` dengan use case revoke
  ber-permission + ber-audit (ADR-003) saat ada caller nyata (admin "akhiri sesi" / handler
  event). Lihat ADR-007 "Keputusan tertunda".

- **[Phase-2.4/PR-2.4.x] OTP & proteksi brute-force login.** `LoginCitizen` (PR-2.4.3) hanya
  jalur password (bcrypt); credential OTP-only (`secret_hash` kosong) ditolak. Belum ada: jalur
  OTP no_hp/email (kirim+verifikasi kode) dan rate-limit/lockout terhadap brute-force pada SEMUA
  alur login. Marker `// DEFERRED(Phase-2.4/PR-2.4.x)` di `identity/usecase/login_citizen.go`.
  Lihat REVIEW_BACKLOG A5.

- **[Phase-2.4] Live wiring alur login.** `LoginEmployee`/`SelectTenant`/`LoginCitizen`
  (PR-2.4.3) belum di-wire ke gateway/handler & `main.go` (preseden codec/sync: di-test dulu).
  Saat wiring: rakit dengan `identity/adapter/auth.NewBcryptVerifier`, `port.TokenIssuer` (codec
  dari config), `identity/adapter/db.CentralRoleResolver` (memenuhi `usecase.CentralRoleResolver`),
  dan `TenantRoleResolver` per-tenant (atas `tenantrole.TenantRoleResolver` + `TenantConnManager`
  → memilih DB tenant terpilih). Handler login = driving adapter baru (POST /auth/login,
  /auth/select-tenant, /auth/public/login). Belum ada use case pembuatan credential ber-password
  (pakai `PasswordVerifier.Hash`) — dibutuhkan untuk seed/admin saat handler dibangun.

- **[Phase-2.4] Live wiring token codec.** `identity/adapter/token.JWTCodec` belum di-wire
  ke server (preseden event bus & sync engine: di-test, belum di `main.go`). Saat 2.4.2/2.4.3:
  bangun codec dari `AppConfig.Auth` (secret + `TokenTTL()`) + `RevokedTokenStore` DB, inject
  `port.TokenVerifier` ke gateway auth middleware (verify → populasi `gateway.Context`) dan
  `port.TokenIssuer` ke alur login. Secret production wajib (sudah ditegakkan `config.Validate`);
  untuk dev, set di `config/local.yaml`.

- **[Phase-3.6+] Purge entri revoked kedaluwarsa.** `id.revoked_tokens` punya index `expires_at`;
  entri benar & lazy tanpa job (token mati setelah exp). Job pembersih (hapus `expires_at < now`)
  menyusul saat `core/scheduler` ada — hiasan, bukan kebenaran.

- **[PR-2.3.5] Penegakan scope unit kerja (data-level ABAC). SELESAI.** Ditegakkan di
  `core/permission.ScopedEngine` (Tahap 1 RBAC `Engine.Allows` UTUH + Tahap 2 scope via
  scoped-grant + hierarki OPD). `unit_kerja_id` + `include_subtree` (kolom additive baru)
  pada `gov.user_role_assignments` menentukan jangkauan; `gov.org_units` (adjacency, recursive
  CTE) menjawab subtree. Delegasi/PLT (`gov.delegations`, orang→orang, expiry lazy) = jalur
  grant mandiri. Lihat backlog turunan di bawah.

- **[Phase-3.x] ABAC atribut tahun anggaran/periode.** MVP ABAC (PR-2.3.5) hanya `unit_kerja_id`
  (flat + subtree); `permission.ResourceScope` sengaja struct agar atribut tahun/periode bisa
  ditambah additive. Marker `DEFERRED(Phase-3.x)` di `core/permission/scope.go` & `scoped_engine.go`.
  Scoping tahun fiskal sebagian sudah ditangani `data_lifecycle: annual_cutoff` (schema-per-tahun)
  & `fiscal_periods` — desain ABAC-tahun saat modul keuangan hadir agar tak tumpang-tindih.

- **[Phase-2.4] Wiring Authority live + seam scoped + revoke/event delegasi.** Evaluasi data-level
  PR-2.3.5 terbukti via integration (test berperan sbg middleware). Yang menyusul saat auth flow:
  (a) middleware membangun `permission.Authority` (RoleNames+RoleGrants dari resolver tenant,
  emitter central-role→Grant `TenantWide`, DelegatedGrants dari resolver delegasi) lalu
  `ScopedEngine.Bind` → `gateway.Context.SetScopedEvaluator`, mengaktifkan `RequirePermissionInUnit`
  (kini default permisif bila evaluator nil); (b) use case `RevokeDelegation` + publish event
  delegasi (refresh/revoke klaim token). Emitter central-role→Grant belum dibuat → `identity/`
  TAK disentuh di PR-2.3.5.

- **[Phase-2.4] Sumber non-delegable dari manifest.** `CreateDelegation` menolak permission
  non-delegable dari `domain.NonDelegableSet` yang di-inject (MVP: daftar manual). DEFERRED:
  sumber dari flag `non_delegable` per-permission di manifest modul (lihat `delegation/domain/policy.go`).

- **[Phase-3.6+] Job purge/notifikasi delegasi kedaluwarsa.** Kedaluwarsa delegasi sudah BENAR
  & lazy saat evaluasi (`ListActiveByDelegatee` filter di SQL) — tak bergantung job. Job scheduler
  untuk membersihkan/menotifikasi delegasi yang lewat masa berlaku = hiasan, menyusul saat
  `core/scheduler` ada.

- **[Phase-2.x / infra] Runner migrasi framework-gov formal.** Tabel framework `gov.*` masih
  dibuat lewat EnsureSchema-on-write, bukan migrasi formal: `gov.user_profiles` (PR-2.2.4),
  `gov.tenant_roles` + `gov.tenant_role_permissions` + `gov.user_role_assignments` (PR-2.3.3,
  kolom `include_subtree` PR-2.3.5), dan kini `gov.org_units` + `gov.delegations` (PR-2.3.5, di
  `tenantrole/adapter/db/hierarchy.go` & `delegation/adapter/db/schema.go`). Bangun set migrasi
  framework yang dijalankan per-tenant via `Migrator` + retrofit tabel-tabel ini, sekaligus
  menambah FK referensial yang ditunda: `gov.user_role_assignments.user_id → gov.user_profiles(id)`
  dan `gov.user_role_assignments.unit_kerja_id → gov.org_units(id)` (di-skip pada jalur ensure
  karena tabel ensure-on-write tanpa jaminan urutan pembuatan). Catatan: `gov.org_units` adalah
  placeholder minimal — modul OPD penuh kelak menjadi pemiliknya lewat port `permission.Hierarchy`.

- **[Tooling / linter] Rule `markerref` (penegakan ref penanda).** CODE_CONVENTION §9
  mewajibkan tiap `TODO`/`FIXME`/`DEFERRED` ber-ref (PR/#issue/Phase), tapi belum ada
  penegak otomatis — saat ini hanya review manusia (linter aktif baru `domainnoinfra`; CI
  tak punya grep-gate). Tambah analyzer `tools/linter/rules/markerref` (scan komentar, cek
  format ref) + daftarkan di `registry.go` (pola sama dgn placeholder yang sudah dicatat di
  sana). §9 sudah jadi spesifikasinya. Setelah ada, wording "via review" di §9 jadi "via CI".
