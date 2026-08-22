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
  - Branch protection di `main`
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
  - **FIX (31 Jul 2026, ditemukan saat PR-3.8.2):** `Subscribe` tak mem-Flush, sehingga SUB
    masih di buffer klien saat publish pertama datang — NATS Core membuang pesan tanpa
    subscriber terdaftar dan tak pernah re-deliver. Dampak produksi: event yang di-dispatch
    pada jendela antara Bootstrap dan pencatatan SUB HILANG PERMANEN padahal OutboxRelay sudah
    menandainya terkirim. Gejalanya selama ini terbaca sebagai `TestNATSDriver_MultiSubscriber`
    "flaky" (terbukti gagal 4/10 run). Perbaikan: `Subscribe` blokir sampai server
    mengonfirmasi (Unsubscribe + error bila gagal) + test regresi
    `TestNATSDriver_SubscribeSiapSaatReturn` (20 iterasi publish-langsung-setelah-subscribe;
    diverifikasi gagal 3/5 run tanpa fix, hijau 10/10 dengan fix).

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

- **PR-3.2.5** Guard expression DSL ← 3.2.3, 2.4.2 ✅
  - Evaluator ekspresi boolean, di-compile saat load, tanpa side-effect
  - DoD: guard mengevaluasi konteks actor & entity; syntax error ketahuan saat load ✅
  - SELESAI (`ac65fec`): `core/workflow/guard.go` — compile saat load, boolean-only.

- **PR-3.2.6** SLA, deadline & eskalasi ← 3.2.1, 3.6.1 ✅
  - Batas waktu per state, eskalasi otomatis saat lewat
  - DoD: state lewat SLA memicu eskalasi & notifikasi ✅
  - SELESAI (ADR-011): tiga port baru di `core/workflow` (`DeadlineScheduler`,
    `InstanceStateReader`, `Escalator`) + `Deadline`/`Escalation` + `DeadlineKey` +
    `EscalationCoordinator` (guard race fire-time) di `core/workflow/sla.go`. Engine
    menjadwalkan deadline saat MASUK state ber-SLA (Start + ExecuteWithComment) &
    membatalkan saat KELUAR; opsi fungsional `WithDeadlines` (nil = SLA nonaktif,
    backward-compatible). Engine tetap tenant-agnostik (bawa PERAN generik). Adapter
    di luar core: `infra/workflow.SchedulerDeadlines` (job one-shot atas core/scheduler),
    `EscalationJob` (JobFunc pembungkus coordinator), `NotifierEscalator` (atas
    core/notification.RoleNotifier). Guard race = backstop: deadline basi / cancel luput →
    no-op karena instance sudah pindah. Mock `MockDeadlineScheduler`/`MockInstanceStateReader`/
    `MockEscalator` di testkit; 15 unit test (core/workflow + infra/workflow), build+vet+lint
    clean. **Binding tenant pada peran eskalasi: RESOLVED PR-N2** — lewat `Engine.StartFromTemplate`
    + `ApplyBindings` di tiap `ExecuteWithComment` (bukan di `NotifierEscalator`), lihat PR-N2.

- **PR-3.2.7** Workflow history & instance versioning ← 3.2.3, 1.3.1 ✅
  - Riwayat transisi immutable; instance berjalan pakai versi definisi saat mulai
  - DoD: perubahan definisi tidak mengubah instance yang sedang berjalan ✅
  - SELESAI (`6c49969`): instance versioning + history komentar (`core/workflow/instance.go`).

### Sub-phase 3.3 — Strategy registry & tenant config

- **PR-3.3.1** Strategy registry + key resolution ← 1.1.1 ✅
  - Interface + registry ber-key + `Register()`; tolak key tak terdaftar
  - DoD: dua strategy dummy ter-register; use case memilih via key dari config

- **PR-3.3.2** Tenant config ber-scope + resolver ← 3.3.1, 2.2.1 ✅ (inti)
  - Skema config `tenant[/unit/resource]`, resolusi paling-spesifik-menang
  - DoD: config tenant terbaca; scope unit kerja meng-override tenant
  - SELESAI: `gov.tenant_configs` + `core/config.Resolver` + `infra/config` store +
    `strategy.ConfigSelectionSource` (ganti MemorySelectionSource). Rekonsiliasi template
    selection (versi/effective-date/validasi/permission) SELESAI PENUH di PR-3.3.2b (a-d).

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

- **PR-3.4.1** Custom field & label override ← 1.1.2, 2.2.1 ✅
  - Layer terpisah dari definisi modul; upgrade-safe
  - DoD: tenant menambah field tanpa mengubah modul; upgrade tak menimpa ✅
  - Impl: `core/customization/` — `CustomFieldDef` (bungkus domain.FieldDef, reuse aturan
    tipe) + `DataClass` (default aman `internal`; enkripsi DEFERRED Phase-3.8/ADR-009);
    `CustomFieldStore` (List/Save/Deactivate) + Memory; `MergeEntity` (murni, InsertAfter
    ordering + deferred anchor, base tak dimutasi) + `ValidateAgainstBase` + `EntityLookup`
    (adapter atas domain.Registry, fail-closed entity tak dikenal); label override NUMPANG di
    config ber-scope (key `customization.label.{modul}.{entity}.{field}`, `LabelResolver`) —
    hemat tabel/adapter; `Manager` jalur tulis ber-permission + event (invalidasi cache merge).
    Migrasi `gov.tenant_custom_fields` + `gov.tenant_capability_overrides` (embed) + adapter
    Postgres `infra/customization/`. **Carry-over 3.4.2 tuntas**: persistensi capability +
    write-path ber-permission. Penerapan label/field ke FORM dirender lapis UI — DEFERRED
    (FieldUI belum ada). Unit test penuh + integration (Postgres) lulus.

