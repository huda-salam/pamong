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
    `surat_masuk` membuang error publish (`_ =`) sehingga hilangnya TANPA gejala;
    (b) use case identity belum punya adapter HTTP, jadi produsen event penugasan di server
    hidup belum ada; (c) outbox transaksional belum punya penulis produksi (`OutboxStore`
    tak pernah dirakit), jadi `OutboxRelay` SENGAJA tidak di-wire — relay tanpa produsen
    hanya mem-poll tabel kosong; (d) subscription NATS berjalan SERIAL dan handler tak
    ber-deadline: satu tenant DB yang macet menahan antrean tenant lain. Tak ditambal timeout
    per-handler (pada transport tanpa re-delivery, membatalkan handler = kehilangan yang sama);
    yang menyelesaikan = consumer durable ber-ack + dispatch konkuren,
    DEFERRED(Phase-3.1.x) bersama rekonsiliasi clone.

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