- **PR-3.4.2** Capability flags per-tenant ← 2.2.1 ✅
  - Gate fitur dormant tanpa percabangan kode menyebar
  - DoD: fitur ber-flag aktif/nonaktif per-tenant tanpa rilis terpisah ✅
  - SELESAI (core-only): `core/customization/capability.go` — `Capability` (Name
    {modul}.{fitur}, DefaultEnabled) DIDEKLARASIKAN di kode (titik ekstensi #6/#1);
    `CapabilityRegistry` (Register nama-unik + List terurut); port `TenantCapabilityStore`
    (Override/Set — hanya override eksplisit; ketiadaan = pakai default) + `MemoryTenantCapabilityStore`;
    `CapabilityResolver.IsEnabled` (override tenant menang → else DefaultEnabled; capability
    tak terdaftar → error FAIL-CLOSED). 8 unit test, coverage 98%. **Adapter DB
    (`gov.tenant_customizations` / tabel flag) + use case admin ber-permission DITUNDA** —
    mekanisme gate sudah lengkap & teruji via Memory store; persistensi + write-path menyusul
    bersama PR-3.4.1 (custom field) yang berbagi tabel/permission kustomisasi.

### Sub-phase 3.5 — Scheduler

- **PR-3.5.1** Cron & job queue ← 0.2.1 ✅
  - Penjadwalan, eksekusi, riwayat job
  - DoD: job terjadwal jalan tepat waktu di test
  - Impl: parser cron 5-field murni (no lib), Registry handler ber-key (titik ekstensi #1),
    Runner (RunDue/Trigger/Replay/Start), JobStore port + MemoryJobStore + Postgres
    (gov.scheduled_jobs + gov.job_runs). One-shot (cron kosong) = seam deadline SLA (F2).
    Anti double-run multi-instance DITUNDA ke 3.5.2 (lock).

- **PR-3.5.2** Distributed lock ← 3.5.1, 3.1.x ✅
  - Job tidak double-run di multi-instance
  - DoD: dua instance, job jalan sekali
  - Impl: Locker port ber-sewa (lease+TTL, token guard), MemoryLocker + Postgres DBLocker
    (gov.job_locks, acquire atomik INSERT..ON CONFLICT + guard kedaluwarsa). Runner.WithLocker
    mengunci per-jadwal + re-check jatuh tempo di bawah lock. Sewa kedaluwarsa → ambil alih
    (anti-deadlock bila instance mati). DoD terbukti: dua Runner konkuren, job jalan sekali.

### Sub-phase 3.6 — Notification & messaging

- **PR-3.6.1** Channel abstraction + template engine ← 3.1.1 ✅
  - Port channel, template per tenant, i18n
  - DoD: kirim notif in-app & email (mock) dengan template benar
  - Impl: `ChannelRegistry` + `EmailChannel`/`InAppChannel`, `TemplateEngine` per-tenant+i18n
    (tenant>global, locale exact>default), delivery tracking, migrasi
    `gov.notification_templates|inapp|deliveries`, adapter Postgres `infra/notification/db_store.go`
    (`DBTemplateStore`/`DBInAppInbox`/`DBDeliveryRecorder` + integration test) (`13d31c0`).

- **PR-3.6.2** Routing by role/jabatan ← 3.6.1, 2.3.x ✅
  - Notif ke role/jabatan, fallback ke PLT
  - DoD: notif ke "Kadis" jatuh ke PLT bila jabatan kosong
  - Impl: `Router` (kebijakan fallback PLT di core) + `RoleNotifier` + port `RecipientDirectory`
    + `MemoryDirectory` (seed/test) + doc kontrak cross-tenant (`8051d2c`). Adapter tenant-DB
    (`DBRecipientDirectory`) menyusul di PR-N1 (lihat bawah) — ActingFor (PLT-jabatan) DEFERRED
    ke modul kepegawaian, lihat backlog "ActingFor PLT-jabatan".

- **PR-N1** Adapter tenant-DB untuk `RecipientDirectory` ← 3.6.2 ✅
  - `DBRecipientDirectory` (`infra/notification/directory.go`): baca pemegang role tenant nyata
    dari `gov.tenant_roles`/`gov.user_role_assignments` (menggantikan `MemoryDirectory` di
    produksi). In-app jalan end-to-end lewat DB.
  - Scope unit kerja + subtree (reuse `tenantrole/adapter/db.OrgUnitHierarchy.IsWithin`,
    disaring di Go), assignment kedaluwarsa diabaikan, `is_cross_tenant` SENGAJA TIDAK
    difilter (PJ/Plt luar-daerah jatuh ke HoldersOf, bukan ActingFor).
  - `ActingFor` mengembalikan kosong (nil, nil) — PLT-jabatan DEFERRED, lihat backlog
    "ActingFor PLT-jabatan". DoD: integration test lulus (`-p 1`), lint/vet/gofmt bersih.
  - N2 (bridge workflow→notifikasi) & N3 (contact seam + email/SMS real) menyusul, TIDAK
    bergantung phase berikutnya — lihat memory `plan-notification-completion`.

- **PR-N2** Bridge workflow→notification (Escalator adapter + seam Notify transisi) ← 3.2.6, N1 ✅
  - Menutup backlog "[PR-3.6.x] Konsumsi role binding saat notifikasi/eskalasi" (lihat di
    bawah) — workflow yang sudah ada kini BENAR-BENAR memicu notifikasi (sebelumnya dormant).
  - `Engine.StartFromTemplate` (`core/workflow/engine.go`, opsi `WithTemplates`) menggantikan
    jalur `Start(def.ID)` mentah untuk instance ber-template: mengambil `TemplateStore.GetTenantConfig`
    lalu `ApplyBindings` SEKALI, membekukan hasilnya ke `WorkflowInstance.RoleBindings` (field
    baru). `ExecuteWithComment` menerapkan ulang `ApplyBindings(def, instance.RoleBindings)` pada
    SETIAP transisi (bukan cuma initial_state) sebelum membaca state/transition — jadi
    `Escalation.EscalateToRole` & `NotifySpec.ToRole` konsisten role KONKRET tenant sepanjang
    hidup instance, kebal terhadap tenant merekonfigurasi `RoleBindings` di tengah jalan
    (paralel dengan `DefinitionVersion` yang juga dikunci saat Start).
  - Seam baru `TransitionNotifier` (`core/workflow/notify.go`, opsi `WithNotifier`, pola sama
    `WithDeadlines` — nil = no-op) dipicu SETELAH transisi otoritatif & SLA state baru
    terjadwal. Kegagalan notifikasi TIDAK membatalkan transisi (state sudah berubah) tapi TETAP
    dipropagasi sebagai error (best-effort, caller async bisa retry) — didokumentasikan di
    `notify.go`.
  - Adapter `infra/workflow.NotifierTransition` (mirror `NotifierEscalator` yang sudah ada sejak
    PR-3.2.6) memetakan `NotifySpec`→`RoleTarget` lalu panggil `RoleNotifier.NotifyRole`.
    `NotifierEscalator` TIDAK berubah — comment DEFERRED lama dihapus karena binding kini
    diterapkan di Engine, bukan di adapter.
  - Bagian C (backlog keamanan "Prasyarat keamanan saat binding mulai dikonsumsi"): seam
    `RoleChecker` (`core/workflow/template_choice.go`) + adapter `infra/workflow.TenantRoleChecker`
    (atas `gov.tenant_roles`) — `TemplateChoiceManager.SetChoice` kini menolak `RoleBindings`
    yang menunjuk role tak terdaftar di tenant SAAT TULIS (`NewTemplateChoiceManager` nambah
    parameter `RoleChecker`).
  - Mock baru di testkit: `MockTransitionNotifier`, `MockRoleChecker`.
  - DoD terbukti: unit test `core/workflow` (binding konsisten di Notify+SLA, notifier
    best-effort, RoleChecker menolak binding tak dikenal) + integration test
    `infra/workflow.TestN2Bridge_TemplateBerbinding_NotifyDanSLA_SampaiInbox` (instance dari
    template ter-binding → transisi ber-Notify sampai inbox holder role konkret → SLA lewat →
    eskalasi sampai inbox holder role konkret lain), semua `-p 1`, lint/vet/gofmt bersih.

- **PR-N3a** Driver messaging pluggable (log + smtp) ← N1 ✅
  - `infra/messaging` mengimplementasi `port.MessagingPort` dengan registry driver (pola sama
    `infra/storage`): `NewFromConfig` switch `log|smtp`. `config.MessagingConfig` +
    `GOV_MESSAGING_*`.
  - `log`: driver dev/test (catat ke slog, selalu sukses, nol dependency) — DILARANG di
    production via `config.Validate` (body OTP bocor ke log, fail-fast). `smtp`: email nyata
    stdlib `net/smtp` (subject RFC 2047); SMS tak didukung driver ini (`PERMANENT`) — provider
    SMS = driver onboarding terpisah.
  - Membuat jalur transport OTP citizen (2.4.4) nyata (tinggal `driver=smtp`). DoD: unit test
    (factory, log, komposisi/mapping-error SMTP), lint/vet/gofmt bersih.

- **PR-N3b** Contact seam — kontak di clone tenant (routing email/SMS) ← N1 ✅
  - Opsi A (ADR-013): kolom `email`/`no_hp` ditambah ke `gov.user_profiles`, mengalir lewat event
    fat `identity.employment.ditugaskan` (payload + publisher + writer + engine). `DBRecipientDirectory.
    fillContacts` mengisi `Recipient.Email/Phone` best-effort (dijaga keberadaan kedua kolom via
    `information_schema`; error kontak dicatat & di-swallow di HoldersOf → jalur in-app tak ikut gagal);
    kontak kosong/NULL → channel email/SMS gagal anggun `INVALID_RECIPIENT`.
  - Freshness (update-on-change kontak) DEFERRED — identik dengan gap `nama_lengkap` (butuh handler
    `identity.person.diperbarui` lintas-tenant yang belum ada); menumpang solusi clone-freshness umum.
  - SENSITIF (menyentuh identity/sync) + PII kontak (personal_id, ADR-009) menyebar ke tenant DB —
    konsekuensi sadar opsi A; enkripsi field DEFERRED (ROADMAP 3.8). Skema clone dipastikan sekali
    per tenant/proses (bukan DDL tiap write). DoD: integration test lulus (`-p 1`, clone bawa kontak;
    directory isi Recipient.Email/Phone; holder tanpa profil → kosong), lint/vet/gofmt bersih.

### Sub-phase 3.7 — Storage & metrics ports

- **PR-3.7.1** Storage port + MinIO/S3 adapter ← 0.2.1 ✅
  - Upload/download/delete, metadata
  - DoD: integration test simpan & ambil file dari MinIO ✅
  - SELESAI (`5e8ec08`): `infra/storage` driver minio/s3 + local.

- **PR-3.7.2** Metrics port + Prometheus/OTEL adapter ← 0.2.2 ✅
  - Counter, gauge, histogram; tracing OTEL
  - DoD: metric tereskpos di endpoint; trace muncul di collector ✅
  - SELESAI (`eae322f`, fix `c61d7c5`): `infra/observability` MetricsPort Prometheus + tracing OTEL.

### Sub-phase 3.8 — Klasifikasi data & enkripsi field (ADR-009/010)

Tujuan: enkripsi field selektif (pengenal + data spesifik) at-rest dengan blind index,
tanpa mematikan lookup/UNIQUE. WAJIB selesai sebelum tenant produksi pertama — biaya
migrasi naik seiring data & entity. Lihat ADR-009 (klasifikasi/enkripsi) & ADR-010 (KMS
pluggable + custody sebagai kebijakan per-tenant).

- **PR-3.8.1** `DataClass` di `FieldDef` + validasi + DDL multi-kolom ← 1.1.x (core/domain) ✅
  - Tambah `Class`/`Searchable`; `Validate()` tolak kombinasi mustahil; `columnDef` → N kolom
    (`_enc`+`_bidx`) untuk field terenkripsi. **Murah sekarang** (satu-satunya konsumen
    EntityDef produksi = surat_masuk, tanpa pengenal).
  - DoD: entity dengan field `personal_id` meng-generate dua kolom; validasi menolak
    `Unique`+terenkripsi+`!Searchable`; entity lama tetap kompilasi & lulus test. ✅
  - SELESAI (`fdb1a78`): `DataClass` kanonik di `core/domain` (customization di-alias); UNIQUE
    di `_bidx`; `EntityDef.Searchable` tolak field terenkripsi. Nol runtime kripto.

- **PR-3.8.2** `port/crypto.go` + `infra/crypto` (AES-256-GCM + blind index) ← 3.8.1 ✅
  - CryptoPort (Encrypt/Decrypt/BlindIndex); KeyProvider registry + envelope; DEK store
    `id.data_keys`; driver `static` (KMS-alike bawaan, master KEK ber-versi — **default
    produksi Tier 1/2**) + `local` (dev/test); format ciphertext self-describing. KMS eksternal
    (vault/aws-kms/bssn) di-plug kelak tanpa ubah kode.
  - DoD: roundtrip; ciphertext beda tiap panggilan; blind index deterministik; isolasi per
    tenant; `static` menolak start tanpa master key valid + rotasi V1→V2 jalan. ✅
  - SELESAI: `port/crypto.go` (+`ErrCiphertextInvalid`) & `infra/crypto` (crypto.go/provider.go/
    kek.go/drivers.go/dek_store.go/envelope.go/custody.go) + `testkit.MockCrypto`.
    Ciphertext `v1|purpose|key_version|nonce|ct+tag`, AAD mengikat (tenant, purpose, versi) →
    ciphertext tak bisa dipindah antar tenant. DEK per **(tenant, purpose, kind)** —
    `kind` enc vs bidx SENGAJA kunci terpisah, bukan turunan satu DEK (kalau diturunkan,
    rotasi kunci enkripsi ikut memaksa reindex bidx yang mahal). Migrasi identity 007
    (`id.data_keys`, unique index parsial → tepat satu versi aktif) + 008 (kolom
    `key_custody`). Kunci dibuat otomatis saat purpose dipakai pertama kali; balapan insert
    diuji (8 goroutine → satu kunci). Custody dibaca per-tenant dari registry;
    `key_custody='tenant'` DITOLAK LANTANG (`ErrCustodyUnsupported`) — tanpa fallback ke
    platform; unwrap memakai provider sesuai kolom `custody` baris itu sehingga data lama
    tetap terbaca bila custody berpindah. Driver `local` ditolak di luar development oleh dua
    gerbang (`config.Validate` + `NewFromConfig`). **BELUM di-wire ke repository (itu 3.8.3)
    → nol perubahan perilaku.** Config `GOV_CRYPTO_*` (default dev: `kms_driver: local`).

- **PR-3.8.3** Enkripsi transparan di lapis repository ← 3.8.2, 1.2.1
  - infra/db enkripsi saat tulis + blind index + dekripsi saat baca; equality/UNIQUE → `_bidx`.
    Otomatis dari `FieldDef.Class`, bukan use case.
  - DoD: CRUD entity ber-`personal_id` bekerja; kolom `_enc` di DB bukan plaintext; lookup jalan.
  - **WAJIB (warisan 3.8.2):** sebelum `Decrypt`, repo memeriksa `crypto.PurposeOf(ct)` sama
    dengan purpose kolom yang dibaca, lalu menolak bila berbeda. AAD hanya mengikat TENANT —
    purpose & versi dibaca dari blob itu sendiri (konsekuensi format self-describing), jadi
    tanpa pemeriksaan ini ciphertext bisa dipindah antar kolom DALAM SATU tenant (mis. `nik`
    disalin ke `no_rekening_enc`) dan tetap terbuka. Pengikatan kolom hanya bisa ditegakkan
    di lapis yang tahu kolomnya — yaitu di sini.
  - Celah desain yang harus diputuskan lebih dulu: ~~`SQLRepository` generik bekerja atas
    `Mapper[T]` (kolom = string) dan TAK tahu `FieldDef.Class`~~ → **DIPUTUSKAN**: spec
    diturunkan dari `EntityDef` (`db.FieldCryptoFromEntity`) lalu diserahkan ke repo lewat
    opsi `db.WithCrypto`/`WithFieldCrypto`. `Mapper[T]` TIDAK berubah (menambah method memaksa
    semua mapper existing ikut berubah, dan menaruh kebijakan keamanan di kode tulis-tangan
    Tier 3 = tempat paling mudah lupa); repo juga tidak membaca registry sendiri (kopling).
    Rekonsiliasi `CustomFieldDef.Class` MASIH terbuka.

- **PR-3.8.4** Enkripsi diff audit sensitif (tutup E2) ← 3.8.2, core/audit
  - Diff class `personal_id`/`specific` terenkripsi; raw tetap bukti; read-gated
    `audit:sensitive:baca`. Hash chain tetap verify.
  - DoD: dump `gov.audit_logs.diff` tak memuat NIK plaintext; verify integritas lulus.

> **3.8.3 & 3.8.4 DIKERJAKAN SEBAGAI SATU PR.** Alasannya bukan kepraktisan: `auditedRepo`
> mengambil snapshot diff dari ENTITY (nilai plaintext), bukan dari kolom DB — sehingga 3.8.3
> yang mendarat sendirian akan mengenkripsi kolom sambil tetap menulis NIK plaintext ke
> `gov.audit_logs.diff`. Itu memindahkan kebocoran, bukan menutupnya, sekaligus menciptakan
> keyakinan keliru bahwa data sudah terlindungi.
>
> **Status: ✅ SELESAI** di branch `feat/crypto-repo-transparent-3.8.3` (checkpoint `e22506c`
> + penyelesaian sisa pekerjaan). BELUM direview & BELUM di-merge.
>
> Yang berdiri sekarang: `infra/db/field_crypto.go` (spec dari EntityDef, kolom
> logis→`_enc`/`_bidx`, `decryptingScanner` menyisip sebelum `Mapper.Scan` sehingga Mapper
> tetap polos), filter equality→`_bidx`, sort & search-column terenkripsi ditolak, tenant
> kosong gagal keras, `NewRepository` menolak entity terenkripsi tanpa `CryptoPort`, diff
> audit menyimpan ciphertext base64, dan `core/audit.Reader` menggerbangi pembukaannya.
>
> **Sisa pekerjaan yang dicatat sebelumnya — semuanya tuntas:**
>
> 1. ✅ **Integration test Postgres nyata** (`infra/db/field_crypto_integration_test.go`,
>    7 test). Tabel dibangun dari `GenerateMigration` — yang diuji SAMBUNGAN generator DDL ↔
>    repo, bukan repo sendirian. Terbukti: kolom plaintext `nik`/`no_rekening` tidak ada di
>    katalog; `SELECT nik_enc` bukan plaintext; `List` ber-filter `nik` menemukan baris lewat
>    `_bidx`; UNIQUE di `nik_bidx` menolak duplikat (23505) sementara `no_rekening` non-Unique
>    tetap menerima; ciphertext yang dipindah antar kolom lewat SQL ditolak; dump
>    `gov.audit_logs.diff` bersih dari NIK & hash chain verify. Tiga mutasi kode produksi
>    (matikan enkripsi diff / matikan cek purpose / simpan plaintext) diverifikasi MEMBUAT
>    test gagal — hijau di sini bukan hijau kosong.
> 2. ✅ **ADR-015** (`docs/adr/015-pengikatan-kolom-ciphertext-purposeof.md`) — memperluas
>    ADR-009 §4 dengan metode keempat `PurposeOf`, berikut tiga alternatif yang ditolak
>    (AAD ber-kolom, parsing header di repo, mengandalkan hak akses DB). ADR-009 diberi
>    pointer di Status + §4; tidak di-supersede (pola yang sama dipakai ADR-002→ADR-009).
> 3. ✅ **Read-gating** — `core/audit/reader.go` + `permissions.go`: `Reader` membuka nilai
>    terenkripsi hanya untuk pemegang `audit:sensitive:baca`, mengenali nilai sensitif dari
>    BENTUKNYA (`PurposeOf`) bukan dari class yang dibaca ulang, `tenant_id` selalu dari
>    AuthContext, dan `VisibleEntry` tak membawa Hash/PrevHash agar chain tak pernah
>    diverifikasi atas nilai hasil baca.
> 4. ✅ **Dokumentasi** — `infra/db/CLAUDE.md` (§Enkripsi field transparan + pitfall),
>    `core/audit/CLAUDE.md`, `docs/contracts/permissions.md` (§audit).
>
> **Perbaikan hasil `/code-review` (31 Jul 2026, sebelum merge):** penyegelan diff audit
> semula mengenkripsi sisi before & after sendiri-sendiri. Karena nonce acak, `audit.Diff`
> (`reflect.DeepEqual`) melaporkan SETIAP kolom terenkripsi sebagai berubah pada SETIAP
> update — jejak mengarang "NIK berubah A→A" dan supresi no-op update ikut mati; di jalur
> gagal, penanda kegagalan yang sama di kedua sisi justru MENGHAPUS perubahan nyata (bila
> ia satu-satunya yang berubah, baris ter-commit tanpa entry audit sama sekali). Diperbaiki
> dengan membandingkan plaintext SEBELUM menyegel (`auditedRepo.sealPair`): nilai tak
> berubah disegel sekali untuk kedua sisi, nilai berubah disegel per sisi dengan penanda
> gagal yang berbeda. Ditutup 8 unit test (`infra/db/audit_diff_crypto_test.go`) +
> `TestFieldCrypto_AuditDiffHanyaFieldYangBerubah`, ketiganya diverifikasi lewat mutasi.
>
> **Yang TIDAK ikut tertutup di sini — jangan dibaca sebagai "PII di audit sudah beres":**
> jalur audit **identity** masih menulis NIK mentah (`identity/adapter/db/audited_repos.go`
> `personFields`, kolom `id.audit_logs.diff`). Yang tertutup adalah jalur repository entity
> tenant (`gov.audit_logs`). Menutup jalur identity butuh `CryptoPort` di-wire ke identity +
> keputusan tenant kunci untuk chain sentral (`tenant_id="central"`) — lihat REVIEW_BACKLOG E2.
>
> Berikutnya: `/code-review` + `/security-review` sebelum merge (HYBRID; permukaan kripto).

- **PR-3.8.5** Tutup jalur kebocoran samping ← 3.8.3 (ADR-009 §6) — **SEBAGIAN**
  - Payload event, `gov.idempotency_keys`, staging table migrasi, log/trace, clone
    `gov.user_profiles`.
  - DoD: tiap jalur tak membocorkan pengenal mentah (test per-jalur).
  - **3.8.5a — clone `gov.user_profiles` ✅ SELESAI.** Dikerjakan lebih dulu karena ia jalur
    samping yang paling luas jangkauannya: NIK/NIP/email/no_hp tersalin ke SETIAP tenant DB tempat
    person ditugaskan, sehingga pengenal yang terlindungi di identity DB tetap terbaca di dump
    tenant mana pun. Yang berdiri: kolom plaintext DI-DROP dari clone → `_enc`+`_bidx` dengan
    **realm TENANT** (bukan `_central` — clone hidup di DB tenant, dan realm sentral di sana berarti
    satu kunci membuka clone seluruh pemda); `ResolveByNIK`/`ResolveByNIP` pindah ke blind index;
    kontak notifikasi (ADR-013) dibuka lewat sealer, tetap best-effort. Kebijakan seal/index/open
    diekstrak ke `crypto.FieldSealer` — satu implementasi dipakai identity DB & clone, sebab empat
    salinan aturan kripto akan menyimpang diam-diam. Ikut tertutup: `ErrNotFound` di `infra/user`
    dulu mengutip NIK/NIP yang dicari (jalur samping ADR-009 §6 yang sama).
    - DoD terbukti di DB nyata: kolom plaintext hilang dari katalog; baris clone dibaca sebagai
      teks **beserta bentuk hex bytea-nya** bersih dari keempat pengenal sementara `nama_lengkap`
      (kelas `personal`, sengaja plaintext) tetap ada sebagai kontrol negatif; resolve by NIK/NIP
      utuh lewat `_bidx`; realm tenant lain gagal me-lookup DAN gagal membuka; kunci yang terbentuk
      di `id.data_keys` ber-realm tenant, bukan `_central`; kontak yang ciphertext-nya ditukar antar
      baris lewat SQL ditolak (ADR-016) alih-alih terkirim ke alamat orang lain.
    - **Tujuh mutasi kode produksi diverifikasi**; dua di antaranya LOLOS pada percobaan pertama dan
      itu temuan tentang test-nya, bukan tentang kodenya: (a) NIK yang disimpan mentah ke kolom
      `bytea` tak terlihat pada `row::text` karena Postgres merendernya sebagai hex — test kini
      memeriksa kedua bentuk; (b) membuka ciphertext dengan id dari PERMINTAAN alih-alih dari BARIS
      selalu benar selama hanya ada satu baris berkontak — test kini menyeed dua.
  - **3.8.5b — payload event + cache idempotency ✅ SELESAI.** Dua jalur terakhir yang masih
    menyimpan pengenal plaintext, ditutup dengan DUA mekanisme berbeda karena sifatnya berbeda —
    keputusan itu yang jadi isi **ADR-018**:
    - **Payload event: nilainya DIHAPUS, bukan disegel.** `PersonDibuatPayload.NIK`,
      `EmploymentDibuatPayload.NIP`, dan `EmploymentDitugaskanPayload.{NIK,NIP,Email,NoHP}`
      dibuang; `identity/sync` memintanya lewat port baru `CloneSource` (impl di atas repo
      identity, jadi kunci realm sentral tak pernah keluar dari sisi identity). Menyegel akan
      menaruh ciphertext di stream NATS retensi & `gov.outbox_events.payload` — kewajiban
      dekripsi permanen yang melintasi rotasi kunci dan patahan format (`0x01` sudah ditolak
      `Decrypt` sejak ADR-016). Membalik doktrin "fat event" **untuk pengenal saja**;
      `NamaLengkap` tetap ikut payload (kelas `personal`, sengaja tak dienkripsi di kolom).
      Keputusan #1 ADR-013 tidak berubah — kontak tetap mendarat di clone, yang berganti hanya
      kurirnya; opsi yang dulu ditolak ADR-013 adalah baca live saat KIRIM di sisi tenant.
    - **`gov.idempotency_keys.response`: disegel** (di sini tak ada yang bisa dihapus — badan
      respons itu memang datanya). Realm tenant, purpose `idempotency_response`, **tanpa blind
      index** (`FieldSealer.SealOpaque` — bidx atas nilai yang tak pernah dicari hanya oracle
      kesamaan antar respons). Koordinat AAD diturunkan dari KEDUA bagian PK, sebab dari
      `person_id` saja respons bisa dipindah antar key milik orang yang sama dan tetap terbuka.
    - DoD terbukti: payload diperiksa pada bentuk **hasil marshal JSON** (bukan struct Go —
      `MarshalJSON` yang menyertakan field kembali akan lolos type check), dengan nilai dicari
      sebagai substring sehingga kebocoran lewat field bernama lain ikut tertangkap; kolom
      `response` bersih dari badan respons dalam bentuk teks **dan** hex, sementara replay tetap
      memulihkannya utuh; ciphertext yang dipindah antar key ditolak.
    - **Enam mutasi kode produksi diverifikasi** (koordinat salah ke `CloneSource`, error source
      ditelan, pengenal bocor lewat field payload bernama lain, tak-ditemukan dikembalikan
      kosong, respons disimpan plaintext, koordinat AAD hanya `person_id`).
  - **Belum ditutup (sisa 3.8.5):** staging table migrasi — pipeline legacy-import belum ada sama
    sekali, jadi tak ada yang bisa ditutup sekarang; aturannya harus mendarat bersama pipeline-nya.
    (Log/trace sudah disisir tuntas di 3.8.5a: satu-satunya pelog alamat/body adalah driver
    `infra/messaging/log.go` yang memang ditolak di staging & production.)

- **PR-3.8.6** Migrasi identity UNIQUE→blind index ← 3.8.3 ✅ *(dikerjakan bersama E2)*
  - `nik`/`nip`/`cred_value`/`no_hp`/`email` → `_enc`+`_bidx`; UNIQUE pindah; backfill (dev
    kosong = gratis). SENSITIF (identity) — review ekstra.
  - DoD: login & resolve by NIK/NIP/email tetap jalan lewat blind index; UNIQUE ditegakkan di `_bidx`. ✅
  - **Keputusan yang mendahului kode: kunci mana untuk data sentral.** Hierarki DEK ADR-010
    ber-tenant, sedangkan identity tak punya tenant — dan yang mengunci pilihan bukan estetika
    melainkan `UNIQUE(nik)` yang berlaku **global se-identity-DB**: kunci bidx per-tenant
    membuat NIK yang sama menghasilkan bidx berbeda, sehingga UNIQUE berhenti menangkap
    duplikat dan `FindByNIK` harus menyebut tenant yang ia tak punya. → **ADR-017**: sumbu
    partisi `id.data_keys.tenant_id` dibaca ulang sebagai **key realm**, dengan token cadangan
    `_central` yang **tak bisa dipalsukan** (gagal `^[a-z]` di `tenantIDRe`, jadi mustahil jadi
    tenant_id). Custody realm sentral = `platform` sebagai **invarian kode**
    (`crypto.WithCentralRealm`), bukan baris `id.tenant_registry` yang bisa di-`UPDATE`.
    Skema `id.data_keys` & kontrak `port.CryptoPort` TIDAK berubah.
  - **Backfill diverifikasi benar-benar nol** sebelum menulis kode: tak ada manifest deploy di
    repo, remote hanya Gitea lokal, dev & CI tanpa schema `id` sama sekali. Window ini tidak
    terulang.
  - Yang berdiri: migrasi `009_encrypt_identity_identifiers` (kolom plaintext **DI-DROP**, bukan
    dibiarkan berdampingan; UNIQUE pindah ke `_bidx`; CHECK `nip`↔`status` ikut pindah & kini
    menuntut kedua kolom konsisten); `identity/adapter/db/field_crypto.go` (seal/open/index,
    pemeriksaan `PurposeOf` ADR-015, AAD ber-baris ADR-016 dari `id` BARIS ITU SENDIRI);
    ketiga repo identity menolak `CryptoPort` nil saat konstruksi.
  - **Cacat laten yang ikut tertutup:** sentinel chain audit identity dulu bernilai `"central"`,
    string yang **LOLOS** `tenantIDRe` — pemda yang kebetulan didaftarkan dengan nama itu akan
    melebur audit-nya ke chain sentral, dan sejak PR ini juga berbagi ruang kunci. Kini ia
    memakai `crypto.RealmCentral` yang sama dengan realm kunci, jadi keduanya tak bisa menyimpang.
  - **Perubahan semantik auth yang disengaja:** purpose kunci kredensial diturunkan dari
    `cred_type`, bukan satu purpose gabungan — sehingga kredensial email masuk tabel normalisasi
    framework. Akibatnya **login lewat email menjadi case-insensitive** dan UNIQUE menangkap
    `Budi@x.id` vs `budi@x.id` sebagai duplikat. Gratis sekarang, mahal sesudah ada akun.
    `oauth` sengaja tidak ikut di-fold (subject provider opaque).
  - **E2 (REVIEW_BACKLOG) ikut tertutup di PR yang sama, dan itu bukan kepraktisan:**
    `personFields()`/`employmentFields()` mengambil snapshot dari ENTITY (plaintext), jadi
    mengenkripsi kolom `id.persons` sendirian hanya **memindahkan** kebocoran ke
    `id.audit_logs.diff` — sekaligus menciptakan keyakinan keliru bahwa NIK sudah terlindungi.
    Penyegelannya memakai mesin yang sama dengan jalur tenant (`infra/db.SealAuditDiff`,
    diekspor dari `auditedRepo.sealPair`) agar aturan "bandingkan plaintext dulu, segel
    sesudahnya, penanda gagal per sisi berbeda" tak ditulis dua kali.
  - DoD terbukti di DB nyata (`identity/adapter/db/field_crypto_integration_test.go`, 8 test):
    kolom plaintext hilang dari katalog; dump `_enc`/`_bidx` bersih dari NIK/NIP/email;
    resolve by NIK/NIP/email utuh lewat `_bidx`; UNIQUE menolak duplikat NIK/NIP/kredensial
    sementara nilai sama pada `cred_type` berbeda tetap diterima; ciphertext yang dipindah
    antar BARIS & antar KOLOM ditolak; DEK realm sentral terbentuk **tanpa satu baris pun** di
    `id.tenant_registry`; dump `id.audit_logs.diff` bersih dari pengenal sementara nilai
    non-sensitif tetap ada dan hash chain tetap verify.
  - **Sembilan mutasi kode produksi diverifikasi MEMBUAT test gagal:** (1) blind index ikut
    ber-baris; (2) buang pemeriksaan `PurposeOf`; (3) `RecordID` konstan (pengikatan baris mati);
    (4) `audited_repos` berhenti menyegel diff; (5) buang `WithCentralRealm` dari `NewFromConfig`;
    (6) `RealmCentral` kembali ke `"central"`; (7) purpose kredensial digabung jadi `cred_value`;
    (8) migrasi tak men-DROP kolom `nik`; (9) konstruktor menerima `CryptoPort` nil.
  - **Temuan review yang diperbaiki di PR yang sama** (`/code-review` + `/security-review`):
    - **Kuota OTP lumpuh oleh normalisasi blind index** (HIGH — REVIEW_BACKLOG A7). Lookup kini
      trim + case-fold, jadi `budi@x.id`/`Budi@x.id`/`" budi@x.id"` adalah SATU kredensial dengan
      tiga bucket rate limiter — kuota bisa dilipatgandakan tanpa batas, menyisakan hanya cap
      per-OTP yang memang dirancang sebagai setengah proteksi. Kuota dipecah dua lapis: lapis
      mentah pra-lookup (menjaga enumeration-resistance) + lapis **ID kredensial** pasca-lookup
      (kuota sebenarnya, kanonik by construction), dengan habisnya lapis kedua dibuat tak bisa
      dibedakan dari jalur normal agar tak jadi orakel keberadaan akun. Empat test baru, lima
      mutasi diverifikasi — termasuk "perbaikan" yang salah (key lapis-2 kembali ke nilai mentah).
    - **NIK/NIP masih dikutip pesan `ErrNotFound`** — jalur samping ADR-009 §6 yang sama yang
      ditutup PR ini di tempat lain (pesan mengalir ke log & body HTTP). Referensi error kini
      menyebut jenis pencarian, bukan nilainya.
    - **OTP dikirim ke nilai permintaan, bukan alamat terdaftar** (HIGH — REVIEW_BACKLOG A7, akar
      yang sama dengan temuan pertama, ditemukan putaran review kedua). `normalize()` ber-`TrimSpace`
      ikut membuang CR/LF, jadi `"victim@x.id\n"` me-resolve ke kredensial nyata → OTP dibuat, lalu
      SMTP menolak alamatnya sebagai header injection (500) sementara alamat asing menjawab 200:
      orakel keberadaan akun satu-probe-per-target, yang sekalian menimpa OTP korban yang sedang
      berjalan. Tujuan kirim kini `cred.CredValue` (hasil dekripsi kolom `_enc`). Ini regresi yang
      PR ini perkenalkan — sebelumnya lookup eksak membuat kedua nilai selalu identik.
    - **Tak ada yang mengkanonikalisasi nilai yang MASUK** (HIGH — akar yang sama, putaran review
      ketiga). Dua temuan di atas menutup jalur BACA; tapi `seal()` mengenkripsi verbatim sementara
      `index()` menormalkan, dan `Credential.Validate()` hanya menolak nilai kosong — jadi kredensial
      ber-CRLF bisa TERSIMPAN, dan sesudah itu alamat kanonik hasil dekripsi sendiri yang ditolak
      SMTP. Orakel yang sama, pindah ke jalur tulis dan bertahan permanen di baris DB.
      **Diperbaiki di DOMAIN, bukan di `seal()`**: `bentukPengenalRusak` (control character + spasi
      tepi) di `Credential.Validate` (`cred_value`) dan `Person.Validate` (`email`/`no_hp` — keduanya
      ikut ke clone lalu jadi alamat kirim notifikasi). Menormalkan di `seal()` ditolak: nilai
      terdekripsi jadi ≠ nilai yang didaftarkan, dan kebijakan `infra/crypto` tersalin ke lapis repo.
      Tiga mutasi diverifikasi.
    - **Penggantian sentinel chain audit `"central"`→`_central` menumpang pada premis "DB kosong"
      yang ditulis untuk hal lain** (putaran ketiga). Faktual aman (nol baris di seluruh env), tapi
      premisnya ditulis untuk backfill pengenal & down-migration — sentinel adalah pemakaian ketiga,
      tanpa DDL yang menandainya. Kode tak berubah; DB_CHANGELOG 009 kini menyatakannya eksplisit
      plus urutan yang dituntut DB berisi (repartisi chain dulu, kalau tidak verifikasi chain
      melaporkan diskontinuitas — gejala yang justru berarti "audit log dirusak").
    - **Entri audit identity belum punya jalur baca ber-permission** — `_central` by construction
      tak akan pernah cocok dengan tenant aktor di `audit.Reader`. **Tidak ditambal**: pertanyaannya
      "siapa berwenang membaca audit sentral" (kemungkinan central role platform), dan laten sampai
      `audit.NewReader` dirakit di Phase 5.x. Dicatat di REVIEW_BACKLOG E1.
  - **Yang TIDAK ikut tertutup di sini:** clone `gov.user_profiles` masih membawa pengenal plaintext
    lewat fat event — **`nik`/`nip`, bukan hanya email/no_hp** (PR-N3b/ADR-013). Jadi NIK terenkripsi
    di identity DB & `id.audit_logs`, tapi tersalin apa adanya ke setiap tenant DB yang punya
    penugasan person itu; jalur samping, cakupan **PR-3.8.5**. Dicatat pula dua temuan review yang
    sengaja tidak ditambal di sini: kanal OTP `no_hp` tanpa driver SMS (REVIEW_BACKLOG A8 —
    pra-existing, butuh keputusan konfigurasi) dan `CryptoPort` typed-nil yang lolos konstruktor
    (H6 — gagalnya berisik, bukan plaintext diam-diam).

- **PR-3.8.7** Generator `docs/contracts/data-inventory.md` (pamongctl) ← 3.8.1
  - Inventaris field ber-`Class` dari manifest — artefak kepatuhan UU PDP yang tak basi.
  - DoD: `pamongctl` regenerate; diff PR menampilkan perubahan klasifikasi.

- **PR-3.8.9** Pengikatan BARIS ke AAD ← 3.8.3 (sisa risiko ADR-015) ✅
  - ADR-015 menutup perpindahan ciphertext antar KOLOM lewat `PurposeOf`. Antar BARIS masih
    lolos: menukar `nik_enc`+`nik_bidx` dua pegawai dalam tenant yang sama mendekripsi bersih,
    dan NIK seseorang terbaca sebagai milik orang lain. Sebabnya sama — AAD hanya mengikat
    tenant.
  - Menyentuh kontrak `Encrypt`/`Decrypt` (identitas baris masuk AAD) → merambat ke seluruh
    driver KMS, `testkit.MockCrypto`, `decryptingScanner`, dan jalur baca audit (`Reader`
    harus membawa `EntityID`). Butuh ADR sendiri; bukan tambalan di lapis repo.
  - **Penjadwalannya bukan selera:** retrofit murah hanya selama DB masih kosong. Sesudah ada
    data produksi ia berarti re-enkripsi seluruh baris berpengenal di semua tenant.
  - DoD: menukar pasangan `_enc`/`_bidx` antar baris lewat SQL langsung membuat pembacaan
    GAGAL (test integrasi, bukan unit); blind index tetap row-independent agar pencarian &
    UNIQUE tak rusak. ✅
  - **SELESAI (`4f5ee87`, ADR-016).** Dikerjakan MENDAHULUI 3.8.5/3.8.6 secara sadar: ia mengubah
    kontrak port, sedangkan kedua PR itu justru MENAMBAH pemanggil kontrak tersebut
    (identity, payload event, idempotency). Saat mendarat, pemanggil produksinya tepat empat.
  - Yang berdiri: `port.FieldRef`/`port.RowRef` menggantikan parameter string di
    `Encrypt`/`Decrypt`; AAD `(tenant, purpose, key_version, record_id)` ber-length-prefix
    (pemisah polos ambigu — `record_id` tak selamanya UUID); format ciphertext naik
    `0x01`→`0x02` dengan v1 DIKENALI parser tapi DITOLAK `Decrypt` (pesan menyebut
    re-enkripsi, bukan "blob rusak"); `record_id` di AAD SAJA, tak pernah di blob.
    `BlindIndex` sengaja tak berubah (wajib row-independent). Jalur baca repo mengambil
    identitas baris dari BARIS ITU SENDIRI (`dest[0]`), jalur audit dari `EntityID`.
  - DoD terpenuhi di DB nyata (`TestFieldCrypto_CiphertextDipindahAntarBarisDitolak`): tukar
    konsisten `_enc`+`_bidx` pada kolom non-Unique → baca GAGAL; pada kolom Unique, tukar
    konsisten justru ditolak DB sendiri (UNIQUE `_bidx` bentrok di tengah statement) sehingga
    yang diuji adalah pemindahan `_enc` saja → juga GAGAL.
    `TestFieldCrypto_BlindIndexTetapRowIndependent` menjaga lookup & UNIQUE tak ikut mati.
  - Empat mutasi kode produksi diverifikasi MEMBUAT test gagal: (1) buang `record_id` dari
    AAD → test tukar-baris gagal; (2) terima format v1 → test penolakan v1 gagal; (3) blind
    index ikut ber-baris di jalur tulis → 1 unit + 3 integrasi gagal; (4) `MockCrypto`
    berhenti mengikat → 4 test lapis atas gagal (mock terbukti bukan hiasan).
  - **Sisa risiko yang DIPUTUSKAN diterima:** menukar `_bidx` SAJA antar baris tetap mungkin
    → pencarian menemukan baris salah, tapi nilai yang dibaca tetap milik baris itu
    (integritas indeks, bukan kebocoran/atribusi palsu). Penutupnya butuh blind index
    ber-baris = menghapus kegunaannya. Wilayah kontrol akses tulis DB + rekonsiliasi.

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

### Sub-phase 5.0 — Sprint wiring (bayar utang live-wiring)

**Mendahului semua pekerjaan baru, termasuk Phase 4.** Sub-phase ini tidak menambah
kemampuan; ia menyalakan kemampuan yang sudah ditulis, diuji, dan dinyatakan SELESAI di
Phase 2–3 tapi tak pernah dirakit di `cmd/server`.

Temuan yang melahirkannya (audit 10 Agu 2026, ROADMAP vs kode aktual):

- Server hanya melayani `/healthz` + rute `surat_masuk`. **Satu-satunya driving adapter
  HTTP di seluruh repo** adalah `modules/surat_masuk/adapter/http/handler.go`.
- `buildServerHandler` memasang `RequireAuth()` untuk semua rute non-healthz, sementara
  tak ada endpoint login. Server rakitan sekarang **tak bisa melayani klien mana pun**:
  token hanya bisa dicetak di luar sistem.
- `RequirePermissionInUnit` **fail-open** bila evaluator nil (`gateway/context.go`), dan
  tak ada yang memanggil `SetScopedEvaluator` → seluruh ABAC unit kerja + hierarki OPD +
  delegasi (PR-2.3.5) tidak menegakkan apa pun di produksi.
- `core/workflow.Engine`, `core/scheduler.Runner`, `core/notification.*`,
  `core/customization.Manager`, `core/audit.Reader` — **nol** pemanggil di `cmd/server`.
  Itu 13 PR (3.2 ×7, 3.5 ×2, 3.6 ×2, N1–N3b ×4) yang tak punya jalur eksekusi produksi.
  `workflowActions` di `main.go` adalah map yang tak pernah di-dispatch.
- 15 dari 32 penanda `DEFERRED(` menunjuk fase yang **sudah lewat** (8× Phase-2.4,
  3× Phase-5.1.1, 3× Phase-5.1.2, 1× PR-3.8.3) — gerbang "Audit DEFERRED saat tutup fase"
  terlewat dua kali.

Aturan DoD 11 (lihat Definition of Done) mencegah utang jenis ini bertambah; sub-phase
ini membayar yang terlanjur ada. Urutan W1→W6 adalah urutan ketergantungan, bukan selera:
tanpa W1 tak ada token, tanpa token tak ada rute yang bisa diuji end-to-end.

- **PR-W1** Handler HTTP alur auth (`/auth/*`) ← 2.4.3, 2.4.4, 5.1.2 ✅
  - Driving adapter `identity/adapter/http`: `POST /auth/login` (`LoginEmployee`),
    `POST /auth/select-tenant` (`SelectTenant`), `POST /auth/public/login` (`LoginCitizen`),
    `POST /auth/public/otp/request` + `/auth/public/otp/verify` (`RequestOTP`/`VerifyOTP`).
  - Rakit di `cmd/server`: `identity/adapter/auth.NewBcryptVerifier`, `JWTCodec` sebagai
    **TokenIssuer** (sisi verify sudah ter-wire PR-5.1.2), `identitydb.CentralRoleResolver`,
    `TenantRoleResolver` per-tenant di atas `TenantConnManager`, `port.MessagingPort` dari
    `infra/messaging.NewFromConfig` (jalur OTP N3a), `port.RateLimiter` (sudah ada).
  - **Rute auth harus lolos `RequireAuth`** — jelas, tapi ia titik paling mudah salah:
    memasangnya di dalam business chain membuat login menuntut token untuk mendapatkan
    token. Pasang di top mux (seperti `/healthz`) dengan stack terbatas
    (Recovery/CORS/RequestID/RateLimit), TANPA Auth/RequireAuth/TenantResolver.
  - Menutup: backlog "[Phase-2.4] Live wiring alur login" + "Live wiring token codec"
    (sisa sisi issuer); marker `DEFERRED` di `identity/usecase/login.go:37` &
    `login_citizen.go:52` (yang terakhir sudah basi — OTP selesai di 2.4.4).
  - Belum ada use case pembuatan credential ber-password (`PasswordVerifier.Hash`) —
    dibutuhkan untuk seed admin pertama; masuk W2 bersama sentinel SYSTEM actor.
  - DoD: `cmd/server` e2e — boot → `POST /auth/login` mengembalikan token → token itu
    dipakai memanggil `POST /surat-masuk` dan diterima (bukan 401). Tanpa test ini, W1
    tidak selesai. ✅
  - **SELESAI.** Yang berdiri: `identity/adapter/http/handler.go` (5 endpoint, DTO kawat
    snake_case terpisah dari struct use case, body dibatasi 64 KiB karena rute ini menerima
    kiriman siapa pun); `cmd/server/auth.go` (`wireAuth` + `tenantRoleResolver` yang memilih
    pool tenant lalu mendelegasikan ke resolver tenantrole yang sengaja tak ber-tenantID);
    `mountAuthRoutes` di top mux dengan seam `authRoutes` agar pemasangannya bisa diuji tanpa DB.
  - **Proteksi brute-force jalur password ikut mendarat di sini, dan itu bukan scope creep**:
    memasang `/auth/login` tanpa proteksi = mempromosikan kelemahan yang selama ini dorman
    (REVIEW_BACKLOG A5, jalur OTP sudah terlindungi sejak 2.4.4) menjadi permukaan serang nyata.
    `usecase.passwordAuthenticator` — satu implementasi untuk employee & citizen, rate limit
    berlapis dua meniru `RequestOTP`. Kuncinya: **habisnya kuota lapis-2 menjawab 401 SERAGAM,
    bukan 429**, sebab lapis itu hanya tercapai untuk kredensial yang ada → 429 di sana adalah
    orakel keberadaan akun. `NewLoginEmployee`/`NewLoginCitizen` menerima limiter+policy sebagai
    parameter WAJIB (kontrol keamanan yang menunggu pemanggil ingat memasangnya bukan kontrol).
  - **RateLimit middleware gateway SENGAJA tidak dipasang di grup ini**: kuncinya per-principal,
    dan pada rute pra-otentikasi principal selalu `uuid.Nil` → semua penyerang berbagi SATU bucket
    global, sehingga ia memberi siapa pun cara mematikan login bagi semua orang. Rate limit per-IP
    menuntut keputusan proxy tepercaya yang belum diambil — tetap OPEN di REVIEW_BACKLOG A5.
  - **Empat mutasi kode produksi diverifikasi membuat test gagal:** (1) lapis-2 menjawab 429
    (orakel) → test pembanding pesan error gagal; (2) lapis-2 kembali ber-key nilai mentah
    (regresi A7) → 2 test gagal; (3) `/auth/login` dipasang di balik `RequireAuth` → test rute
    gagal dengan pesan "butuh token untuk memperoleh token"; (4) role tenant dibuang setelah
    di-resolve → klaim kosong + `POST /surat-masuk` jadi 403.
  - Menutup penanda: `DEFERRED(Phase-5.1.x)` route-grouping di `require_auth.go`,
    `DEFERRED(Phase-2.4)` live wiring di `login.go`, dan penanda basi di `login_citizen.go`.
  - Tidak ada perubahan struktur DB (tak ada DDL/ensure-on-write baru), tak ada permission/event baru.

- **PR-W2** Handler HTTP admin identity (`/admin/identity/*`) ← W1, 2.1.2, 2.4.5
  - `CreatePerson`, `AttachEmployment`, `AssignEmploymentToTenant`, `CreateCredential`
    (baru — butuh `PasswordVerifier.Hash`), `AssignCentralRole`.
  - Menutup GAP (b) PR-5.1.4: produsen event `identity.employment.ditugaskan` di server
    hidup. Clone engine sudah ter-wire (PR-5.1.4) tapi **tak ada yang menerbitkan
    event-nya** — jalur clone produksi belum pernah berjalan di luar test.
  - Sekalian tutup backlog "Sentinel SYSTEM actor": `assigned_by` NOT NULL membuat admin
    pertama chicken-and-egg. Seed lewat migrasi identity baru + `domain.SystemActorID`.
  - SENSITIF (identity) — review ekstra per CLAUDE.md.
  - DoD: `POST /admin/identity/assignments` → baris `gov.user_profiles` muncul di DB
    tenant tujuan dengan pengenal terenkripsi (bukti lewat bus NYATA, bukan driver memory). ✅
  - **SELESAI.** Yang berdiri: `identity/adapter/http/admin_handler.go` (5 endpoint POST, DTO
    kawat snake_case terpisah dari struct use case); `identity/usecase/create_credential.go`
    (jalur tulis password SATU-SATUNYA, hash lewat `port.PasswordVerifier` — bukan bcrypt lokal,
    supaya cost tak menyimpang dari sisi verifikasi); `cmd/server/admin_identity.go`
    (`wireAdminIdentity` + `mountAdminIdentityRoutes`); migrasi `identity/migrations/010`
    (seed sentinel) + `identity/domain.SystemActorID`.
  - **Grup ini dipasang di ROUTER BISNIS, bukan top mux** — kebalikan dari `/auth/*` (W1), dan
    itulah keputusan pemasangan yang menentukan. Router bisnis sudah dibungkus stack lengkap
    (Auth → RequireAuth → TenantResolver → RateLimit → Idempotency), dan tiap lapisnya memang yang
    dibutuhkan: rutenya menuntut token, aktornya ber-principal nyata sehingga rate limit
    per-orang punya arti, dan seluruhnya mutasi sehingga `Idempotency-Key` layak dihormati.
    Memasangnya di top mux menuntut menyalin ulang stack itu — dan salinan yang tertinggal satu
    lapis tak bergejala sampai ada yang menyerangnya.
  - **Repo dibungkus dekorator AUDIT**, kebalikan dari `wireAuth`/`wireIdentitySync` yang sengaja
    memakai repo telanjang (keduanya hanya membaca, dan login belum punya aktor). Grup ini
    seluruhnya mutasi oleh aktor terotentikasi → ADR-003 berlaku penuh. PR ini juga pemanggil
    produksi PERTAMA `AuditStore.EnsureSchema` (`id.audit_logs`); dipanggil saat BOOT, bukan lazy,
    supaya kegagalannya jadi gagal-boot alih-alih mutasi yang commit tanpa jejak.
  - **`NewAuditedCredentialRepo` baru**, dan `secret_hash` SENGAJA tak ikut diff: hash bcrypt bisa
    di-crack offline, jadi menyalinnya ke `id.audit_logs` menjadikan kompromi satu tabel audit =
    kompromi seluruh password — sementara ia tak menjawab pertanyaan yang audit ada untuk
    menjawabnya. `cred_value` ikut, tapi TERSEGEL (purpose diturunkan dari `cred_type`, ADR-017 §4).
  - **Batas panjang password (12 rune – 72 byte) mendarat di sini**, dan itu bukan scope creep:
    ini satu-satunya jalur tulis password, jadi floor yang tak dipasang di sini tak punya tempat
    lain untuk ada. Batas atas = batas bcrypt; di atasnya bcrypt memotong diam-diam sehingga dua
    password dengan 72 byte awal sama dianggap cocok. Keduanya `ErrValidation` (422), bukan 500 —
    `BcryptVerifier.Hash` memang menolak >72 byte, tapi di sana kegagalannya terbaca sebagai
    kesalahan server.
  - **Sentinel SYSTEM = baris NYATA di `id.persons`, bukan FK yang dilonggarkan.** `nik_enc`/
    `nik_bidx`-nya bytea zero-length: `Open` memetakannya ke string kosong (jadi `FindByID` tak
    error), `FindByNIK("")` tak menemukannya (bidx dari `""` adalah HMAC), dan `Person.Validate`
    menolak NIK kosong sehingga ia tak bisa dibuat ulang/ditimpa lewat repo — penulisnya hanya
    migrasi. Lihat DB_SCHEMA §3.1.
  - **Tiga mutasi kode produksi diverifikasi membuat test gagal:** (1) publish
    `identity.employment.ditugaskan` dihapus → e2e gagal ("clone tak muncul dalam 10 detik") +
    2 unit test gagal; (2) `RequirePermission` dihapus dari handler `assignments` → test
    "gerbang sebelum parse" gagal dengan 400 alih-alih 403 (pemeriksaan use case TIDAK
    menutupinya — ia baru tercapai setelah decoder menjawab); (3) migrasi 010 dihapus → e2e gagal
    dengan pelanggaran FK `tenant_assignments_assigned_by_fkey`.
  - **Satu kelemahan DORMAN ikut dibayar di sini, dan itu bukan scope creep** (preseden PR-W1):
    `core/permission.Engine` menggabungkan grant lintas lapis secara UNION, sementara
    `TenantRole.Validate` tak membatasi ISI `Permissions`. Sebelum PR ini tak ada permission
    `identity:*` yang punya eksekutor; memasang `/admin/identity/*` mengubahnya menjadi jalur
    pengambilalihan: admin tenant pemegang `iam:tenant_role:buat` bisa membuat role berisi
    `identity:credential:buat`, menugaskannya ke dirinya, menerbitkan kredensial ber-password
    pilihannya untuk person mana pun yang id-nya ia ketahui (id terbaca di `gov.user_profiles`
    tenantnya sendiri), lalu login sebagai orang itu. Ditutup dengan mereservasi namespace
    `identity:` bagi lapis sentral — ditolak di domain, ditegakkan di PINTU TULIS repo tenantrole.
    REVIEW_BACKLOG **B6 → TERTUTUP SEBAGIAN** — sebagian, bukan penuh, karena B8 di bawah
    melewatinya lewat jalur NAMA tanpa pernah menyebut `identity:`.
  - **DUA properti otorisasi TIDAK ditutup di sini, dan keduanya butuh keputusan di atas level PR
    ini** (karena itu didokumentasikan, bukan ditambal):
    * **B7 — containment aktor→TARGET.** Ketiga use case mutasi memeriksa "aktor punya
      permission?", tak pernah "target dalam wewenang aktor?". Yang terkuat justru endpoint BARU
      PR ini: `CreateCredential` menerima `person_id` mana pun, UNIQUE-nya `(cred_type,
      cred_value)` bukan `person_id`, dan login me-resolve murni lewat kredensial → menerbitkan
      kredensial SETARA dengan menjadi target. Penegakannya menuntut `tenant_scope` aktor, yang
      ada di klaim JWT tapi TIDAK terekspos lewat `port.AuthContext` — perubahan kontrak port
      lintas layer, plus kebijakan yang belum diputuskan ("no privilege escalation"?).
    * **B8 — tabrakan NAMA role tenant vs sentral.** Otorisasi di-resolve dari NAMA, dan
      `CompositeCatalog.Lookup` mendahulukan central, jadi role TENANT bernama `super_admin`
      me-resolve ke definisi SENTRAL ber-`LayerGlobal`. Itu melewati B6 tanpa menyebut
      `identity:` sama sekali. Penutupnya menyentuh `core/permission` (bawa lapis asal sampai ke
      titik evaluasi) → **butuh ADR**. Dorman hari ini (tenantrole belum punya permukaan HTTP);
      WAJIB ditutup sebelum permukaan itu ada.
  - **Durabilitas jalur clone belum sempurna dan itu diketahui:** `AssignEmploymentToTenant`
    menerbitkan event SESUDAH `assignments.Save` commit. Publish yang gagal → 500 dengan baris
    sudah tersimpan, dan retry berikutnya ditolak `ErrAssignmentDuplikat` sehingga clone tak
    pernah lahir. Penutupnya outbox transaksional, yang SENGAJA belum di-wire (PR-5.1.4:
    `OutboxStore` belum punya penulis produksi). Tak ditambal di sini karena tambalan mana pun —
    retry buta, atau melonggarkan anti-duplikat — menukar satu mode kegagalan dengan yang lain.
  - **`VerifyGate` kini dibagi tiga permukaan bcrypt** (LoginEmployee, LoginCitizen,
    CreateCredential), dirakit sekali di `run()` lalu diteruskan ke `wireAuth` & `wireAdminIdentity`.
    Gerbang per fungsi wiring akan melipatgandakan batas concurrency yang justru ingin ditegakkan
    — aturan yang sudah tertulis di `NewVerifyGate` sejak PR-W1, kini berlaku untuk penulis
    kredensial juga.
  - Perubahan struktur DB: `identity/migrations/010` (seed sentinel, ber-down) — tercatat di
    `docs/DB_CHANGELOG.md` & `docs/DB_SCHEMA.md`. Permission baru: `identity:credential:buat`.

- **PR-W3a** Provenance role + containment wewenang (B7+B8, ADR-019) ✅
  - **Menutup REVIEW_BACKLOG B7 & B8 lewat SATU ADR (019)** — keduanya satu keluarga cacat:
    *wewenang tidak dibawa sampai ke titik keputusan*. B8 kehilangan LAPIS ASAL role, B7
    kehilangan SCOPE aktor.
  - B8: `port.RoleRef{Origin,Name}` menggantikan nama role telanjang sebagai masukan
    `PermissionEvaluator.Allows`; `CompositeCatalog` me-resolve PER LAPIS ASAL dan berhenti
    mengimplementasi `RoleCatalog` (kompilasi = penegakannya); `gateway.Context` berhenti
    meratakan `TenantRoles`+`CentralRoles`.
  - B7: `identity/usecase/containment.go` — aturan Kubernetes (*privilege escalation
    prevention*) apa adanya, ditegakkan di PINTU TULIS, dengan pintu keluar eksplisit
    `identity:authority:escalate`. Wewenang teritorial aktor = `ctx.TenantID()`;
    `port.AuthContext` SENGAJA tidak diberi `TenantScope()` (ADR-019 Keputusan 3).
  - **`SetScopedEvaluator` TIDAK dipasang di sini** — itu PR-W3b. Jangan mengklaim seam scoped
    sudah hidup.
  - DoD: (b) role TENANT bernama persis sama dengan role SENTRAL tidak mewarisi permission
    maupun `LayerGlobal` ✅ (e2e: `POST /admin/identity/persons` → 403); (c) aktor ber-scope
    tenant A ditolak saat memutasi identity target di luar wewenangnya, termasuk
    `POST /admin/identity/credentials` ✅ (e2e, dengan kontrol positif 201 untuk target biasa).
    Keduanya diverifikasi lewat MUTASI dua arah.

- **PR-W3c** ✅ Pagar ukuran token JWT ← W3a — **SELESAI** (ADR-020, amends ADR-007)
  - **URUTAN: dikerjakan SEGERA setelah W3a, MENDAHULUI W3b.** Huruf di sini identitas, bukan
    urutan — W3b (wiring Authority) menyusul sesudahnya.
  - Masalah: `central_roles[]` + `tenant_roles[]` adalah satu-satunya klaim yang bertumbuh, dan
    tak ada pagar di mana pun — bukan di `tenantRoleNameRe` (mengizinkan nama 100 karakter),
    bukan di resolver, bukan di `JWTCodec.Issue`. Diukur dengan codec produksi: dasar ~420 B,
    tiap role menambah ≈ `panjang_nama × 1,37` B. 50 role@25 char = 2,3 KB (aman);
    100 role@100 char = 14,2 KB (**lewat batas nginx 8 KB**).
  - **Bentuk kegagalannya yang membuat ini mendesak:** login BERHASIL (200, token terbit), lalu
    SETIAP request berikutnya 400 dari proxy — tanpa satu pun jejak di log aplikasi, karena
    permintaan tak pernah sampai ke Go (`MaxHeaderBytes` default 1 MB, jauh di atas batas proxy).
    User terkunci total dan tak ada sinyal yang menunjuk penyebabnya. Yang paling mungkin kena
    justru akun paling penting: admin tenant yang mengakumulasi role lintas tahun.
  - Isi PR: (a) tolak di `JWTCodec.Issue` bila token melewati ambang aman (≈6 KB, di bawah 8 KB
    nginx) dengan error yang MENYEBUT jumlah role — bukan 500 generik; (b) metrik ukuran token
    (histogram) lewat `MetricsPort` supaya pertumbuhannya terlihat sebelum jadi insiden;
    (c) set `http.Server.MaxHeaderBytes` eksplisit di `cmd/server` agar batasnya satu tempat
    dan terdokumentasi, bukan bergantung default yang tak pernah dipilih siapa pun.
  - **Yang JANGAN dilakukan:** memasukkan permission ke token "supaya tak perlu katalog". Itu
    penyebab paling umum JWT membengkak, dan desain sekarang sengaja hanya membawa NAMA role —
    satu role dengan 40 permission tetap satu entri.
  - Batas ambang adalah kebijakan ops, jadi ia config (`GOV_AUTH_TOKEN_MAX_BYTES`), bukan
    konstanta — deployment di belakang ALB (16 KB) boleh lebih longgar dari nginx (8 KB).
  - DoD: token yang melewati ambang ditolak saat login dengan error eksplisit + ter-log, dan ada
    test yang menerbitkan token melewati ambang lalu memastikan ia TIDAK pernah sampai ke klien.
  - **Hasil:** pagar di `JWTCodec.Issue` (`core.ErrTokenTooLarge` → 409 `TOKEN_TOO_LARGE`, pesan
    menyebut jumlah role) + `port.MetricsPort.RecordSize` (histogram byte; `auth_token_bytes` untuk
    token yang lolos, counter `auth_token_oversize_total` untuk penolakan) + `MaxHeaderBytes`
    diturunkan dari ambang token (`maxHeaderBytes()`, `max(16 KiB, ambang+8 KiB)`). Ambang =
    `GOV_AUTH_TOKEN_MAX_BYTES`; 0 = default 6 KiB, <1 KiB ditolak saat boot; tak ada nilai yang
    mematikan pagar. `NewJWTCodec` beralih ke `token.Options`. DoD dibuktikan **Langkah 4**
    `TestE2E_Login_LaluAksesRuteBisnis`: akun yang login normal di Langkah 1 menumpuk 50 role
    @100 karakter lewat repo NYATA, lalu login berikutnya dijawab 409 tanpa token di badan
    respons. Token yang masih lolos di atas 80% ambang ter-log `Warn` (peringatan dini: pagar
    yang hanya menolak akan mengunci akun tepat saat rilis tanpa sinyal sebelumnya), dan nilai
    ambang EFEKTIF + `MaxHeaderBytes` ikut di-log saat boot (loader sengaja mengabaikan env yang
    tak bisa di-parse, jadi `=16k` diam-diam menyisakan default). Angka ROADMAP di atas dikunci sebagai test
    (`TestJWTCodec_UkuranTokenTumbuhSesuaiJumlahRole`: dasar 383 B, 137,6 B/role, 100 role =
    14.139 B) sehingga "memasukkan permission ke token" gagal lantang. 5 mutasi diverifikasi.
  - **Belum ter-expose:** kedua metrik baru menunggu `GET /metrics` di **PR-W6** (lihat butir (a)
    di sana). Sinyal yang bekerja hari ini = log.
  - **Tidak diselesaikan di sini (sengaja):** batas jumlah/panjang nama role tenant —
    `tenantRoleNameRe` masih tanpa batas panjang. Pagar token menangkap AKIBAT, bukan sebabnya;
    membatasi di pintu tulis `tenantrole` adalah perubahan perilaku bagi tenant yang mungkin sudah
    punya nama panjang. Lihat "Keputusan tertunda" ADR-020.

- **PR-W3b** ✅ Wiring Authority live → `SetScopedEvaluator` ← W1, 2.3.5, W3a — **SELESAI** (ADR-021, amends ADR-019)
  - Bangun `permission.Authority` (`Roles []RoleRef` + RoleGrants dari resolver tenant, emitter
    central-role→Grant `TenantWide`, DelegatedGrants dari resolver delegasi) →
    `ScopedEngine.Bind` → `gateway.Context.SetScopedEvaluator` di middleware Auth.
  - **Mengubah default dari permisif ke menegakkan** — begitu evaluator terpasang,
    `RequirePermissionInUnit` mulai menolak. Cari dulu pemanggilnya (`grep`) agar
    perubahan perilaku ini disengaja, bukan kejutan.
  - Emitter central-role→Grant belum dibuat (sengaja tak disentuh di PR-2.3.5).
  - **Wiring evaluator dan PEMANGGIL PRODUKSINYA wajib satu PR** (DoD 11). Hari ini
    `RequirePermissionInUnit` **nol pemanggil produksi**; memasang evaluator tanpa pemanggil =
    seam dorman, persis dosa yang Sub-phase 5.0 ada untuk membayarnya. Pemanggil alaminya
    `tenantrole.AssignTenantRole` & `delegation.CreateDelegation` (satu-satunya use case
    ber-`UnitKerjaID`); `SuratMasuk` TIDAK punya `UnitKerjaID` dan menambahkannya semata agar
    DoD bisa dibuktikan = memodelkan domain demi test. Ditolak.
  - Permukaan HTTP `tenantrole` boleh mendarat di sini — gerbangnya (B8) sudah tutup di W3a.
  - Menutup: marker `DEFERRED(Phase-2.4)` di `gateway/middleware/auth.go:73`; backlog
    "[Phase-2.4] Wiring Authority live + seam scoped".
  - DoD: dua request token identik beda `unit_kerja` → satu lolos satu 403, lewat stack
    HTTP nyata (bukan pemanggilan engine langsung).
  - **Hasil:** `permission.CentralGrants` (emitter sentral→Grant TenantWide) +
    `permission.BuildAuthority` + `middleware.EvaluatorFactory.Build` kini mengembalikan DUA
    evaluator (RBAC + scoped) dari satu panggilan → `SetScopedEvaluator` di Auth middleware.
    Perakitan Authority **lazy & ter-memo** per request (dua query tenant DB hanya dibayar oleh
    request yang benar-benar memeriksa unit); kegagalan resolver jadi ERROR, bukan `false`.
    Konteks tanpa tenant dapat Authority KOSONG (menolak), bukan evaluator nil (permisif).
    **Pemanggil produksi:** `AssignTenantRole` & `CreateDelegation` lewat
    `permission.RequireAuthorityOver`, termasuk kasus `unit_kerja_id` KOSONG = se-tenant
    (ditanyakan sebagai `AllowsInUnit(perm, uuid.Nil)`; `Validate` kedua domain kini menolak unit
    ber-UUID nol supaya pertanyaan itu sahih). **Permukaan HTTP baru** `/admin/iam/{tenant-roles,
    tenant-role-assignments,delegations}` di router bisnis (di balik RequireAuth), ter-audit.
    DoD dibuktikan `TestE2E_ABAC_PenugasanRole_ScopeUnitDitegakkan`: token yang SAMA → unit
    sendiri 201, unit lain 403, se-tenant 403, dan hanya satu baris tersimpan. 3 mutasi
    diverifikasi (use case tanpa unit / middleware tak memasang evaluator / cabang se-tenant
    dihapus) — semuanya membuat DoD gagal.
  - **Efek samping yang dibayar di sini:** `db.TxConn` (Conn + `Begin`) lahir karena
    `TenantRoleRepo` & `AuditRepo` memegang `*db.Pool` — yaitu tak bisa dipakai di jalur request
    multi-tenant sama sekali. `TenantRoutingConn` kini merutekan transaksi juga, dan
    `gov.audit_logs` mendapat penulis produksi pertamanya (ensure-on-write per tenant; lihat
    DB_CHANGELOG PR-W3b).
  - **Ditemukan review (2 putaran `/code-review`), diperbaiki di PR yang sama — semuanya lolos
    seluruh test sebelum ditemukan:** (a) `RefCatalog` diperoleh lewat type-assert atas katalog sentral, yang
    SELALU gagal di produksi (`identitydb.CentralRoleCatalog` tanpa `LookupRef`) → grant sentral
    tak pernah terbit dan `super_admin` platform 403 di setiap unit; kini lewat `CompositeCatalog`
    + test ber-katalog "Lookup-saja"; (b) `include_subtree` tak diperiksa → pemegang satu unit bisa
    membagikan jangkauan seluruh keturunannya (eskalasi lewat BOOLEAN) → `AllowsSubtree` +
    `RequirePermissionInSubtree`; (c) `NewNonDelegableSet()` kosong di wiring padahal ADR menjadikan
    himpunan itu alasan menunda pemeriksaan per-permission → `DefaultNonDelegable` (`identity:*`,
    `iam:*`, entri ber-namespace). Putaran KEDUA:
    (d) `CreateTenantRole` tak memagari ISI role — containment menjaga DI MANA, bukan APA, sehingga
    pemegang `iam:tenant_role:buat`+`assign` bisa MENCETAK role berisi `iam:delegasi:buat` yang tak
    pernah diberikan kepadanya, menugaskannya ke dirinya sendiri di unitnya (lolos containment), dan
    dengan itu membuat larangan `iam:*` pada delegasi berhenti berarti → `grantingPermissionPrefix`
    (ADR-021 Keputusan 6; residu permission BISNIS dicatat eksplisit, tidak ditutup);
    (e) `testkit.WithUnitAuthority(uuid.Nil)` didokumentasikan "se-tenant" tapi diperlakukan sebagai
    kunci map biasa — fake LEBIH KETAT dari produksi (grant `TenantWide` menutupi setiap unit &
    subtree), sehingga test bisa menghijaukan invariant yang tak ada → diselaraskan + test pembanding
    langsung terhadap evaluator produksi; (f) wiring `SetScopedEvaluator` hanya tertangkap e2e
    ber-tag `integration` — menghapusnya membuat seluruh suite non-integration tetap hijau (Context
    tanpa evaluator = PERMISIF, jadi tiap assertion "harus lolos" tetap lolos) → uji middleware yang
    membalik pertanyaannya (unit di LUAR jangkauan harus DITOLAK) + uji kegagalan factory harus
    menolak request. Ketiganya mutation-verified. Putaran KETIGA (atas `fc078ff`, temuan di
    bootstrap skema yang baru diseret wiring ini ke jalur request — bukan di logika containment):
    (g) DDL ensure-on-write berjalan TANPA advisory lock padahal `IF NOT EXISTS` tak atomik →
    terukur 11/12 ensure serentak gagal 23505 di `pg_namespace_nspname_index`, dan yang kalah gagal
    SESUDAH mutasinya commit (baris tersimpan tanpa audit) → satu helper `db.EnsureSchemaLocked`
    untuk semua jalur; (h) tiap pemeriksaan ABAC menjalankan 2–3 blok DDL SETIAP request (instance
    resolver baru tiap kali) → `db.SchemaMemo` per-instance + cache bahan per tenant di
    `newScopedDepsBuilder`; (i) memo audit ber-kunci tenant salah untuk repo ber-pool tetap
    (`id.audit_logs` sudah dipastikan saat boot) → kunci dari KONEKSI lewat `db.DBKeyer`.
  - **Residu yang disengaja:** delegasi belum memeriksa apakah PEMBUAT memegang tiap permission
    yang ia limpahkan (hanya jangkauan unit + `NonDelegableSet`); memegang `iam:tenant_role:buat`
    BERSAMA `iam:tenant_role:assign` setara dengan memegang seluruh permission BISNIS tenant di
    dalam jangkauan unit sendiri (sifat administrasi role — pagar berikutnya = flag `grantable_by`
    di manifest, bukan aturan prefiks); belum ada use case revoke;
    `unit_kerja_id` belum ber-FK ke `gov.org_units`; dan pengecekan unit PERTAMA per tenant per proses
    masih memicu DDL ensure-on-write (kini ber-advisory-lock & ter-memo, jadi aman dan sekali saja —
    runner migrasi framework-gov tetap solusi akhirnya, sudah ter-DEFERRED). Lihat "Keputusan
    tertunda" & §Konsekuensi ADR-021.

- **PR-W4** Runtime workflow + notifikasi + scheduler ← W1, 3.2.x, 3.5.x, 3.6.x, N1–N3b
  - Blok dorman terbesar. DIPECAH sejak awal jadi **W4a (engine + dispatch + store)** dan
    **W4b (scheduler runner + SLA deadline + notifier)**. Alasan pecahnya bukan panjang diff:
    W4b memasukkan goroutine berumur panjang ke `run()`, dan kelas cacat itu sudah dua kali
    memukul repo ini (`Subscribe` tanpa Flush; `Drain` tak menunggu). Dicampur dengan perakitan
    engine, dua sumber kegagalan sulit dipisah saat DoD gagal.
  - **Prediksi seam TERBUKTI, dan lebih dalam dari dugaan** — lihat ADR-022. Bukan hanya
    `workflowActions` yang tak punya kontrak dispatch; `DefinitionStore`/`TemplateStore` juga tak
    membawa `ctx` maupun tenant sama sekali (jadi satu engine proses-lebar mustahil memilih DB
    tenant yang benar), dan persistensi instance TIDAK ADA sama sekali — tak ada tabel, repo,
    maupun implementasi `InstanceStateReader`.

- **PR-W4a** Engine + dispatch + store per-tenant ✅ (ADR-022)
  - `port.WorkflowAction` + `WorkflowActionInput` (dispatch bertipe menggantikan `any`);
    `domain.WorkflowRegistry.RegisterAction` bertipe & ber-`error`;
    `ActionDispatcher.Dispatch(+params)`; `Engine.ExecuteRequest`/`TransitionRequest` (Entity
    untuk guard vs Params untuk action — dipisah tegas, ADR-022 Keputusan 2);
    `workflow.ActionRegistry`; `WorkflowRef.FS` + `workflow.SeedFS` (seed dari FS ter-embed);
    `gov.workflow_instances` + `InstanceStore` + `infra/workflow.DBInstanceStore` (optimistic
    locking + kunci transisi ber-sewa `gov.workflow_instance_locks`); `gateway/workflow`
    (`/workflow/instances*`); `cmd/server/workflow.go` (`workflowFactory`: tumpukan dirakit per
    permintaan dari pool yang di-resolve ulang; yang di-cache hanya penyiapan DB) ✅
  - DoD: `surat_masuk` disposisi LEWAT WORKFLOW dari template tenant, transisi di request
    TERPISAH dari start, seluruhnya lewat stack komposisi produksi (`buildServerHandler`) →
    baris disposisi tersimpan di tenant DB ✅ (`cmd/server/workflow_e2e_integration_test.go`;
    diverifikasi dua arah lewat mutasi pada dispatcher & seed)

- **PR-W4b** ✅ Scheduler runner + SLA deadline + notifier ← W4a — **SELESAI** (ADR-023)
  - **Keputusan residensi diambil: opsi (b)** — `gov.scheduled_jobs`/`job_runs`/`job_locks` pindah
    ke **DB sentral**. Prinsip yang ditetapkan ADR-023 dan bisa dipakai ulang: *residensi mengikuti
    PEMBACA, bukan penulis*. Penulisnya ber-tenant (dipanggil di tengah transisi), tapi pembacanya
    (`Runner.RunDue`) adalah loop proses-lebar tanpa tenant. Tiga alasan konkret: (1) prinsip itu;
    (2) `ScheduledJob.TenantID`/`JobRun.TenantID` sudah ada sejak PR-3.5.1 dan `tenant_id = ''`
    ("job level-platform") hanya koheren di tabel sentral — tipe datanya memang ditulis untuk itu,
    kebalikan dari ADR-022 yang memilih per-tenant justru karena port-nya tak membawa tenant sama
    sekali; (3) opsi (a) mengubah pool tenant yang hari ini dibuka MALAS menjadi pool permanen
    untuk setiap tenant tiap tick — `pool_idle` 5 × 20 tenant sudah menabrak `max_connections`
    default, jauh sebelum jumlah query jadi soal.
  - Terpasang: `DBJobStore`+`DBLocker` di atas pool sentral, `Runner` ber-locker tanpa syarat,
    `escalationJob` yang me-resolve tumpukan tenant SAAT BERJALAN dari `port.TenantFrom(ctx)`,
    `notificationFactory` per-tenant (in-app + email, dipakai DUA jalur), dan
    `WithDeadlines`/`WithNotifier` pada engine per-tenant. Driver messaging kini SATU untuk seluruh
    proses (dibagi OTP login & channel email) — `wireAuth` menerima `port.MessagingPort`.
  - Tiga perubahan yang lahir DARI perakitan, bukan direncanakan sebelumnya:
    (a) `Runner.Start` kini mengembalikan channel yang tertutup **sesudah siklus berjalan tuntas** —
    sebelumnya membatalkan ctx hanya menghentikan iterasi berikutnya sementara job yang sedang jalan
    tetap memegang lock & hendak menulis riwayat (kelas cacat Subscribe-tanpa-Flush, PR-3.1.3);
    (b) `Runner.invoke` menyisipkan `port.WithTenant` TANPA SYARAT termasuk saat kosong, agar job
    level-platform tak mewarisi tenant ambient dari `Trigger`/`Replay` yang dipanggil dari request;
    (c) `RenderedMessage.TemplateKey` — kolom `gov.notification_inapp.template_key` ada sejak
    PR-3.6.1 tapi tak pernah terisi di jalur produksi mana pun karena `Channel.Send` tak menerima
    key-nya.
  - Gerbang residensi: `infra/schema` dipecah tenant/central + `pamongctl migrate --central`.
    Nama schema kedua jalur sama-sama `gov`, jadi yang memisahkan hanyalah keanggotaan daftar —
    dikunci `TestResidensi_*`.
  - DoD terpenuhi ✅: `sla_notification_e2e_integration_test.go` — definisi NYATA modul referensi
    (`sla_hours: 72` → `sekretaris_daerah`; transisi selesai → notify `agendaris`) lewat
    `buildServerHandler`; deadline mendarat di `gov.scheduled_jobs` sentral ber-`tenant_id` ✅;
    `RunDue` → eskalasi ke inbox pemegang role KONKRET ✅ (dengan kontrol negatif: inbox aktor &
    agendaris tetap kosong); transisi `selesai` → notify agendaris ✅; runner berhenti bersih ✅.
    Sifat shutdown & tenant-ctx diverifikasi DUA ARAH lewat mutasi (`core/scheduler/runner_tenant_test.go`).
  - Perbaikan dari `/code-review high` (4 temuan, semua ditindak): (1) kegagalan notifikasi
    transisi tak lagi menjatuhkan request — pada titik itu transisi sudah tersimpan handler, jadi
    5xx adalah kebohongan yang retry-nya berbahaya (`tolerantTransitionNotifier`); (2) `Start`
    kini menjalankan siklus dengan `context.WithoutCancel` — meneruskan ctx shutdown apa adanya
    ikut membatalkan PEMBUKUAN sesudah handler (RecordRun/advance/Release), sehingga job yang
    efeknya sudah terjadi tampak tak pernah jalan lalu diulang setelah restart; (3) komentar yang
    menunjuk `gov.notification_deliveries` diperbaiki — `Hub.Send` sengaja tak mencatat kegagalan
    pra-dispatch, jejaknya di baris gagal `gov.job_runs`; (4) pool tenant tak di-resolve dua kali
    per request workflow (`TenantConnManager.Tenant` membaca registry tiap panggilan).
    Temuan (2) diverifikasi dua arah lewat mutasi; template eskalasi framework kini diseed
    (`seedFrameworkTemplates`) dan e2e membuktikannya dengan tidak menyeed sendiri.
  - Perbaikan dari `/code-review` **putaran ke-2** (5 temuan) — pola yang sudah berulang di repo
    ini: putaran pertama tak cukup, dan tiga dari lima adalah konsekuensi perbaikan putaran
    pertama. (a) `tolerantDeadlines` — putaran-1 menoleransi kegagalan NOTIFIKASI tapi membiarkan
    kegagalan penjadwalan SLA menjatuhkan transisi, padahal engine memanggil keduanya di titik yang
    sama; (b) `seedFrameworkTemplates` memakai `InsertIfAbsent`, bukan `Upsert` — seeder jalan tiap
    boot, jadi Upsert mengembalikan template ke bunyi bawaan setiap restart dan menghapus suntingan
    operator (preseden repo: `coreWf.SeedIfAbsent`); (c) notifier dirakit TERTUNDA sampai ada
    transisi ber-`notify:` — perakitan di muka membuat DB notifikasi yang bermasalah menjatuhkan
    SELURUH endpoint workflow tenant itu, termasuk GET riwayat; (d) `escalationJob` tak lagi
    me-resolve pool tenant dua kali; (e) komentar basi "kini dormant" di `gateway/workflow/handler.go`.
    Dua temuan lain sengaja TIDAK ditambal di sini karena menuntut kebijakan retry/rekonsiliasi —
    lihat backlog.
  - Menutup `DEFERRED(PR-W4b)` di `cmd/server/workflow.go`.
  - **Tindak lanjut ADR-024 (grup PR-W7 di bawah).** Analisis pasca-PR ini menemukan bahwa
    sembilan temuan di dua putaran review bukan kebetulan: ketiga tambalannya
    (`tolerantDeadlines`, `tolerantTransitionNotifier`, heuristik `berubah`) menutupi satu cacat
    struktural — efek samping durable dijalankan sebelum baris instance tersimpan, lintas dua DB.
    Yang dibangun di W4b TETAP (residensi sentral, runner, lock, seeder, pagar tenant, e2e);
    yang dibongkar W7 justru tambalannya.

- **PR-W4c** Seam entitas: snapshot guard + otorisasi tingkat entitas ← W4a
  - Dua kebutuhan, satu seam yang sama ("bolehkah aktor ini menyentuh entitas itu, dan seperti apa
    isinya sekarang?"), jadi dikerjakan bersama:
    (a) Guard ber-`entity.x` kini DITOLAK di jalur HTTP (fail-closed, ADR-022 Keputusan 7) karena
    handler tak punya cara membaca entity modul; snapshot TIDAK boleh datang dari body request
    (aktor akan menulis sendiri nilai yang menentukan apakah ia boleh lewat).
    (b) `workflow:instance:*` masih berlaku SE-TENANT lintas modul: pemegang `:baca` bisa membaca
    riwayat instance mana pun di tenantnya (komentar + id aktor), pemegang `:mulai` bisa memulai
    alur atas entitas apa pun. Pagar sementara: keunikan instance per entitas + permission domain
    yang tetap diperiksa use case di dalam action.
  - Menutup `DEFERRED(PR-W4c)` di `gateway/workflow/handler.go`.

- **PR-W5** Wiring customization write-path ← W1, 3.4.1, 3.4.2
  - Urutan WAJIB sudah tertulis lengkap di backlog "[Phase-5.1.1] Live wiring
    customization write-path" butir (a)–(e) — ikuti apa adanya, termasuk
    `customization.RegisterEventSchemas(bus.Schema())` SEBELUM `Manager` dipakai
    (tanpanya setiap write gagal di langkah publish).
  - Sekalian tutup butir "Atomicity store-write + publish" (alihkan ke outbox) bila
    outbox sudah punya penulis produksi; bila belum, catat ulang eksplisit.
  - Menutup: marker `DEFERRED(Phase-5.1.1)` di `core/customization/admin.go:22` &
    `events.go:45`.

- **PR-W6** Observability endpoint + audit reader ← W1, 3.7.2, 1.3.x
  - (a) Mount `GET /metrics` (`PrometheusMetrics.Handler()`, auth-free atau ber-auth
    sesuai kebijakan ops) + `observability.NewTracerProvider` di boot dengan
    `defer tp.Shutdown(ctx)` agar batch span ter-flush. Menutup marker
    `DEFERRED(Phase-5.1.1)` di `infra/observability/metrics.go:56` + backlog
    "[Phase-5.1.1] Live wiring metrics endpoint".
    - **Sudah ada yang MENUNGGU endpoint ini:** `auth_token_bytes` (histogram byte) &
      `auth_token_oversize_total` dari pagar ukuran token PR-W3c/ADR-020. Sampai W6 mendarat,
      pertumbuhan ukuran token hanya terlihat lewat log `Warn` di 80% ambang — cukup untuk
      menemukan AKUN, tak cukup untuk melihat TREN. Sertakan alert pada counter itu saat mount.
  - (b) Rakit `audit.NewReader` + handler baca audit ber-permission. **Menuntut keputusan
    REVIEW_BACKLOG E1 lebih dulu:** entri audit identity ber-realm `_central` tak akan
    pernah cocok dengan tenant aktor di `Reader` — siapa yang berwenang membaca audit
    sentral (kandidat: central role platform)? Putuskan sebelum menulis kode; kemungkinan
    butuh ADR.

### Grup PR-W7 — Perbaikan mendasar efek samping workflow (ADR-024)

Lahir dari analisis pasca-PR-W4b: perakitannya menuntut dua putaran review dengan sembilan
temuan, dan tiga temuan putaran-2 adalah konsekuensi perbaikan putaran-1. Akarnya bukan kualitas
perbaikannya melainkan **satu cacat struktural** — efek samping durable (jadwal SLA ke DB SENTRAL,
notifikasi + email ke DB TENANT) dijalankan SEBELUM baris instance tersimpan, lintas dua database,
tanpa transaksi yang mungkin melingkupinya. Semua tambalan W4b (`tolerantDeadlines`,
`tolerantTransitionNotifier`, heuristik `berubah` di handler) adalah gejala, bukan obat.

Analisis penuh + daftar cacat mikro M1–M8 + perbandingan dengan kode yang ada:
**`docs/adr/024-efek-samping-transisi-dan-sla-level-triggered.md`**.

**Urutan disengaja.** W7b mengubah interface publik `core/workflow` dan menulis ulang bagian
handler yang juga disentuh W4c — kerjakan **W7a–W7b sebelum W4c** agar handler tak ditulis dua kali.
Tarik juga **W6(a) ke depan**: relay tanpa metrik yang terlihat adalah komponen yang matinya diam.

- **PR-W7a** Null Object untuk seam opsional engine (ADR-024 K5) ← W4b
  - `WithDeadlines`/`WithNotifier` tetap ada; field selalu non-nil (default no-op). Hapus empat
    `!= nil` dari `core/workflow/engine.go` (baris 277, 291, 311 + konstruksi).
  - Kecil, mandiri, tanpa perubahan perilaku. Sengaja dipisah agar diff W7b tetap terbaca.
  - Test: mis-wiring tak lagi bisa menyamar sebagai "SLA sengaja mati".

- **PR-W7b** Engine mengembalikan NIAT + outbox efek tenant (ADR-024 K1, K2) ← W7a, W7-pra
  - **Prasyarat sudah lunas (2026-08-21, PR-W7-pra):** template notifikasi modul kini punya jalur
    seeding. Tanpa itu, menghapus `tolerantTransitionNotifier` akan mengubah setiap transisi
    ber-`notify:` modul menjadi baris DLQ, bukan baris log.
  - `ExecuteRequest`/`Start` → `(TransitionOutcome, error)` dengan `Effects []Effect`
    (`ScheduleDeadlineEffect` / `CancelDeadlineEffect` / `NotifyEffect`). Engine berhenti
    menyentuh DB mana pun; `error` darinya kembali berarti **satu hal**: transisi ditolak.
  - Tabel baru `gov.workflow_effects` (tenant DB). Handler menyimpan instance + baris efek dalam
    SATU transaksi tenant. **DB_SCHEMA.md + DB_CHANGELOG.md di PR yang sama.**
  - Relay efek (bentuknya menyalin `infra/eventbus/outbox.go` — store + retry policy + DLQ sudah
    ada di sana; JANGAN bikin mekanisme kedua). Tahap ini relay boleh masih per-pool sederhana;
    W7c yang memindahkannya ke penyapu bersama.
  - Dedup efek: `ScheduleDeadline` sudah idempoten lewat `jobIDForKey`; `Notify` di-dedup pada
    `(instance_id, transition_seq, to_role, channel)`.
  - **HAPUS** `tolerantDeadlines`, `tolerantTransitionNotifier` (`cmd/server/`), dan heuristik
    `berubah` (`gateway/workflow/handler.go:217`). Pertanyaan "200 atau 5xx" lenyap bersamanya.
  - Sesuaikan e2e `cmd/server/sla_notification_e2e_integration_test.go`: menunggu relay, bukan
    efek sinkron. Tambah test yang membuktikan **kegagalan Save tidak menyisakan efek terkirim**
    (mutasi: lepas transaksi → test harus gagal).

- **PR-W7c** `TenantSweeper` — pekerjaan latar per-tenant, anggaran O(worker) (ADR-024 K3) ← W7b
  - Registry `SweepTask` + kolam worker berukuran tetap (config, default 8) + cache pool tenant
    ber-LRU **berbatas** (`TenantConnManager` sekarang meng-cache tanpa batas & tanpa evakuasi —
    lihat M7). Daftar tenant dari `id.tenant_registry`, sumber yang sama dengan resolver runtime.
  - Pindahkan relay efek W7b ke sini sebagai `SweepTask` pertama; siapkan tempat untuk relay
    outbox event (yang sampai kini tak punya penulis produksi) dan pembersih idempotency.
  - Metrik lag relay + health check WAJIB di PR ini, bukan menyusul.

- **PR-W7d** Rekonsiliasi SLA level-triggered (ADR-024 K4) ← W7c
  - `SLAReconcileTask`: turunkan deadline yang SEHARUSNYA dari state terbuka + `sla_hours` +
    waktu masuk state (dari `History`), bandingkan dengan `gov.scheduled_jobs`, perbaiki selisih.
  - Butuh method baru pada `InstanceStore`: daftar instance terbuka di state ber-SLA (belum ada).
  - Menutup backlog "deadline gagal terjadwal tak punya jalur pemulihan" dengan **menghapus kelas
    masalahnya**, bukan menambah penanganan error.

- **PR-W7e** Kebijakan retry job one-shot (ADR-024 K6) ← W7d
  - `core/scheduler/runner.go:214` `advance` berhenti menonaktifkan one-shot tanpa memandang
    status. Backoff eksponensial + batas percobaan (default 5) → status `exhausted`, bukan dibuang
    diam-diam. `JobRun.Attempt` akhirnya terpakai di luar `Replay`.
  - Aman justru karena W7d sudah mendarat: rekonsiliasi adalah jaring terakhir.
  - Menutup backlog "job gagal tak pernah diulang".

- **PR-W7f** DDL keluar dari jalur request (ADR-024 K7) — track terpisah
  - Ganti `EnsureSchemaLocked` di jalur request dengan pola deploy-time: provisioning membuat
    skema, aplikasi **memeriksa & gagal cepat** bila tak sesuai (pola `migrate --check`).
  - Menutup kelas flake `TestEnsureSchemaLocked_BootParalel_TakBalapan` (23505 pada
    `pg_namespace_nspname_index`) dengan menghapus balapannya, bukan mengunci lebih rapat.
  - Menyentuh provisioning tenant → boleh dijadwalkan lepas dari W7a–W7e.

**Setelah W1–W7 tutup:** jalankan audit DEFERRED penuh
(`grep -rn 'DEFERRED(' --include='*.go'`) dan pastikan tak ada lagi penanda ber-fase lewat.
Baru sesudah itu ambil keputusan urutan berikutnya — Phase 4 (rule engine, pilar pendiri
yang memblokir 3.3.4/`core/fiscal`/5.2.2) vs entity tiers & `pamongctl eject` (janji DX
terbesar CLAUDE.md, **nol** baris rencana sampai sekarang — lihat backlog di bawah).

### Sub-phase 5.1 — API gateway

- **PR-5.1.1** Router aggregator ← 1.1.1, 2.4.2 ✅
  - Kumpulkan rute dari semua modul saat bootstrap
  - DoD: rute modul ter-register & dapat diakses ✅
  - Impl: `gateway.Router` (implementasi konkret `port.Router` di atas net/http.ServeMux
    method-aware Go 1.22+); `infra/db.TenantRoutingConn` (`port.DBConn` yang me-route DB
    per-tenant: `port.TenantFrom(ctx)` → `TenantConnManager.Tenant()`, dengan helper
    `port.WithTenant`/`TenantFrom` + fallback AuthContext); `config.HTTPConfig`/`HTTPAddr()`.
    `cmd/server` kini composition root PENUH: connect identity DB (bootstrap ADR-004), rakit
    driven adapter (eventbus/storage/metrics/tenant-routing DB), `NewApp`, Bootstrap semua modul
    (rute ter-register), `http.Server` + `/healthz` + recovery (crash-safety) + graceful shutdown.
    Smoke: boot → `/healthz`=200, `POST /surat-masuk` reachable (500 anggun karena Sequence
    Phase-1 belum ada), `GET /tak-ada`=404, SIGTERM=shutdown bersih.
  - GAP diketahui (bukan lingkup 5.1.1): Sequence generator (Phase-1) & UserResolver adapter
    produksi (Phase-2) belum ada → di-wire nil; jalur request yang memakainya gagal anggun.
    **Stack middleware KEAMANAN (auth/tenant/ratelimit/CORS/audit) = PR-5.1.2** — sampai itu,
    rute bisnis TANPA auth & RequirePermission permisif-default. Server BELUM layak deploy.

- **PR-5.1.2** Middleware stack ← 5.1.1, 2.2.2, 1.3.1
  - Auth, rate limit, tenant resolver, CORS, audit trail
  - DoD: request tanpa auth ditolak; rate limit aktif; audit tercatat

- **PR-5.1.3** Auto-generate CRUD endpoint ← 5.1.1, 1.1.2
  - Endpoint CRUD dasar dari entity def
  - DoD: entity baru otomatis punya endpoint GET/POST/PATCH/DELETE

- **PR-5.1.4** Live wiring clone engine identity ← 5.1.1, 2.2.4, 3.8.5b ✅
  - Rakit `identity/sync` di composition root: Engine (subscriber) + `TenantDBWriter` +
    `RepoCloneSource`, plus pendaftaran schema event identity ke registry bus
  - DoD: `identity.employment.ditugaskan` terbit di bus nyata → baris `gov.user_profiles`
    muncul di DB tenant dengan pengenal TERENKRIPSI, dan `UserResolver.ResolveByNIK`
    menemukannya lewat blind index ✅
  - Impl: `cmd/server/identity_sync.go` (`wireIdentitySync`) + `identity/domain.RegisterEventSchemas`
    (daftar event hidup bersama konstantanya, pola `core/customization.RegisterEventSchemas`),
    dipanggil dari `run()`. Repo identity dipakai tanpa dekorator audit (jalur clone murni
    baca); `cryptoSvc` yang sama disuntik ke kedua sisi — realm SENTRAL dibuka repo identity,
    realm TENANT disegel writer (ADR-017). Tak ada DDL baru: `gov.user_profiles` tetap
    ensure-on-write milik writer (DB_CHANGELOG 3.8.5a).
  - Menyusul review: (i) `eventbus.Drainer` + `Bus.Drain()`, `NATSDriver.Drain` kini MENUNGGU
    koneksi tertutup (batas 10 dtk) — `nats.Conn.Drain()` asinkron; `run()` menguras SESUDAH
    `srv.Shutdown` dan SEBELUM defer menutup pool, kalau tidak handler clone dipotong di tengah
    dan pesannya hilang (NATS Core tanpa re-delivery). (ii) `AppConfig.Validate` menolak
    `eventbus.driver` memory/KOSONG di luar development: driver memory mengantar sinkron dan
    mengembalikan error subscriber ke pemanggil `Publish`, sehingga clone yang gagal
    menggagalkan use case SESUDAH commit dan retry-nya menabrak invariant anti-duplikat.
  - Bukti: `cmd/server/identity_sync_integration_test.go` — NATS embedded (asinkron, bukan
    driver memory), migrasi identity nyata, dump `row::text` diperiksa termasuk bentuk hex;
    `infra/eventbus` drain (unit + integration "Drain menunggu handler selesai");
    `identity/domain/events_test.go` (cakupan daftar schema).
  - GAP diketahui (bukan lingkup 5.1.4): (a) event `Manifest().Events.Produces` MODUL belum
    didaftarkan ke schema registry — publish dari modul ditolak "event tak terdaftar", dan
    `surat_masuk` membuang error publish (`_ =`) sehingga hilangnya TANPA gejala
    → **ditutup PR-5.1.5** (sisi registrasi; `_ =` di use case belum diputuskan);
    (b) use case identity belum punya adapter HTTP, jadi produsen event penugasan di server
    hidup belum ada; (c) outbox transaksional belum punya penulis produksi (`OutboxStore`
    tak pernah dirakit), jadi `OutboxRelay` SENGAJA tidak di-wire — relay tanpa produsen
    hanya mem-poll tabel kosong; (d) subscription NATS berjalan SERIAL dan handler tak
    ber-deadline: satu tenant DB yang macet menahan antrean tenant lain. Tak ditambal timeout
    per-handler (pada transport tanpa re-delivery, membatalkan handler = kehilangan yang sama);
    yang menyelesaikan = consumer durable ber-ack + dispatch konkuren,
    DEFERRED(Phase-3.1.x) bersama rekonsiliasi clone.

- **PR-5.1.5** Registrasi schema event modul di composition root ← 5.1.4, 3.1.1 ✅
  - Daftarkan `Manifest().Events.Produces` SEMUA modul terdaftar ke registry schema bus;
    menutup GAP (a) PR-5.1.4
  - DoD: modul ter-bootstrap menerbitkan event yang dideklarasikan manifest-nya → subscriber
    di bus NYATA menerimanya dengan payload bertipe konkret ✅
  - Impl: `core/domain.Registry.RegisterEventSchemas(EventSchemaRegistrar)` — agregasi lintas
    modul hidup di registry (pola `StrictPermissions`), seam-nya interface sebaris agar
    `core/domain` tak menyentuh `infra/eventbus`; dipanggil dari
    `cmd/server/module_events.go` (`wireModuleEventSchemas`) SESUDAH `registry.Validate()` dan
    SEBELUM Bootstrap modul (Bootstrap = titik pertama modul bisa menerbitkan event).
  - Keputusan: (i) aturan nama→tipe TIDAK diduplikasi di `core/domain` — tetap milik
    `SchemaRegistry`, sehingga tabrakan dengan event non-modul (identity/customization) yang
    menumpang registry yang sama ikut tertangkap; dua modul dengan nama event sama & tipe
    payload berbeda MENGGAGALKAN BOOT, nama sama & tipe sama lolos (idempoten).
    (ii) `Events.Consumes` yang produsennya tak terpasang TIDAK menggagalkan boot — Consumes
    antar modul memang loose coupling (modul referensi sengaja tanpa `DependsOn` ke
    kepegawaian) dan deployment berbeda memasang himpunan modul berbeda; tapi kondisinya
    DILAPORKAN saat boot (`Registry.ExternalSubscriptions` → log warn), sebab pada jalur NATS
    pesan untuk event tanpa schema dibuang diam-diam sehingga "subscriber tuli" dan "produsen
    tak terpasang" mustahil dibedakan dari luar.
  - Bukti: `cmd/server/e2e_integration_test.go` — bus NATS embedded (sisi TERIMA merekonstruksi
    payload lewat schema registry; driver memory tak bisa membedakan terdaftar/tidak),
    POST /surat-masuk 201 → subscriber menerima `surat_masuk.surat.diterima` bertipe
    `SuratDiterimaPayload` dengan nomor agenda & tenant benar; `core/domain/event_schema_test.go`
    (agregasi, atribusi modul pada konflik, Consumes menggantung). Tiga mutasi diverifikasi
    gagal: registrasi dilewati, event dihapus dari `Produces`, tipe payload manifest diganti.
  - Tidak ada perubahan struktur DB (tak ada DDL/ensure-on-write baru).
  - GAP tersisa: `_ = uc.publisher.Publish(...)` di `modules/surat_masuk/usecase` SENGAJA tak
    disentuh — mengubahnya = keputusan semantik (event best-effort vs use case gagal SESUDAH
    commit), yang jawaban sebenarnya adalah outbox transaksional (belum punya penulis produksi,
    GAP (c) PR-5.1.4). Kini setidaknya event-nya benar-benar terkirim, bukan hilang di gerbang.

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
11. **Terpasang di composition root pada PR yang sama** — lihat aturan di bawah
12. **Kontrak sambungan ditulis sebelum komponen kedua** — lihat aturan di bawah

### Aturan 11 — tidak ada komponen "selesai tapi dorman"

**Sebuah komponen tidak boleh dinyatakan SELESAI tanpa wiring-nya di `cmd/server`
(atau `pamongctl`, bila di situ tempatnya) pada PR yang sama.** Berlaku untuk apa pun
yang punya jalur eksekusi produksi: adapter, middleware, use case ber-handler, subscriber
event, job scheduler, store.

Alasannya bukan kerapian, melainkan pengalaman terukur di repo ini. Pola lama
("adapter di-test dulu, live wiring menyusul") dipakai berulang di Phase 2–3 dan
menghasilkan empat cacat yang tak mungkin terlihat sebelum dirakit, semuanya baru
muncul berbulan-bulan kemudian saat Phase 5.1:

- `NATSDriver.Subscribe` tanpa Flush → event hilang permanen (PR-3.1.3 dinyatakan
  selesai Juni; cacatnya baru ketahuan akhir Juli lewat test yang dikira "flaky").
- `SchemaRegistry` selalu kosong di `run()` → **semua** publish modul ditolak (PR-5.1.4).
- Driver `memory` mengantar sinkron → clone gagal menggagalkan use case SESUDAH commit
  (fix `f5f1a65`).
- `Bus.Drain` tak menunggu koneksi tertutup → handler clone terpotong saat shutdown
  (fix `438c6df`).

Test unit & integrasi per-komponen semuanya hijau selama itu. Yang tidak diuji adalah
**rakitannya**, dan itu satu-satunya bentuk yang dijalankan di produksi.

Konsekuensi praktis:

- PR yang menambah adapter WAJIB menyentuh `cmd/server` (atau menyatakan eksplisit di
  deskripsi PR mengapa komponen ini memang tak punya jalur produksi — mis. mock testkit).
- Bukti DoD-nya adalah test yang menjalankan **rakitan**, bukan komponen: pola
  `cmd/server/e2e_integration_test.go` (bus NATS embedded + `buildServerHandler`),
  bukan pemanggilan konstruktor langsung.
- Bila wiring benar-benar terhalang dependensi yang belum ada, komponen itu **BELUM
  SELESAI**: statusnya `SEBAGIAN` dengan penanda `DEFERRED(...)` di kode + entri backlog,
  dan sub-phase-nya tidak boleh ditutup. Jangan diberi ✅.

Ini aturan yang mengoreksi cara kerja Phase 2–3, bukan penilaian ulang atasnya —
utang yang terlanjur ada dibayar lewat sprint PR-W1..W5 (Sub-phase 5.0).

### Aturan 12 — kontrak sambungan ditulis sebelum komponen kedua

Sebelum menulis komponen **kedua** yang akan berbagi sambungan dengan komponen yang sudah
ada, tulis dulu **kontrak sambungannya**: siapa memanggil siapa, sinkron atau tidak, apa yang
terjadi bila gagal **di tiap sisi**, dan di mana batas durabilitasnya. Satu paragraf di PRD
atau doc comment port — bukan dokumen tersendiri. Bila kedua sisi menulis durable ke database
yang berbeda, itu **ADR**, bukan komentar review.

**Pemicunya dibatasi tiga keadaan saja**, supaya tidak berubah jadi upacara yang dilewati:

1. sambungan melintasi **batas durabilitas** (ada commit di antara kedua sisi), atau
2. sambungan melintasi **batas tenant/DB**, atau
3. komponen ditulis **sebelum pemanggilnya ada**.

**Kenapa ini tidak sama dengan "rancang makro dulu".** Aturan 11 sudah ada sebelum PR-W4b
dan W4b MEMATUHINYA — komponen dirakit, rakitannya diuji. Tetap butuh dua putaran review
dengan sembilan temuan. Aturan 11 menjawab *"apakah dirakit?"*; yang gagal adalah *"apa
semantik sambungannya?"*. Rancangan makro yang lebih tebal tidak menutup itu — ia menutup
hal-hal yang bisa dibayangkan, dan melewatkan sisanya sama saja, hanya dengan sunk cost yang
membuat orang enggan membongkar.

Yang menutupnya adalah pertanyaan mekanis lima menit (CLAUDE.md §Aturan pengembangan #11):
daftarkan tulisan durable di jalur itu berurutan, tandai titik commit. Diterapkan ke jalur
transisi W4b, daftarnya langsung berbunyi sendiri — dan tak satu pun test per-komponen bisa.

Lahir dari PR-W4b / ADR-024. Catatan penting soal cara menyimpan pelajaran: memori proyek ini
sudah memuat "satu putaran review tak cukup untuk perubahan lifecycle" sejak PR-W4a. Ia
**memprediksi** W4b dengan tepat dan tidak mencegahnya sedikit pun. Pelajaran yang disimpan
sebagai peringatan tidak mengubah perilaku; yang mengubah perilaku adalah gerbang dan daftar
periksa. Karena itu aturan ini punya pemicu yang bisa diperiksa, bukan nasihat.

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

- **[Sub-phase 5.0] Hasil audit DEFERRED 10 Agu 2026 — 15 penanda ber-fase LEWAT.**
  Gerbang di atas terlewat saat menutup Phase 2.4 dan saat menutup PR-5.1.1/5.1.2, jadi
  audit dijalankan mundur. Dari 32 penanda `DEFERRED(` di kode, yang fase tujuannya sudah
  tiba/lewat:
  - `DEFERRED(Phase-2.4)` ×8 — publish event role sentral/tenant/delegasi
    (`identity/usecase/create_central_role.go`, `assign_central_role.go`,
    `tenantrole/usecase/create_tenant_role.go`, `assign_tenant_role.go`,
    `delegation/usecase/create_delegation.go`, `delegation/domain/policy.go`),
    wiring Authority (`gateway/middleware/auth.go:73` → **PR-W3b**), live wiring login
    (`identity/usecase/login.go:37` → **PR-W1**).
  - `DEFERRED(Phase-5.1.1)` ×3 — metrics endpoint, customization Manager & RegisterEventSchemas
    (→ **PR-W5/W6**).
  - `DEFERRED(Phase-5.1.2)` ×1 — `infra/db/routing_conn.go:19` residency routing.
    (Dua sebutan `Phase-5.1.2 #1/#2` lain di `gateway/context.go` & `infra/db/central_conn.go`
    adalah catatan "ini MENUTUP penanda X", bukan utang terbuka — jangan ikut dihitung.)
  - **Basi, hapus saja saat menyentuh file-nya:** `identity/usecase/login_citizen.go:52`
    (OTP sudah selesai PR-2.4.4), `core/customization/customfield.go:18` `DEFERRED(PR-3.8.3)`
    (enkripsi custom field — 3.8.3 selesai, tapi rekonsiliasi `CustomFieldDef.Class`
    memang masih terbuka; ubah ref-nya ke PR-W5, jangan dibiarkan menunjuk PR yang tutup).
  - Penanda event role sentral/tenant/delegasi (6 dari 8) **tidak** masuk W1–W6: belum ada
    konsumen, dan konsumennya (refresh/revoke klaim token) menumpang keputusan epoch
    `tokens_valid_after` yang juga belum diambil. Dijadwalkan ulang eksplisit ke
    **Phase-5.0+ / bersama use case revoke** — lihat butir "[Phase-2.4] Revocation
    per-person" di bawah. Ini penjadwalan ulang sadar, bukan kelalaian yang diperpanjang.

- **[Phase-5.x] Job yang GAGAL tak pernah dicoba ulang — eskalasi SLA bisa hilang permanen.**
  Ditemukan `/code-review` putaran-2 PR-W4b. `Runner.runOnce` memanggil `advance` tanpa syarat, dan
  `advance` menonaktifkan job one-shot terlepas dari statusnya. Jadi satu kegagalan SEMENTARA di
  fire-time (tenant DB sedang tak terjangkau, template belum ada) menjatuhkan eskalasi SLA untuk
  SELAMANYA; yang tersisa hanya baris gagal di `gov.job_runs`, dan `Replay` belum punya permukaan
  ter-wire. Sengaja TIDAK diperbaiki di PR-W4b: perbaikan yang benar menuntut kebijakan retry
  (backoff, batas percobaan — `JobRun.Attempt` sudah ada tapi selalu 1), dan "coba ulang tiap tick
  selamanya" akan menghantam DB tiap 30 detik untuk job yang rusak permanen. Itu keputusan desain,
  bukan tambalan wiring. Kerjakan bersama permukaan admin scheduler (retry/replay manual) atau saat
  outbox punya penulis produksi — keduanya butuh kebijakan retry yang sama.
  **DIJADWALKAN: PR-W7e** (ADR-024 K6). Batas percobaan jadi aman di sana karena PR-W7d sudah
  memasang rekonsiliasi sebagai jaring terakhir.

- **[Phase-5.x] Deadline SLA yang gagal dijadwalkan tak punya jalur pemulihan.** Konsekuensi sadar
  dari `tolerantDeadlines` (PR-W4b): kegagalan `ScheduleDeadline` dicatat metrik
  `workflow_sla_deadline_failed_total` + log, tapi state itu tak akan pernah mengeskalasi.
  Mempropagasi error tidak memulihkannya juga (deadline sama-sama hilang, hanya klien ikut
  dibohongi) — pemulihan yang sesungguhnya adalah REKONSILIASI: job periodik yang membandingkan
  instance ber-state SLA di tiap tenant terhadap `gov.scheduled_jobs` sentral lalu menjadwalkan
  yang hilang. Itu job scheduler biasa; bisa ditulis begitu ada kebutuhan nyata (mis. setelah
  metrik di atas benar-benar bergerak).
  **DIJADWALKAN: PR-W7d** (ADR-024 K4). Kesimpulannya naik pangkat: rekonsiliasi bukan lagi
  "bisa ditulis bila perlu" melainkan MODEL KEBENARAN untuk SLA — penjadwalan saat transisi
  turun status jadi optimasi. Tepi yang terlewat sembuh sendiri, bukan ditangani.

- ~~**[Phase-5.x] Template notifikasi MILIK MODUL tak punya jalur seeding.**~~ **DITUTUP
  2026-08-21** oleh PR-W7-pra: `domain.NotificationRef` di manifest → `collectNotificationSeeds`
  (parse + validasi di BOOT) → `seedTemplates` ber-`InsertIfAbsent` saat tumpukan notifikasi
  tenant disiapkan. Tiga hal ditambahkan di luar seeding itu sendiri, karena menambal satu modul
  tidak menutup lubangnya: (a) key template modul WAJIB berawalan `{modul}.` — template modul
  di-seed sebagai baris global, jadi key polos bertabrakan DIAM antar modul (InsertIfAbsent =
  yang boot lebih dulu menang); (b) `validateNotifyTemplatesSeeded` menjatuhkan BOOT bila ada
  `notify.template` yang tak punya default di mana pun — "developer harus ingat" jadi "mustahil
  lupa"; (c) e2e berhenti menyeed template manual sehingga jalur produksinya yang diuji.
  Entri asli disimpan di bawah sebagai riwayat.

  <details><summary>Entri asli</summary>

  **[Phase-5.x] Template notifikasi MILIK MODUL tak punya jalur seeding.** Ditemukan saat
  merakit PR-W4b. `gov.notification_templates` kini diisi seeder framework untuk template milik
  FRAMEWORK saja (`seedFrameworkTemplates` di `cmd/server/notification.go`, default global
  `tenant_id=''`). Template yang dirujuk definisi workflow MODUL — mis. `surat_selesai` di
  `modules/surat_masuk/workflows/disposisi.yaml` — tidak diseed siapa pun: `domain.Manifest` tak
  mengenal notifikasi sama sekali. Akibatnya `notify:` modul selalu `ErrTemplateNotFound` pada
  instalasi baru. Ditutup SEMENTARA oleh `tolerantTransitionNotifier` (kegagalan notifikasi dicatat
  log+metrik, tidak menjatuhkan transisi yang sudah tersimpan) — jadi bukan kegagalan diam, tapi
  juga belum berfungsi. Perbaikan yang benar: tambahkan `Notifications []NotificationTemplateRef`
  ke manifest modul (pola sama dengan `Workflows []WorkflowRef` ter-embed) lalu seed bersama seed
  definisi workflow di `workflowFactory.prepare`. Kerjakan bersama PR-W5 (yang juga menyentuh
  jalur seed/customization) atau saat modul kedua butuh notifikasi.
  **Catatan ADR-024:** `tolerantTransitionNotifier` yang kini menutupinya DIHAPUS di PR-W7b, dan
  kegagalan render berpindah ke relay (tercatat + di-retry + ber-DLQ). Jadi butir ini harus tutup
  SEBELUM atau BERSAMA W7b — kalau tidak, tiap transisi ber-`notify:` modul menjadi baris DLQ.

  </details>

- **[Phase-5.x] Definisi workflow BASELINE tak punya jalur upgrade.** Ditemukan `/code-review`
  PR-W7-pra. `DBStore.SeedIfAbsent` menulis hanya bila `workflow_id` itu belum punya versi APA PUN
  (`WHERE NOT EXISTS ... WHERE workflow_id = $1`), jadi tenant yang sudah di-provision **selamanya**
  memakai definisi versi pertamanya. Perubahan baseline apa pun — termasuk yang wajib, seperti
  rename key template notifikasi di PR ini (`surat_selesai` → `surat_masuk.surat_selesai`) — tak
  pernah sampai ke mereka, dan gagalnya diam.

  Itu **bukan bug `SeedIfAbsent`**: sifat all-or-nothing-nya sengaja, dan doc-nya menyebut
  alasannya — menyisipkan versi baseline baru membuatnya menjadi versi TERBARU yang menggantikan
  definisi hasil kustomisasi tenant. Jadi "seed versi berikutnya bila lebih tinggi" adalah obat
  yang lebih berbahaya daripada penyakitnya. Yang dibutuhkan adalah keputusan desain: bagaimana
  baseline developer naik versi tanpa menimpa kustomisasi tenant — kandidat: merge tiga-arah,
  atau memisahkan lajur versi baseline dari lajur versi tenant (`authoring_source` sudah ada dan
  membedakan keduanya). Kemungkinan besar butuh ADR.

  Peredam sementara yang SUDAH terpasang (PR-W7-pra): `laporStaleNotifyTemplates` di
  `workflowFactory.prepare` membaca definisi yang benar-benar tersimpan dan **melaporkan** setiap
  `notify.template` yang tak punya default — tenant, alur, versi, transisi, key. Ini memberantas
  DIAM-nya, bukan kegagalannya. Sengaja tidak menggagalkan: satu key basi tak boleh mematikan
  seluruh permukaan workflow tenant, termasuk GET riwayat yang tak menyentuh notifikasi.

  Peredam kedua (PR-W7-pra putaran 2): `legacy_keys:` pada entri template modul menanam key LAMA
  dengan isi yang sama, dikecualikan dari aturan awalan `{modul}.` — `modules/surat_masuk/`
  memakainya untuk `surat_selesai`. Ini memberi rename key template jalur migrasi yang tak
  bergantung pada masalah versi definisi di butir ini. Ia **utang**: setiap alias adalah baris
  global tambahan di ruang nama bersama, dan seluruhnya harus dihapus begitu jalur upgrade
  baseline ada. Cakupannya juga terbatas pada rename KEY TEMPLATE — perubahan baseline lain
  (state baru, guard baru, SLA berubah) tetap tak sampai ke tenant lama.

  **Belum tertutup, sengaja:** pemeriksaan drift hanya membaca versi TERBARU tiap definisi
  (`DBStore.Get`), padahal instance mengunci `DefinitionVersion`-nya sendiri — versi lama yang
  masih di-pin instance berjalan tanpa diperiksa. Menutupnya butuh query "daftar semua versi"
  yang belum ada di `DBStore`, dan hasilnya tetap laporan, bukan perbaikan. Digabungkan ke sini
  karena obat sesungguhnya sama: jalur upgrade definisi.

- **[infra/db] Flake `TestEnsureSchemaLocked_BootParalel_TakBalapan`** (teramati LIMA kali,
  18, 19, 21 & 22 Agu 2026 — dua kali pada 22 Agu): 1 dari 12 ensure paralel gagal dengan
  `duplicate key ... pg_namespace_nspname_index` (SQLSTATE 23505) — persis kegagalan yang advisory
  lock di `EnsureSchemaLocked` seharusnya cegah. TIDAK tereproduksi dalam 11 percobaan berikutnya
  (3 run `./...` penuh dengan perubahan PR-W4b, 3 tanpa, 8 iterasi terfokus `-count=8`), dan kode
  yang terlibat tak disentuh PR-W4b. Bukan diabaikan: bila lock benar-benar bocor sesekali, gejala
  produksinya adalah ensure-on-write yang gagal SESUDAH mutasi commit (baris tersimpan tanpa audit)
  — alasan asli lock itu ada (PR-W3b). Hipotesis yang perlu diuji lebih dulu: visibilitas katalog
  `CREATE SCHEMA IF NOT EXISTS` bagi transaksi yang MULAI sebelum pemegang lock commit. Cara
  menagihnya: jalankan test itu ber-`-count` tinggi di CI dan catat frekuensinya sebelum menduga
  penyebab. Bukti tambahan 19 Agu: 25× berturut terisolasi dan 3× paket penuh — semua hijau; jadi
  pemicunya ada pada kondisi suite penuh (beban/koneksi), bukan pada test itu sendiri.
  Statusnya kini **berulang di bawah suite penuh** (4 dari 4 run terakhir), sementara terisolasi
  tetap hijau (25× berturut, 3× paket penuh) — jadi pemicunya adalah kondisi suite (beban, jumlah
  koneksi, katalog yang sudah ramai), bukan test-nya. **Koreksi 22 Agu:** dugaan sebelumnya bahwa
  goroutine yang kalah selalu #7 TIDAK bertahan — run keempat kalah di #10. Indeksnya acak;
  jangan kejar itu sebagai petunjuk.
  **Penyempitan 22 Agu (kejadian kelima), memperbaiki dugaan di atas:** ia tereproduksi pada run
  `-tags=integration -p 1` atas EMPAT paket saja (`cmd/server`, `infra/notification`,
  `infra/workflow`, `infra/db`) — bukan `./...` penuh. Sesudahnya, `infra/db` sendirian tetap
  hijau: 6× test itu terisolasi dan 4× paket penuh. Jadi pemicunya BUKAN ukuran suite melainkan
  **aktivitas paket lain lebih dulu pada instance Postgres yang sama** (yang semuanya melakukan
  `DROP SCHEMA gov CASCADE` di setup). Itu justru menguatkan hipotesis snapshot katalog: yang
  berubah antar kondisi bukan beban, melainkan riwayat katalog. Kalah di goroutine #5 kali ini —
  konsisten dengan koreksi di atas bahwa indeksnya acak.

  **DIJADWALKAN: PR-W7f** (ADR-024 K7) — jalan keluarnya bukan mengunci lebih rapat melainkan
  mengeluarkan DDL dari jalur request sama sekali; balapannya hilang, bukan dikalahkan.
  Eksperimen murah yang layak dijalankan sebelum W7f, untuk memastikan diagnosanya benar:
  gabungkan `pg_advisory_xact_lock` dan DDL ke dalam SATU Exec (satu command string) lalu ulang
  suite penuh. Bila balapan lenyap, penyebabnya adalah snapshot katalog per-command, bukan lock
  yang bocor — dan itu mengubah bunyi perbaikannya.

- **[tooling] `pamongctl validate modules` tak memeriksa `Manifest.Notifications`.** Ditemukan
  `/code-review` PR-W7-pra putaran 4. `NotificationRef` yang cacat (FS nil, Path kosong, YAML
  rusak, key salah namespace, placeholder di luar kontrak) lolos gerbang CI dan baru jatuh saat
  boot `cmd/server`. Gerbang boot memang yang lebih kuat — jadi ini lubang pada pemeriksaan yang
  MURAH, bukan pada pemeriksaannya sendiri: developer modul baru harus menjalankan server untuk
  tahu manifestnya salah. Perbaikannya kecil: `collectNotificationSeeds` sudah pure (hanya
  membaca registry), tinggal dipanggil dari validator. Digabungkan saja ke PR tooling berikutnya
  yang menyentuh `pamongctl validate`.

- **[core/workflow] `ParseYAML` definisi alur masih longgar terhadap field tak dikenal.**
  Ditemukan pada putaran yang sama. `core/notification.ParseYAML` kini memakai
  `KnownFields(true)` karena field OPSIONAL yang salah ketik ter-parse mulus lalu diam
  (`legacy_keys` → alias tak pernah ditanam). `core/workflow/loader.go` punya kelonggaran yang
  sama dan permukaan opsional yang lebih luas (`sla_hours`, `escalate_to_role`, `notify`,
  `guards`) — `sla_hour:` yang salah ketik menghasilkan state tanpa SLA, tanpa satu pun error.
  TIDAK diubah di PR ini karena definisi alur juga datang dari DB (bukan hanya YAML ter-embed),
  jadi blast radius-nya beda dan perlu ditimbang tersendiri.

- **[BELUM PUNYA RENCANA — perlu keputusan] Permukaan desain CLAUDE.md tanpa satu pun
  baris di ROADMAP.** Ditemukan saat audit yang sama, dengan membandingkan konsep di
  CLAUDE.md terhadap daftar PR. Bukan utang implementasi (tak ada kode yang menunggu),
  melainkan **lubang perencanaan** — dan itu sebabnya ia tak pernah muncul di review
  per-PR mana pun:
  - **Entity tiers & progressive eject** — 16 sebutan di CLAUDE.md, **0** di ROADMAP.
    Ini janji DX terbesar framework ("~60–70% entity Tier 1, zero Go code",
    `pamongctl define entity`, `eject hooks`, `eject usecase`). Yang terdekat cuma
    PR-5.1.3 (auto-CRUD endpoint, belum dikerjakan) & PR-5.2.1 (scaffold, masih stub
    `notImplemented`). Tanpa ini, klaim "rails, bukan kebebasan" belum punya rel.
  - **Penegakan periode fiskal (`Lockable`)** — `core/fiscal` hanya CLAUDE.md + PRD.md.
    `port.FiscalChecker` sudah jadi seam (PR-3.3.3) tapi tak ada implementasinya, jadi
    gerbang non-retroaktif strategy choice belum nyata.
  - **`data_lifecycle: annual_cutoff`** — carry-forward & aggregation spec dideklarasikan
    di manifest tapi tak ada mesin yang membacanya.
  - **Bulk operations** — CLAUDE.md menyebutnya WAJIB disediakan framework (idempotency &
    optimistic locking sudah; bulk belum).
  - **Pipeline migrasi data legacy** — juga pemblokir sisa PR-3.8.5 (staging table).
  - **Portabilitas tier tenant** (`pg_dump → restore → update registry`) — 0 sebutan.
  - **12 dari 14 rule linter** di tabel CLAUDE.md belum ada (aktif: `domainnoinfra`,
    `permissionregistered`). Seluruh filosofi "enforcement lewat tooling, bukan
    dokumentasi" bersandar pada PR-5.3.1 yang belum dikerjakan.
  Keputusan penjadwalan diambil **setelah Sub-phase 5.0 tutup**, bersama pertanyaan
  Phase 4 vs entity tiers. Dicatat di sini supaya tak hilang lagi.

- **[PR-3.3.2 — bagian INTI SELESAI] Tenant config ber-scope + resolver.** `gov.tenant_configs`
  (KV ber-scope `tenant[/unit_kerja[/resource]]`) + `core/config.Resolver` "paling spesifik
  menang" + `core/config.TenantConfigStore` (Memory + Postgres `infra/config`) + migration
  `core/config/migrations/001`. `core/strategy` kini memakai `ConfigSelectionSource` di atas
  resolver ini sebagai jalur produksi (MemorySelectionSource tinggal untuk test). DoD terpenuhi:
  scope unit kerja meng-override tenant (unit-test + integration test). Rekonsiliasi template
  selection SELESAI PENUH di PR-3.3.2b (a-d, lihat backlog).

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

  **Riwayat & audit pilihan template — SELESAI PENUH (PR-3.3.2b, butir a-d).**
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
  - (c) ✅ **SELESAI**: permission `workflow:template:pilih` (`core/workflow/permissions.go`,
    permission workflow pertama, namespace ikut preseden opsi A `customization:*` dari PR-3.4.1)
    ditegakkan langsung di `TemplateChoiceManager.SetChoice` (baris pertama, sebelum validasi
    apapun) — belum ada use case admin/gateway terpisah karena Manager belum punya pemanggil
    seed/internal yang perlu skip permission. `TenantID` dipaksa dari `AuthContext.TenantID()`
    (pola `customization.Manager`, cegah actor tulis tenant lain). Slot-ownership validation
    ("template sah UNTUK slot itu", cegah arahkan slot ke definisi modul lain) ditegakkan via
    `strings.CutPrefix(TemplateID, Slot+".")` + tolak bila sisa (varian) masih mengandung titik
    — bukan `HasPrefix` biasa, agar slot bertingkat (mis. "keuangan.spm" vs "keuangan.spm.lanjutan")
    tidak saling lolos lewat kecocokan prefix string semata. **Barrier "jangan buka ke UI admin
    tenant" TERANGKAT** — butir a/b/c/d semua closed.
  - **[doc-stale] Komentar `DBTemplateStore` — ✅ DIPERBAIKI bersama butir (c).** Doc struct di
    `infra/workflow/template_store.go` sebelumnya menyebut "UPSERT pada (tenant_id, slot) … menimpa
    pilihan sebelumnya" dan "set_by BUKAN audit trail … Audit & versioning penuh menyusul di PR-3.3.2".
    Diluruskan: append-only ber-versi (versi lama tersimpan, `GetTenantConfigVersions`), otorisasi
    ada di `TemplateChoiceManager`, bukan store.

- **[Backlog] ChoiceManager (`core/config/choice.go`) permission gap.** Ditemukan saat review
  PR-3.3.2b butir (c): `customization.Manager` dan `workflow.TemplateChoiceManager` kini
  menegakkan permission LANGSUNG di dalam Manager (konvensi yang mengkonsolidasi setelah
  PR-3.4.1), sementara `config.ChoiceManager.SetChoice` (dipakai `core/strategy` untuk pilihan
  strategy key) TIDAK punya permission check apapun — doc comment lama menyebut "milik use case
  admin pemanggil (belum ada)" yang kini kontradiktif dengan dua Manager lain. Belum ada
  pemanggil produksi (`grep -rn 'ChoiceManager\b'` di luar `core/config` = 0 hasil di luar
  komentar), jadi TIDAK exploitable sekarang, tapi harus ditutup sebelum `ChoiceManager` di-wire
  ke gateway/UI admin (sama seperti barrier butir (c) di atas sebelum dibuka). Sulit langsung
  disamakan dengan pola workflow/customization karena `ChoiceManager` generik lintas SEMUA
  strategy key (`{modul}.{titik}`) — perlu keputusan: satu permission generik
  (`strategy:choice:pilih`, kasar seperti `workflow:template:pilih`) atau permission per-modul.
  Selesaikan saat menyentuh write-path strategy selection (kandidat: bersama PR-3.3.4 opsi
  irisan rule-tier, atau PR tersendiri sebelum wiring gateway Phase-5.1.1).

- **[PR-3.6.x] Konsumsi role binding saat notifikasi/eskalasi. ✅ RESOLVED PR-N2.** `ApplyBindings`
  (PR-3.2.4) mengganti peran generik → role konkret tenant pada `State.EscalateToRole` &
  `NotifySpec.ToRole`. PR-N2 menyambungkannya: `Engine.StartFromTemplate` (opsi `WithTemplates`)
  mengambil `TemplateStore.GetTenantConfig` + `ApplyBindings` sekali di Start, membekukan hasilnya
  ke `WorkflowInstance.RoleBindings`; `ExecuteWithComment` menerapkan ulang `ApplyBindings` pada
  SETIAP transisi (bukan cuma initial_state) sebelum membaca state/transition — jadi
  `Escalation.EscalateToRole`/`NotifySpec.ToRole` konsisten peran KONKRET sepanjang hidup
  instance. Resolusi role→orang (PLT fallback) tetap di `core/notification.RoleNotifier`
  (core/permission + kepegawaian); Engine tetap tenant-agnostik.

  **Prasyarat keamanan — ✅ RESOLVED PR-N2.** Nilai `RoleBindings` kini divalidasi SAAT TULIS:
  `TemplateChoiceManager.SetChoice` menolak binding yang menunjuk role tak terdaftar di
  `gov.tenant_roles` milik tenant, lewat seam `RoleChecker` (`core/workflow/template_choice.go`)
  + adapter `infra/workflow.TenantRoleChecker`.

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

  Kaitannya dengan butir `go:embed` di atas: race `CREATE SCHEMA IF NOT EXISTS` yang sama akan
  muncul DI PRODUKSI begitu `EnsureSchema` dipanggil saat bootstrap aplikasi. **Untuk 4 komponen
  core race ini SUDAH DITUTUP** — `db.ApplyEmbeddedSchema` (jalur baru `EnsureSchema` core)
  membungkus seluruh bootstrap dalam satu tx ber-`pg_advisory_xact_lock` (kunci tunggal
  `pamong.schema.bootstrap`). Yang MASIH rawan: `EnsureSchema` yang belum dimigrasikan ke pola ini
  (`infra/db/audit.go`, `infra/eventbus/outbox.go`, identity/tenantrole/delegation) — pindahkan
  saat komponen identity dikerjakan (butir `go:embed` di atas). Catatan: race test lintas-paket
  di atas TETAP terpisah dari race produksi ini — advisory lock menutup CREATE SCHEMA konkuren,
  bukan DROP-saat-paket-lain-pakai; obat test = DB/skema per-paket lalu cabut `-p 1`.

- **[core SELESAI; identity TERSISA] Migrasi core & identity tidak dijalankan migrator —
  satukan lewat `go:embed`.** Dulu `tools/pamongctl/migrate.go` mengunci akar ke flag `--modules`
  (`"modules"`) sehingga HANYA `modules/*/migrations/` dimuat; `core/*` & `identity/*` tak pernah
  masuk `gov.migration_history`, tak bisa di-`down`. Yang membuat tabel = `EnsureSchema` dgn const
  DDL inline (DDL ganda: const AKTIF + `.sql` DEKORATIF).

  **SELESAI untuk 4 komponen core (config/notification/scheduler/workflow):** tiap komponen kini
  mengekspor `//go:embed migrations/*.sql` + `MigrationModule` (`core/{comp}/migrations.go`);
  `infra/db.LoadEmbedded(module, fs)` memuat dgn nama modul EKSPLISIT (buang penurunan-dari-path
  yg rapuh utk kasus embed); `infra/schema.CoreMigrations()` merakit → `pamongctl migrate`
  menggabungkan `modules/` + core (terbukti: `migrate status/up/down` menampilkan config/
  notification/scheduler/workflow, `down` me-rollback workflow:003). `EnsureSchema` di-collapse ke
  `db.ApplyEmbeddedSchema(module, fs)` — **sumber tunggal `.sql`, const DDL paralel DIHAPUS**
  (net −161 baris). ApplyEmbeddedSchema melacak `gov.migration_history` (skip yg sudah apply →
  idempoten walau dipanggil berulang & saat dua store berbagi satu komponen) DALAM SATU tx
  ber-`pg_advisory_xact_lock` (kunci tunggal `pamong.schema.bootstrap`) → **race boot produksi
  CREATE SCHEMA gov untuk komponen core: DITUTUP**. Test reset diseragamkan ke
  `DROP SCHEMA IF EXISTS gov CASCADE` (drop-tabel-saja tak cukup krn history bertahan).

  **TERSISA (PR terpisah — identity sensitif, review ekstra CLAUDE.md):** terapkan pola yang sama
  ke `identity/migrations/*` (6 migrasi) + `EnsureSchema` di `identity/sync/writer_tenantdb.go`,
  `tenantrole/adapter/db/*`, `delegation/adapter/db/schema.go`. Juga `infra/db/audit.go` &
  `infra/eventbus/outbox.go` masih const DDL (audit & outbox punya EnsureSchema sendiri; outbox
  bahkan tanpa dir `migrations/`) — pindahkan bila dirasa perlu. `LoadEmbedded`+`ApplyEmbeddedSchema`
  sudah jadi seam siap-pakai; tinggal tambah embed + `MigrationModule` di komponen identity dan
  daftarkan ke assembler (identity punya assembler/DSN sendiri — lihat wiring migrate).

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

- **[PR-W3a → postur repo-wide] Default permisif `RequirePermission` saat evaluator nil.**
  `gateway.Context.RequirePermission` mengembalikan nil bila `c.eval == nil` (peninggalan seam
  2.3.2 agar alur lama tak pecah). Artinya konteks tanpa evaluator LOLOS semua gerbang permission,
  bukan ditolak. Hari ini tak terjangkau lewat HTTP (`RequireAuth` + `evaluatorFactory.Build` yang
  tak pernah nil), tapi ia membuat setiap kontrol baru mewarisi arah gagal yang salah — ADR-019
  harus memasang `requireTenantBound` justru untuk tidak bergantung padanya. Isi: jadikan
  fail-closed (atau wajibkan injeksi evaluator di konstruksi Context), sapu handler test yang
  mengandalkan default lama, dan pastikan rute publik `/auth/*` memang tak memanggil
  `RequirePermission`. **Butuh ADR** (perubahan postur, bukan tambalan). **Gerbang: sebelum
  permukaan mutasi non-HTTP pertama (CLI/job/importer) mendarat** — di situlah ia berhenti
  teoretis.

- **[PR-W3a → butuh perintah] Seed admin platform pertama lewat `pamongctl`.** Sejak ADR-019
  Keputusan 5, pemegang PERTAMA `identity:authority:escalate` tak bisa lahir dari dalam aplikasi
  (memberikannya menuntut sudah memegangnya) — sama seperti admin pertama tak bisa dibuat lewat
  API. Jalurnya hari ini: repo/SQL langsung, dicontohkan `seedAdminBootstrap` di
  `cmd/server/admin_identity_e2e_integration_test.go`. Yang belum ada: perintah `pamongctl`
  yang melakukannya secara resmi (person + employment + credential + role sentral bootstrap
  ber-`escalate` + assignment ber-sentinel SYSTEM). Tanpa itu, instalasi nyata pertama menempuh
  jalur tanpa validasi bentuk pengenal dan tanpa audit — persis yang `CreateCredential` ada
  untuk mencegahnya. **Gerbang: sebelum onboarding tenant NYATA pertama.**

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

- **[Modul kepegawaian] ActingFor PLT-jabatan di `DBRecipientDirectory`.** PR-N1
  (`infra/notification/directory.go`) mengembalikan `ActingFor` KOSONG (`nil, nil`) — bukan
  menunda karena teknis, tapi keputusan sadar (dikonfirmasi user, 2026-07-26): `gov.delegations`
  (`delegation/`) bersifat berbasis-PERMISSION ("user X meminjam wewenang A,B,C"), BUKAN
  "user X ditunjuk PLT menjabat Kadis" — tak ada tabel "penunjukan PLT jabatan". Menebak PLT
  dari delegasi wewenang berisiko salah kirim notifikasi resmi ke orang yang cuma dipinjami
  sebagian izin, bukan menjabat. Konsekuensi interim: jabatan kosong → `ErrNoRecipient`
  (fail-loud via `Router.Resolve`), bukan salah kirim diam-diam — ini benar untuk sekarang.
  Saat modul kepegawaian (penunjukan PLT/pelaksana jabatan) ada: isi `ActingFor` dari sumber
  PLT-jabatan yang benar (bukan `gov.delegations`). Fallback PLT sebagai MEKANISME tetap
  teruji lewat `MemoryDirectory` (unit test PR-3.6.2) — hanya SUMBER data DB yang ditunda.

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

- **[Phase-5.1/7] Pisahkan modul contoh `surat_masuk` dari registry produksi.** `surat_masuk`
  adalah modul REFERENSI (sample project) — panduan menggunakan framework, dirujuk di seluruh
  docs sebagai pola yang ditiru. Saat ini ia terdaftar di `modules.All()` (`modules/registry.go`)
  sehingga ikut ter-register ke binary produksi `cmd/server` DAN toolchain `pamongctl`. Selama
  framework dibangun ini disengaja (satu-satunya modul yang menguji wiring end-to-end; jadi
  harness validasi Phase 7). Yang perlu dilakukan saat registrasi modul solid (Phase 5.1) atau
  saat ia formal jadi test harness (Phase 7): `All()` produksi kosong/berisi modul riil saja;
  `surat_masuk` di-register HANYA dari jalur dev/test (integration test + dev-main atau build
  tag), atau dipindah ke `examples/`. Tujuan: contoh tetap in-repo (tak boleh rot — break CI
  saat core API berubah) tapi tak ikut terkirim sebagai default produksi. JANGAN keluarkan dari
  repo — nilai utamanya justru sebagai canary yang terkompilasi bersama framework.

- **[Phase-6.2 / gabung studi ERP] Metadata presentasi field → auto-form (UI autoflow).**
  `FieldDef` (core/domain) sekarang murni lapisan-data (Name/Type/Required/Unique/Default/
  Options/LinkTo/MaxSizeMB/Precision) — nol atribut presentasi. Niat Tier 1 = "generated penuh
  incl. form UI", tapi form-generator baru hidup di Phase 6 (`ui/` Frappe + adapter Go) dan
  endpoint CRUD di PR-5.1.3 — keduanya belum ada. **Keputusan bentuk (final, belum diimplementasi
  agar `FieldDef` tak churn sebelum ada konsumen):** ikuti model **Odoo** (pisah storage vs
  presentasi), BUKAN Frappe (lebur `fieldtype`):
  1. **Type → widget default** (tabel milik framework): Date→datepicker, DateTime→datetime,
     Boolean→checkbox, Enum→select, Link→autocomplete, File→upload, Text→input 1-baris,
     Decimal→numeric. Nol-config untuk mayoritas = "autoflow" tanpa buat+configure view.
  2. **Hint presentasi opsional** `UI *FieldUI` di `FieldDef` (nil = pakai default Type):
     `Widget` (override dari REGISTRY tertutup — mis. `"textarea"`,`"richtext"` — konsisten
     pola registry & "rails not freedom", BUKAN free-form), `Rows int` (mis. textarea N baris),
     `MaxLength int` (menyetir lebar input teks), `Placeholder`,`HelpText`,`ReadOnly`,`Hidden`,
     `InListView bool`, `DependsOn string` (tampil-bila-kondisi — **pakai ulang DSL guard yang
     sudah ada**, bukan mesin baru). "Sekian baris textinput" = `Widget:"textarea",Rows:N` di
     atas tipe `Text` yang sama (storage tak berubah).
  3. **Hexagonal aman:** `FieldUI` = data deklaratif (string/enum/bool), nol-dep infra → boleh
     di `core/domain`. Domain deklarasi "widget apa"; adapter `ui/` yang memutuskan "digambar
     bagaimana" (sejalan split "domain deklarasi endpoint, gateway routing").
  4. **Override per-tenant** → jatuh ke **customization layer (titik ekstensi #4)** +
     `gov.tenant_customizations` (custom field/label/tampilan) — jalur yang sama dengan write-path
     kustomisasi yang sedang di-park. Karena referensinya identik (DocField Frappe / view Odoo /
     `AD_Field` iDempiere), **gabungkan ke studi ERP open-source yang di-park bersama keputusan
     permission-namespace** — satu studi, dua keputusan (permission + UI-metadata).
  Sudah dikerjakan sekarang (biaya nyaris nol, benar apa pun desain UI): `created_by`/`updated_by`
  masuk `reservedFieldNames` (system-managed, non-assignable UI) — kolom actor-nya sendiri BELUM
  di-generate DDL (menyusul saat generation pipeline punya konteks aktor; nama dilindungi lebih
  dulu agar tak bentrok field modul — lihat catatan di `infra/db/ddl.go`).

- **[Phase-5.1.1] Live wiring metrics endpoint + tracing lifecycle.** `infra/observability`
  kini punya `PrometheusMetrics` (port.MetricsPort, registry privat, `Handler()` exposition
  Prometheus) dan `NewTracerProvider`/`Tracer()` (OTEL, OTLP/gRPC ke `GOV_OTEL_ENDPOINT`) —
  keduanya diuji (termasuk span benar-benar diterima "collector" lewat fake gRPC server, tanpa
  Docker). Belum di-wire ke `cmd/server/main.go` (preseden storage/token/login: adapter
  di-test dulu, live wiring menyusul saat ada konsumen nyata — lihat ROADMAP §"Live wiring
  HTTP/messaging/ratelimit konkret → Phase 5.1.1"). Saat wiring: (a) `domain.NewApp(...,
  observability.NewPrometheusMetrics(), ...)` untuk `Metrics`; (b) mount `GET /metrics` via
  `app.Router()` begitu router (PR-5.1.1) ada; (c) panggil `observability.NewTracerProvider`
  sekali di boot dari `cfg.Observ` (Enabled/Endpoint/ServiceName), `defer tp.Shutdown(ctx)` di
  shutdown server agar batch span ter-flush.

- **[Phase-5.1.1] Live wiring customization write-path (PR-3.4.1).** `core/customization.Manager`
  + store Postgres (`infra/customization`) siap & teruji tapi BELUM di-construct di produksi
  (`cmd/server/main.go` masih stub `nil` semua; Router = Phase 5.1.1). Saat wiring, WAJIB urut:
  (a) construct DB pool → `NewDBCustomFieldStore`/`NewDBTenantCapabilityStore` + `EnsureSchema`
  (atau `pamongctl migrate` modul `customization`), config store untuk label; (b) rakit
  `CapabilityRegistry` dari deklarasi modul + `CapabilityResolver`; (c) **`customization.
  RegisterEventSchemas(bus.Schema())` SEBELUM `Manager` dipakai** — tanpa ini `eventbus.Bus.
  Publish` menolak keempat event `customization.*` dan tiap write gagal di langkah publish
  (dijaga wiring_test tapi tak bisa dijalankan otomatis di boot); (d) `NewManager(...)` dengan
  publisher = bus; (e) expose HTTP admin (driving adapter) yang panggil `Manager.CreateCustomField/
  DeactivateCustomField/SetLabel/SetCapability` — butuh Router (PR-5.1.1) + permission
  `customization:*` sudah terdaftar (`docs/contracts/permissions.md`). Marker kode:
  `DEFERRED(Phase-5.1.1)` di `core/customization/admin.go` (doc `Manager`) & `events.go`
  (`RegisterEventSchemas`). **Saat itu juga tutup butir berikut:**
  - **Atomicity store-write + publish.** Manager kini `Save`/`Set` lalu `Publish` sebagai dua
    langkah terpisah — publish gagal = data tertulis, caller dapat error, cache merge tak
    ter-invalidasi. Alihkan ke OUTBOX (PR-3.1.2) agar event ikut transaksi tulis. Selama
    publisher nil (belum di-wire) tak ada dampak. Marker `DEFERRED(Phase-5.1.x)` di `admin.go`.
  - Penerapan custom field & label override ke FORM/generator = butuh metadata UI field
    (`FieldUI`, studi ERP) — DEFERRED lebih jauh (~Phase 6), bukan bagian wiring backend ini.
