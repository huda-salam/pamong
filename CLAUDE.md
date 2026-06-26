# CLAUDE.md — Pamong Framework

Instruksi ini berlaku untuk seluruh sesi Claude Code di repo ini.
Baca seluruh file ini sebelum melakukan perubahan apapun.

**Dokumen wajib baca (di luar file ini):**
- `docs/CODING_PHILOSOPHY.md` — *mengapa* di balik keputusan teknis; rujukan saat aturan terasa membatasi.
- `docs/CODE_CONVENTION.md` — standar konkret penulisan kode Go (banyak ditegakkan linter).
- `docs/DOCUMENTATION_CONVENTION.md` — cara menulis PRD, ADR, komentar, CLAUDE.md lokal.

**Tiap komponen punya `CLAUDE.md` (panduan kerja ringkas) + `PRD.md` (spesifikasi).**
Saat bekerja di satu komponen, baca CLAUDE.md lokalnya + PRD-nya + port terkait —
tidak perlu membaca seluruh file ini lagi setelah sesi pertama. Modul referensi resmi:
`modules/surat_masuk/` — tiru polanya saat membuat modul baru.

---

## Konteks proyek

Pamong adalah backend framework ERP untuk pemerintah daerah (Provinsi & Kab/Kota)
yang ditulis dalam Go. Framework ini menyediakan fondasi agar developer modul bisnis
(keuangan, kepegawaian, aset, dll) dapat bekerja dengan standar yang konsisten, aman,
dan comply terhadap regulasi pemerintah Indonesia.

Frontend menggunakan Frappe UI dengan adapter Go (REST/WebSocket).

Tujuan utama framework ini adalah **menyediakan rails, bukan kebebasan** — convention
over config, constraint bertingkat, dan enforcement lewat tooling, bukan hanya dokumentasi.

> **Rencana pembangunan bertahap** ada di file terpisah `ROADMAP.md` —
> phase, sub-phase, dan daftar jobs/PR. CLAUDE.md ini memuat konvensi & aturan
> yang berlaku permanen; ROADMAP.md memuat urutan pengerjaan.

---

## Daftar isi

1. Arsitektur: Hexagonal (Ports & Adapters)
2. Struktur monorepo
3. Konfigurasi & konvensi
4. Konvensi penamaan (Go, database, event, permission)
5. Identity & manajemen user
6. Use case vs workflow
7. Fleksibilitas & titik ekstensi
8. Titik ekstensi (open/closed)
9. Entity tiers — progressive eject
10. Modular monolith & module packaging
11. Data integrity (idempotency, optimistic locking, bulk)
12. Data lifecycle — annual cutoff vs continuous
13. Tenant tier & portabilitas
14. Migration strategy
15. Data migration pipeline (legacy import)
16. Aturan pengembangan wajib
17. Testing
18. Git & branching
19. Pull Request
20. CI/CD gates & linter rules
21. Cara menambah modul baru
22. Cara kerja dengan core framework
23. Architecture Decision Records (ADR)
24. Checklist sebelum commit
25. Catatan khusus untuk Claude Code

---

## Arsitektur: Hexagonal (Ports & Adapters)

Seluruh domain — termasuk modul bisnis dan core services — WAJIB mengikuti
pola hexagonal architecture. Ini tidak opsional.

```
┌─────────────────────────────────────────────────────┐
│                    Domain Core                       │
│   Entity · Use Case · Domain Service · Port          │
└──────────────┬──────────────────┬───────────────────┘
               │                  │
        (Port / Interface)  (Port / Interface)
               │                  │
   ┌───────────▼──────┐  ┌────────▼──────────┐
   │  Driving Adapter │  │  Driven Adapter    │
   │  (HTTP handler,  │  │  (DB repo, event   │
   │   CLI, scheduler)│  │   bus, storage,    │
   └──────────────────┘  │   metrics, cache)  │
                         └───────────────────┘
```

### Aturan hexagonal yang tidak boleh dilanggar:

**Domain core tidak boleh tahu detail infrastruktur.**
Tidak ada import `database/sql`, driver DB, HTTP client, atau library eksternal
di dalam package domain (`domain/`, `service/`, `usecase/`).

**Komunikasi masuk ke domain hanya lewat Port (interface).**
HTTP handler, scheduler, dan CLI adalah *driving adapter* yang memanggil
use case lewat interface. Handler tidak boleh memanggil repository langsung.

**Komunikasi keluar dari domain hanya lewat Port (interface).**
Domain mendefinisikan interface (port), infrastruktur mengimplementasi.
Domain tidak pernah bergantung pada implementasi konkret.

**Empat port kategori wajib untuk setiap modul:**

```go
// 1. Repository port — didefinisikan di domain, diimplementasi di infra/db
type SuratMasukRepository interface {
    FindByID(ctx context.Context, id uuid.UUID) (*SuratMasuk, error)
    Save(ctx context.Context, s *SuratMasuk) error
    // ...
}

// 2. EventPublisher port — didefinisikan di domain, diimplementasi di infra/eventbus
type EventPublisher interface {
    Publish(ctx context.Context, event domain.Event) error
}

// 3. StoragePort — didefinisikan di domain, diimplementasi di infra/storage
type StoragePort interface {
    Upload(ctx context.Context, key string, r io.Reader, meta StorageMeta) error
    Download(ctx context.Context, key string) (io.ReadCloser, error)
    Delete(ctx context.Context, key string) error
}

// 4. MetricsPort — didefinisikan di domain, diimplementasi di infra/observability
type MetricsPort interface {
    RecordDuration(name string, d time.Duration, tags map[string]string)
    IncrCounter(name string, tags map[string]string)
    SetGauge(name string, v float64, tags map[string]string)
}
```

### Struktur direktori wajib per modul (hexagonal):

```
modules/{nama_modul}/
├── domain/                  # Inti — zero external dependency
│   ├── entity.go            # Struct domain, value object
│   ├── service.go           # Domain service (pure business logic)
│   ├── errors.go            # Domain-specific error types
│   └── ports.go             # Semua interface (repository, publisher, dll)
│
├── usecase/                 # Orchestrator — hanya bergantung pada domain/ports
│   ├── create_{entity}.go   # Satu file per use case
│   └── ...
│
├── adapter/
│   ├── http/                # Driving adapter
│   │   └── handler.go
│   ├── db/                  # Driven adapter — implementasi repository port
│   │   └── repository.go
│   └── event/               # Driven adapter — consumer event dari luar
│       └── consumer.go
│
├── manifest.go              # Registrasi modul ke framework
├── bootstrap.go             # Wiring DI: bind port ke adapter konkret
└── migrations/
    └── 001_create_{name}.sql
```

**`bootstrap.go`** adalah satu-satunya tempat di mana domain port di-bind
ke implementasi konkret. Tidak ada wiring DI di tempat lain.

```go
func (m *Module) Bootstrap(ctx context.Context, app *core.App) error {
    // Ambil driven adapters dari framework
    db     := app.DB()
    bus    := app.EventBus()
    store  := app.Storage()
    metric := app.Metrics()

    // Buat implementasi port
    repo := db_adapter.NewSuratMasukRepo(db)
    pub  := event_adapter.NewPublisher(bus)

    // Inject ke use case
    createUC := usecase.NewCreateSuratMasuk(repo, pub, metric)

    // Daftarkan handler ke gateway
    app.Router().POST("/surat-masuk", http_adapter.NewHandler(createUC).Create)
    return nil
}
```

---

## Struktur monorepo

```
pamong/
├── core/                          # Framework core — ubah hanya via ADR
│   ├── domain/                    # Engine: registry, schema, entity, hook
│   ├── workflow/                  # State machine, SLA, guard DSL, definition store (DB)
│   ├── strategy/                  # Strategy registry: daftar varian ber-key per titik
│   ├── rules/                     # Tiered constraint, versioning
│   ├── permission/                # RBAC+ABAC, hierarki OPD, delegasi
│   ├── config/                    # Tenant config ber-scope + resolver (paling spesifik menang)
│   ├── customization/             # Tenant customization layer (custom field, label, override)
│   ├── audit/                     # Immutable trail, hash chain, diff
│   ├── scheduler/                 # Cron, job queue, deadline-aware
│   └── notification/              # Channel abstraction
│
├── port/                          # Port (interface) lintas modul — READ-ONLY dari modul
│   ├── eventbus.go                # EventPublisher, EventSubscriber
│   ├── storage.go                 # StoragePort
│   ├── metrics.go                 # MetricsPort
│   ├── messaging.go               # MessagingPort (notifikasi async)
│   └── repository.go             # Base repository generics
│
├── infra/                         # Driven adapter implementasi port
│   ├── db/                        # Postgres adapter, query builder
│   ├── eventbus/                  # NATS / Redis Streams driver
│   ├── storage/                   # MinIO / S3-compat driver
│   ├── cache/                     # Redis cache adapter
│   ├── messaging/                 # WhatsApp, email, push adapter
│   └── observability/             # OTEL, Prometheus, Loki adapter
│
├── gateway/                       # Driving adapter: HTTP API
│   ├── middleware/                # Auth, rate limit, tenant resolver, CORS
│   ├── router/                    # Route aggregator dari semua modul
│   └── context.go                 # gateway.Context — carrier auth+tenant+trace
│
├── identity/                      # Modul user sentral (bukan tenant-local)
│   ├── domain/
│   ├── usecase/
│   ├── adapter/
│   └── sync/                      # Sinkronisasi user ke tenant (clone engine)
│
├── modules/                       # Modul bisnis pluggable
│   └── surat_masuk/               # Modul referensi — baca sebelum buat modul baru
│
├── ui/                            # Admin web (Frappe UI + Go adapter)
│   ├── src/
│   └── adapter/
│
├── tools/
│   ├── pamongctl/                    # CLI: scaffold, generate, lint, migrate
│   └── linter/                    # Custom Go analyzer
│
├── testkit/                       # Mock, fixture, helper testing
│
├── config/                        # Konfigurasi & konvensi (lihat bagian Konfigurasi)
│   ├── default.yaml
│   └── schema.go                  # Config struct + validation
│
└── docs/
    ├── adr/                       # Architecture Decision Records
    └── contracts/                 # API contract & event schema
```

---

## Konfigurasi & konvensi

### Format dan lokasi

Konfigurasi menggunakan YAML. Urutan precedence (tinggi ke rendah):

```
1. Environment variable          GOV_DB_HOST=...
2. File config lokal             config/local.yaml   (tidak di-commit)
3. File config environment       config/{env}.yaml   (staging.yaml, prod.yaml)
4. Default                       config/default.yaml (di-commit, nilai aman)
```

### Konvensi environment variable

Semua env var framework diawali prefix `GOV_`. Format: `GOV_{SECTION}_{KEY}`.

```bash
GOV_ENV=production
GOV_TENANT_ID=pemkot-surabaya         # hanya single-tenant / CLI; di server multi-tenant
                                      # tenant berasal dari request, bukan config (ADR-004)

# Default/SHARED koneksi tenant DB (ADR-004). BUKAN "satu tenant DB": host & nama DB
# per-tenant berasal dari id.tenant_registry saat runtime. Di sini hanya kredensial +
# pool bersama + host default Tier 1. GOV_DB_NAME cuma fallback single-tenant/dev.
GOV_DB_HOST=localhost                 # host default Tier 1 (shared); per-tenant dari registry
GOV_DB_PORT=5432
GOV_DB_NAME=pamong                    # fallback single-tenant/dev; per-tenant dari registry
GOV_DB_USER=govapp
GOV_DB_PASSWORD=...
GOV_DB_POOL_MAX=25
GOV_DB_POOL_IDLE=5

# Identity DB (sentral — koneksi PENUH; registry tenant hidup di sini, dibaca saat bootstrap)
GOV_IDENTITY_DB_HOST=...
GOV_IDENTITY_DB_PORT=5432
GOV_IDENTITY_DB_NAME=gov_identity
GOV_IDENTITY_DB_USER=...
GOV_IDENTITY_DB_PASSWORD=...
GOV_IDENTITY_DB_POOL_MAX=10
GOV_IDENTITY_DB_POOL_IDLE=2

# Provisioning tenant DB (ADR-006) — kredensial ADMIN ber-CREATEDB, TERPISAH dari GOV_DB_*
# (runtime, least-privilege). Dipakai HANYA oleh `pamongctl tenant provision`: connect ke
# GOV_PROVISION_DB_MAINTENANCE pada host target lalu CREATE DATABASE ... OWNER <GOV_DB_USER>.
# Host target & port dari registry + GOV_DB_PORT. Kosongkan jika tidak melakukan provisioning.
GOV_PROVISION_DB_USER=...             # role ber-CREATEDB
GOV_PROVISION_DB_PASSWORD=...
GOV_PROVISION_DB_MAINTENANCE=postgres # DB tempat connect saat CREATE DATABASE

# Event bus
GOV_EVENTBUS_DRIVER=nats             # nats | redis | memory (testing only)
GOV_EVENTBUS_URL=nats://localhost:4222
GOV_EVENTBUS_STREAM=pamong

# Storage
GOV_STORAGE_DRIVER=minio             # minio | s3 | local (testing only)
GOV_STORAGE_ENDPOINT=http://minio:9000
GOV_STORAGE_BUCKET=pamong
GOV_STORAGE_ACCESS_KEY=...
GOV_STORAGE_SECRET_KEY=...

# Cache
GOV_CACHE_DRIVER=redis               # redis | memory
GOV_CACHE_URL=redis://localhost:6379
GOV_CACHE_TTL_DEFAULT=300            # detik

# Observability
GOV_OTEL_ENDPOINT=http://otel-collector:4317
GOV_METRICS_ENABLED=true
GOV_TRACING_ENABLED=true
GOV_LOG_LEVEL=info                   # debug | info | warn | error
GOV_LOG_FORMAT=json                  # json | text (text hanya untuk dev)

# Auth
GOV_AUTH_JWKS_URL=https://sso.gov.example/jwks
GOV_AUTH_ISSUER=https://sso.gov.example
GOV_AUTH_AUDIENCE=pamong

# Rate limiting
GOV_RATELIMIT_ENABLED=true
GOV_RATELIMIT_RPS=100
GOV_RATELIMIT_BURST=20
```

### Struct konfigurasi

Config tidak dibaca langsung dari env di dalam modul. Selalu lewat struct yang
di-inject oleh framework saat bootstrap.

```go
// Modul menerima config lewat parameter Bootstrap, bukan os.Getenv
func (m *Module) Bootstrap(ctx context.Context, app *core.App) error {
    cfg := app.Config()          // *config.AppConfig — sudah diparse & divalidasi
    dbCfg := app.Config().DB     // field spesifik
    // ...
}
```

Modul bisnis tidak boleh membaca environment variable sendiri.
Jika modul butuh konfigurasi tambahan, deklarasikan di manifest:

```go
Config: domain.ConfigSpec{
    Fields: []domain.ConfigField{
        {Key: "max_lampiran_mb", Type: "int", Default: "10", Required: false},
    },
},
```

Nilai akan diambil dari `GOV_MODULE_{NAMA_MODUL}_{KEY}` atau dari UI admin tenant.

---

## Konvensi penamaan

### Go

| Konteks | Konvensi | Contoh |
|---|---|---|
| Package | `snake_case`, singular | `surat_masuk`, `unit_kerja` |
| Interface | `PascalCase`, tanpa prefix `I` | `Repository`, `EventPublisher` |
| Port interface | suffix `Port` jika lintas layer | `StoragePort`, `MetricsPort` |
| Use case struct | `{Verb}{Entity}` | `CreateSuratMasuk`, `ApprovePengajuan` |
| Struct domain | `PascalCase` | `SuratMasuk`, `NomorSurat` |
| Error var | `Err{Deskripsi}` | `ErrSuratTidakDitemukan` |
| Konstanta error code | `SCREAMING_SNAKE` | `ERR_PERMISSION_DENIED` |
| File use case | `{verb}_{entity}.go` | `create_surat_masuk.go` |
| File workflow seed | `{nama_alur}.yaml` | `disposisi.yaml` (baseline, di-load ke DB) |
| Strategy key | `{modul}.{titik}.{varian}` | `keuangan.persediaan.fifo` |
| Workflow template key | `{modul}.{alur}.{varian}` | `pengadaan.approval.tiga_tahap` |

### Database

**DB-per-tenant, schema-per-module.** Setiap tenant punya database sendiri (bukan schema
di DB bersama). Di dalam tenant DB, setiap modul punya schema Postgres sebagai namespace.

```
Postgres instance (atau beberapa instance untuk Tier 2/3)
├── db: gov_identity              ← sentral, satu-satunya yang shared
├── db: gov_pemkot_surabaya       ← tenant, fully isolated
│   ├── schema: gov               ← tabel framework (audit, workflow, config, fiscal)
│   ├── schema: penatausahaan     ← modul
│   │   └── spms, spps, sp2ds
│   ├── schema: kepegawaian
│   │   └── pegawais, jabatans
│   └── schema: aset
│       └── asets, penyusutans
└── db: gov_pemkot_malang         ← tenant lain, DB terpisah total

# Nama tabel: {schema}.{entity_plural}
penatausahaan.spms
penatausahaan.sp2ds
kepegawaian.pegawais
kepegawaian.jabatan_histories
aset.asets

# Tabel framework
gov.tenants
gov.modules
gov.permissions
gov.audit_logs
gov.workflow_definitions
gov.workflow_instances
gov.rule_versions
gov.tenant_configs
gov.tenant_customizations
gov.fiscal_periods
gov.migration_history

# Tabel identity sentral (DB terpisah: gov_identity)
id.persons
id.employments
id.credentials
id.central_roles
id.central_role_assignments
id.tenant_assignments
id.tenant_registry
```

**No-cross-schema-join rule:** modul tidak boleh JOIN tabel dari schema modul lain.
Butuh data modul lain → lewat port. Linter enforce dari nama schema di query.

### Event

Format nama event: `{modul}.{entity}.{kejadian_past_tense}`

```
surat_masuk.surat.diterima
surat_masuk.disposisi.dibuat
keuangan.spm.diterbitkan
keuangan.sp2d.dicairkan
kepegawaian.pegawai.dimutasi
identity.user.dibuat
identity.user.ditugaskan_ke_tenant
```

### Permission

Format: `{modul}:{entity}:{aksi}` — menggunakan titik dua sebagai pemisah
untuk membedakan dari nama event.

```
surat_masuk:surat:buat
surat_masuk:surat:baca
surat_masuk:surat:disposisi
keuangan:spm:terbitkan
keuangan:anggaran:ubah
kepegawaian:pegawai:mutasi
```

Permission dikelompokkan dalam `PermissionGroup` di manifest untuk kemudahan
assignment ke role:

```go
Permissions: domain.PermissionManifest{
    Groups: []domain.PermissionGroup{
        {
            Name:  "operator_surat",
            Label: "Operator Surat Masuk",
            Permissions: []domain.PermissionDef{
                {Name: "surat_masuk:surat:buat",  Label: "Buat surat masuk"},
                {Name: "surat_masuk:surat:baca",  Label: "Lihat surat masuk"},
            },
        },
        {
            Name:  "pimpinan_surat",
            Label: "Pimpinan (disposisi)",
            Permissions: []domain.PermissionDef{
                {Name: "surat_masuk:surat:baca",      Label: "Lihat surat masuk"},
                {Name: "surat_masuk:surat:disposisi", Label: "Buat disposisi"},
            },
        },
    },

    // Permission yang bisa diekspor untuk dipakai modul lain
    Exports: []string{
        "surat_masuk:surat:baca",  // modul lain bisa cek apakah user bisa baca surat
    },

    // Permission dari modul lain yang diimport
    Imports: []domain.PermissionImport{
        {From: "kepegawaian", Permission: "kepegawaian:jabatan:baca"},
    },
},
```

**Export/import permission antar modul:**
- Permission yang di-`Exports` dapat dirujuk oleh modul lain dalam guard workflow
  atau logic mereka, tanpa import Go package modul asal
- Permission yang di-`Imports` didaftarkan ke permission engine saat bootstrap,
  sehingga modul bisa check permission modul lain lewat `ctx.RequirePermission()`
- Linter akan reject jika modul menggunakan permission string dari modul lain
  yang tidak terdaftar di bagian `Imports`

---

## Identity & manajemen user

### Prinsip dasar

User (NIP/NIK) adalah entitas sentral yang tidak dimiliki oleh satu tenant.
Identity DB berdiri sendiri, terpisah dari semua tenant DB.

```
┌──────────────────────────────────────────────────────┐
│              Identity DB (sentral)                    │
│  id.persons · id.employments · id.credentials      │
│  id.central_roles · id.tenant_assignments           │
└────────────────────┬─────────────────────────────────┘
                     │  sync via event / clone engine
          ┌──────────┴──────────┐
    ┌─────▼──────┐        ┌─────▼──────┐
    │  Tenant A  │        │  Tenant B  │
    │  (Pemprov) │        │  (Pemkot)  │
    │ gov.user_profiles │        │ gov.user_profiles │
    │ gov.tenant_roles │        │ gov.tenant_roles │
    └────────────┘        └────────────┘
```

---

### Konsep inti: person, employment, persona

Identitas dimodelkan dalam tiga lapis yang harus dibedakan dengan tegas:

**Person** — satu manusia, satu baris `id.persons`. Anchor identitas adalah NIK,
karena setiap warga negara memiliki NIK. Ini adalah akar dari semua identitas.

**Employment** — relasi kepegawaian, OPSIONAL. Tidak semua person adalah pegawai.
- ASN → punya NIP (employment dengan `status = 'asn'`)
- Non-ASN (kontrak/PPPK) → tanpa NIP (employment dengan `status = 'non_asn'`)
- Masyarakat tanpa kepegawaian → tidak punya record employment sama sekali

**Persona / konteks akses** — bagaimana person mengakses sistem pada satu sesi:
- `citizen` — **selalu tersedia untuk semua person**. Setiap orang adalah masyarakat.
- `employee` — tersedia jika person punya employment aktif + tenant assignment.

**Prinsip kunci: ASN juga masyarakat.** Seorang ASN adalah person yang memiliki
employment, tapi tetap memiliki persona `citizen`. Person yang sama bisa:
- Login ke portal publik sebagai warga (persona `citizen`) menggunakan NIK/email/no HP
- Login ke sistem internal sebagai ASN (persona `employee`) menggunakan NIP

Keduanya menunjuk ke `id.persons` yang sama. Tidak ada duplikasi identitas.

### Credential & jalur login

Satu person bisa punya beberapa credential. Semua resolve ke person yang sama.

| Credential | Dipakai untuk persona | Keterangan |
|---|---|---|
| NIP | `employee` (internal) | Hanya person dengan employment ASN |
| NIK | `citizen` (publik) atau `employee` (non-ASN internal) | Anchor identitas |
| Email | `citizen` (publik) | Verifikasi OTP/password |
| No HP | `citizen` (publik) | Verifikasi OTP |

**Penentuan persona ditentukan oleh portal/jalur masuk, bukan oleh tipe orang:**
- Portal publik → selalu menghasilkan persona `citizen`, untuk siapapun
- Portal internal → menghasilkan persona `employee`, hanya jika person punya
  employment aktif dan tenant assignment. Jika tidak, akses ditolak.

Tidak ada lagi konsep `user_type` pada level person. Yang ada adalah employment
(opsional) dan persona (ditentukan saat login).

---

### Model role — dua lapisan

Role dipisah tegas antara **sentral** (disimpan di identity DB) dan **tenant**
(disimpan di tenant DB). Keduanya tidak saling tahu secara langsung, komunikasi
lewat event dan token claim.

#### Lapisan 1 — Role sentral (identity DB)

Dikelola oleh admin platform / kementerian. Ada dua sub-tipe:

**Global role** — berlaku di semua tenant tanpa terkecuali:
```
super_admin          — akses penuh seluruh sistem dan semua tenant
platform_helpdesk    — read-only lintas semua tenant untuk support
platform_auditor     — akses audit log lintas tenant
```

**Scoped role** — berlaku hanya di tenant yang di-scope-kan, diassign bersama
`tenant_scope` (bisa satu tenant, bisa array, bisa wildcard per provinsi):
```
regional_helpdesk    — helpdesk kementerian untuk Jatim saja
regional_supervisor  — supervisor yang bisa monitor beberapa kab/kota
```

#### Lapisan 2 — Role tenant (tenant DB)

Dikelola oleh admin tenant (Sekda, BKD, dll). Hanya berlaku di dalam tenant tersebut.
Tidak dikenal di luar tenant. Nama role bebas sesuai kebutuhan OPD, tapi mengikuti
konvensi `snake_case`:
```
bendahara_pengeluaran
ppk_opd
verifikator_keuangan
operator_surat
pimpinan_opd
admin_kepegawaian
```

#### Kombinasi sentral + tenant

Seorang user bisa punya role sentral **dan** role tenant secara bersamaan.
Contoh: helpdesk kementerian yang scoped ke Jatim, sekaligus ditetapkan sebagai
verifikator di Pemkot Surabaya.

Aturan prioritas saat konflik permission:
- Role sentral **global** selalu menang atas role apapun
- Role sentral **scoped** setara dengan role tenant di tenant yang di-scope
- Jika ada konflik antar role tenant, permission yang lebih permisif yang berlaku
  (union, bukan intersection) — kecuali permission yang di-mark `strict: true`

---

### Skema database identity (schema id, , DB terpisah)

```sql
-- Master identitas — satu baris per manusia, anchor di NIK
CREATE TABLE id.persons (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    nik          VARCHAR(16) UNIQUE NOT NULL,  -- anchor global, setiap orang punya
    nama_lengkap VARCHAR(255) NOT NULL,
    tgl_lahir    DATE,
    no_hp        VARCHAR(15),
    email        VARCHAR(255),
    is_active    BOOLEAN NOT NULL DEFAULT true,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Employment — relasi kepegawaian, opsional, bisa lebih dari satu sepanjang waktu
CREATE TABLE id.employments (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    person_id    UUID NOT NULL REFERENCES id.persons(id),
    status       VARCHAR(10) NOT NULL CHECK (status IN ('asn','non_asn')),
    nip          VARCHAR(18) UNIQUE,           -- wajib jika status = 'asn', NULL jika non_asn
    instansi_asal VARCHAR(255),                -- instansi induk pegawai
    is_active    BOOLEAN NOT NULL DEFAULT true,
    valid_from   TIMESTAMPTZ NOT NULL DEFAULT now(),
    valid_until  TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((status = 'asn' AND nip IS NOT NULL) OR (status = 'non_asn' AND nip IS NULL))
);

-- Credential login — banyak per person, semua resolve ke person yang sama
CREATE TABLE id.credentials (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    person_id    UUID NOT NULL REFERENCES id.persons(id),
    cred_type    VARCHAR(20) NOT NULL,         -- 'nip','nik','email','no_hp','oauth'
    cred_value   VARCHAR(255) NOT NULL,        -- identifier (NIP/NIK/email/no_hp)
    secret_hash  VARCHAR(255),                -- bcrypt hash, NULL jika SSO/OTP-only
    is_primary   BOOLEAN NOT NULL DEFAULT false,
    last_used_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (cred_type, cred_value)
);

-- Role sentral — global dan scoped
CREATE TABLE id.central_roles (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name         VARCHAR(100) UNIQUE NOT NULL, -- 'super_admin', 'regional_helpdesk'
    label        VARCHAR(255) NOT NULL,
    scope_type   VARCHAR(10) NOT NULL CHECK (scope_type IN ('global','scoped')),
    description  TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Assignment role sentral ke person
CREATE TABLE id.central_role_assignments (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    person_id       UUID NOT NULL REFERENCES id.persons(id),
    role_id         UUID NOT NULL REFERENCES id.central_roles(id),
    -- untuk scoped role: array tenant_id yang berlaku, NULL berarti global
    tenant_scope    VARCHAR(100)[],
    assigned_by     UUID NOT NULL REFERENCES id.persons(id),
    valid_from      TIMESTAMPTZ NOT NULL DEFAULT now(),
    valid_until     TIMESTAMPTZ,              -- NULL = permanen
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Assignment employment ke tenant (persona employee). Citizen TIDAK perlu baris ini.
-- Cross-tenant: satu employment ditugaskan ke tenant berbeda dari instansi asalnya.
CREATE TABLE id.tenant_assignments (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    employment_id   UUID NOT NULL REFERENCES id.employments(id),
    tenant_id       VARCHAR(100) NOT NULL,
    is_home_tenant  BOOLEAN NOT NULL DEFAULT true,   -- false = penugasan cross-tenant
    assigned_by     UUID NOT NULL REFERENCES id.persons(id),
    -- assigned_by wajib punya permission identity:assignment:cross_tenant
    -- jika is_home_tenant = false (contoh: PJ Bupati dari Pemprov)
    valid_from      TIMESTAMPTZ NOT NULL DEFAULT now(),
    valid_until     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (employment_id, tenant_id, valid_from)
);
```

Catatan penting: tenant assignment menempel pada **employment**, bukan langsung pada
person. Persona `citizen` tidak memerlukan tenant assignment — akses publik tersedia
untuk semua person tanpa kaitan tenant. Hanya persona `employee` yang butuh assignment.

### Skema tabel clone di tenant DB (schema gov, )

```sql
-- Clone read-only dari id.persons + employment, di-sync via event.
-- Hanya person dengan persona employee (punya tenant assignment) yang di-clone ke sini.
CREATE TABLE gov.user_profiles (
    id              UUID PRIMARY KEY,   -- sama persis dengan id.persons.id
    person_id       UUID NOT NULL,      -- = id (eksplisit untuk kejelasan)
    employment_status VARCHAR(10) NOT NULL, -- 'asn' | 'non_asn'
    nip             VARCHAR(18),
    nik             VARCHAR(16) NOT NULL,
    nama_lengkap    VARCHAR(255) NOT NULL,
    assignment_id   UUID NOT NULL,      -- referensi ke id.tenant_assignments
    is_cross_tenant BOOLEAN NOT NULL DEFAULT false,
    synced_at       TIMESTAMPTZ NOT NULL,
    -- Kolom tambahan spesifik tenant — boleh ditambah modul kepegawaian
    jabatan_lokal   VARCHAR(255),
    unit_kerja_id   UUID
    -- JANGAN tambah kolom credential atau password di sini
);

-- Role tenant — dikelola admin tenant, tidak ada di identity DB
CREATE TABLE gov.tenant_roles (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        VARCHAR(100) NOT NULL,   -- 'bendahara_pengeluaran', 'ppk_opd'
    label       VARCHAR(255) NOT NULL,
    description TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (name)
);

-- Assignment role tenant ke user (hanya berlaku di tenant ini)
CREATE TABLE gov.user_role_assignments (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES gov.user_profiles(id),
    role_id     UUID NOT NULL REFERENCES gov.tenant_roles(id),
    unit_kerja_id UUID,                  -- scope ke unit kerja tertentu, NULL = seluruh tenant
    assigned_by UUID NOT NULL,
    valid_from  TIMESTAMPTZ NOT NULL DEFAULT now(),
    valid_until TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

---

### Auth flow & login

Persona ditentukan oleh portal tempat login, bukan oleh tipe orang.
Satu person yang sama bisa melewati alur internal maupun publik.

#### Alur 1 — Persona employee, user sentral / kementerian

```
[Login via portal sentral atau via app — menggunakan NIP]
        │
        ▼
[Identity service: verifikasi credential → resolve person + employment]
        │
        ▼
[Cek central_role_assignments person ini]
   Issue token sementara berisi: person_id, persona=employee,
   central_roles[] (global), scoped_roles[] + tenant_scope[]
        │
        ├── Via portal sentral:
        │       Tampilkan tenant dalam scope → pilih tenant → pilih app/modul
        │       (atau urutan dibalik: pilih app dulu, lalu tenant)
        │       → Issue scoped token dengan tenant_id
        │
        └── Via app/modul langsung:
                Tampilkan tenant dalam scope → pilih tenant
                → Issue scoped token dengan tenant_id
```

#### Alur 2 — Persona employee, user daerah (ASN / non-ASN)

```
[Login via portal tenant atau via app — NIP (ASN) atau NIK (non-ASN)]
        │
        ▼
[Identity service: verifikasi → resolve person + employment]
   Tolak jika tidak ada employment aktif (orang biasa tidak bisa masuk internal)
        │
        ▼
[Cek tenant_assignments untuk employment ini]
        │
        ├── Hanya 1 tenant → langsung issue token dengan tenant_id
        │
        └── >1 tenant (ada cross-tenant assignment, mis. PJ Bupati) →
                Tampilkan daftar tenant → pilih → issue scoped token
        │
        ▼
[Token: person_id, persona=employee, tenant_id, tenant_roles[],
        scoped_central_roles[]]
        │
        ▼
[Tampilkan daftar app/modul yang diizinkan di tenant tersebut]
```

#### Alur 3 — Persona citizen (portal publik, untuk SIAPAPUN termasuk ASN)

```
[Portal publik — TERPISAH dari sistem internal]
        │
        ▼
[Login dengan NIK / no HP / email]
   Berlaku untuk semua person, termasuk ASN yang sedang mengakses
   sebagai warga (bukan dalam kapasitas kepegawaian)
        │
        ▼
[Identity service: resolve person → verifikasi OTP/password]
   TIDAK mengecek employment — persona citizen tidak butuh kepegawaian
        │
        ▼
[Token: person_id, persona=citizen — permission scope publik saja]
        │
        ▼
[Modul publik — berinteraksi ke modul internal via service port]
   Tidak ada akses langsung ke DB internal.
   Interaksi lewat API yang di-expose modul internal sebagai port publik.
```

Penting: persona `citizen` adalah konteks akses, bukan tipe orang. ASN yang login
ke portal publik mendapat token `persona=citizen` dan diperlakukan sebagai warga —
tanpa role kepegawaiannya. Pemisahan ini mencegah kebocoran wewenang internal
ke ranah layanan publik.

#### Struktur JWT token

```json
{
  "sub": "uuid-person-id",
  "persona": "employee",                 // "employee" | "citizen"
  "employment_status": "asn",            // null jika persona = citizen
  "tenant_id": "pemkot-surabaya",        // null jika persona citizen atau belum pilih tenant
  "central_roles": ["platform_helpdesk"],
  "tenant_roles": ["verifikator_keuangan", "operator_surat"],
  "tenant_scope": ["pemkot-surabaya"],   // untuk scoped central role
  "is_cross_tenant": false,
  "iat": 1700000000,
  "exp": 1700003600,
  "jti": "uuid-token-id"                 // untuk revocation
}
```

Token tidak memuat password, NIK lengkap, atau data sensitif lain.
Modul bisnis membaca claim dari `gateway.Context`, tidak pernah decode token sendiri.

---

### Event sinkronisasi identity

```
identity.person.dibuat              → master person baru di identity DB
identity.person.diperbarui          → update gov.user_profiles di semua tenant aktif
identity.employment.dibuat          → person ini kini punya kepegawaian
identity.employment.ditugaskan      → buat gov.user_profiles di tenant tujuan
identity.employment.dicabut         → nonaktifkan gov.user_profiles di tenant
identity.central_role.diassign      → update token claim pada next login
identity.central_role.dicabut       → revoke token aktif person tersebut
```

Event sinkronisasi diproses oleh `identity/sync/` engine, bukan oleh modul bisnis.
Modul bisnis hanya subscribe event yang relevan untuk domainnya sendiri (misalnya
modul kepegawaian subscribe `identity.person.diperbarui` untuk update data jabatan).

---

### Port yang disediakan framework untuk modul bisnis

```go
// Modul bisnis mengakses data user lewat port ini — tidak boleh query gov.user_profiles langsung
type UserResolver interface {
    ResolveByID(ctx context.Context, personID uuid.UUID) (*UserProfile, error)
    ResolveByNIP(ctx context.Context, nip string) (*UserProfile, error)
    ResolveByNIK(ctx context.Context, nik string) (*UserProfile, error)
    IsCrossTenant(ctx context.Context, personID uuid.UUID) (bool, error)
    HasCentralRole(ctx context.Context, personID uuid.UUID, role string) (bool, error)
}

// Untuk cek role/permission di use case — biasanya cukup lewat gateway.Context
type AuthContext interface {
    PersonID() uuid.UUID
    Persona() string                           // "employee" | "citizen"
    EmploymentStatus() string                  // "asn" | "non_asn" | "" (citizen)
    TenantID() string                          // "" jika persona citizen
    HasRole(role string) bool                  // cek tenant role ATAU central role
    HasCentralRole(role string) bool           // cek central role saja
    RequirePermission(perm string) error
    IsCitizen() bool                           // shortcut: Persona() == "citizen"
    IsCrossTenant() bool
}
```

---

### Aturan pengembangan terkait identity

- Modul bisnis **tidak boleh** query tabel di identity DB (schema `id.`) (identity DB) langsung
- Modul bisnis **tidak boleh** query `gov.user_profiles` langsung — gunakan `UserResolver` port
- Modul bisnis **tidak boleh** decode JWT sendiri — gunakan `gateway.Context`
- Modul bisnis **tidak boleh** mengasumsikan persona tertentu — selalu cek `IsCitizen()`
  bila modul melayani baik internal maupun publik
- Perubahan data inti person (NIK, nama) atau employment (NIP) hanya boleh oleh modul `identity/`
- Penambahan role tenant dilakukan lewat UI admin tenant — bukan lewat migration modul bisnis
- Role sentral baru hanya bisa ditambah admin platform lewat UI admin sentral atau migration `identity/`
- Modul publik yang butuh data internal **wajib** lewat service port yang di-expose modul internal,
  tidak boleh akses lintas-DB atau lintas-modul langsung
- Linter akan reject jika ada import package `identity/` dari modul bisnis manapun
  (akses hanya lewat port dan event)

---

## Use case vs workflow

Dua konsep ini sering tertukar. Membedakannya dengan tegas adalah fondasi
fleksibilitas framework. Aturan pembeda: **bila sesuatu bisa "menunggu" aksi
orang lain, itu workflow; bila selalu selesai dalam satu request, itu use case.**

| Aspek | Use case | Workflow |
|---|---|---|
| Level | Satu transaksi atomik | Orkestrasi banyak transaksi |
| Durasi | Milidetik, sinkron | Bisa berhari-hari, bertahan |
| Aktor | Satu pemanggil | Banyak aktor lintas waktu |
| State | Tidak menyimpan state alur | State tersimpan & ber-versi |
| Wujud | Kode Go (`usecase/`) | Data (DB) + seed YAML |
| Berubah lewat | Deploy + review | Konfigurasi per-tenant |
| Contoh | "Terbitkan SPM" | "Alur persetujuan SPM 3 tahap" |

Yang berubah pada use case adalah *apa yang terjadi di dalam satu langkah*
(itu strategy bila punya varian — lihat bawah). Yang berubah pada workflow adalah
*langkah apa saja, urutannya, syaratnya, dan notifikasinya*.

Contoh konkret beda tenant pada alur yang sama:
```
Tenant A:  diajukan → validasi_x → validasi_y → selesai (notify: pengaju)
Tenant B:  diajukan → validasi_x → validasi_y → pengesahan_z → selesai
                                                 (notify: pengaju, x, y)
```
Kedua tenant menjalankan use case `validasiPengajuan` dan `sahkanPengajuan` yang
identik. Yang berbeda hanya definisi workflow-nya. Tidak ada business logic yang
digandakan.

---

## Fleksibilitas & titik ekstensi

Framework ini menyediakan fleksibilitas terkontrol lewat **dua mekanisme berbeda**
untuk dua jenis kebutuhan yang berbeda. Keduanya configurable dan berkonstrain ketat,
tapi caranya beda karena sifat yang dikonfigurasi beda.

| Jenis fleksibilitas | Contoh | Disimpan di DB sebagai | Dieksekusi sebagai |
|---|---|---|---|
| Pemilihan algoritma/kebijakan | FIFO vs average, aset vs beban | identifier (`"fifo"`) | strategy Go ter-registry |
| Susunan & syarat langkah | alur approval A vs B | definisi workflow | workflow engine |
| Kondisi boolean | guard transisi, batas anggaran | expression string | DSL boolean tanpa side-effect |

Prinsip yang mendasari semuanya: **tidak ada satu baris logika yang dieksekusi
tersimpan sebagai kode di database.** Yang di DB hanyalah pilihan (identifier),
susunan (struktur), atau kondisi (boolean expression). Logika sesungguhnya selalu
kode Go yang ter-compile, ter-test, dan ter-review. Ini keputusan keamanan sadar
untuk konteks pemerintahan — menutup vektor "kode arbitrary di DB" sepenuhnya.

### Mekanisme 1 — Selectable Strategy (registry pattern)

Untuk titik keputusan yang punya beberapa varian algoritma/kebijakan sah.

Cara kerja: developer menulis interface (port) untuk titik keputusan, lalu beberapa
implementasi, masing-masing didaftarkan ke registry dengan key. Tenant menyimpan
hanya key pilihannya di config. Use case menanyakan ke registry strategy mana yang
aktif untuk tenant ini, lalu menjalankannya.

```
MetodePersediaan (port/interface)
├── "fifo"      → implementasi Go, ter-test
├── "lifo"      → implementasi Go, ter-test
└── "average"   → implementasi Go, ter-test

Config tenant: { "modul.persediaan.metode": "fifo" }   ← hanya key, bukan logika
```

Constraint yang melekat:
- Tenant hanya bisa memilih dari key yang terdaftar. Key tak dikenal → ditolak.
- Opsi yang tersedia untuk tenant = irisan:
  `ditulis developer ∩ diizinkan rule tier nasional ∩ diizinkan tier provinsi`.
  Contoh: bila rule nasional melarang LIFO, LIFO tak muncul sebagai opsi meski
  implementasinya ada.
- Mengubah pilihan = aksi ber-permission + ter-audit + ber-versi dengan effective
  date. Perubahan metode TIDAK retroaktif — berlaku untuk periode baru, tidak
  mengubah data periode yang sudah ditutup.
- Registry idealnya memvalidasi **koherensi kombinasi** (bukan hanya "key ada"),
  karena beberapa pilihan akuntansi saling bergantung. Lihat titik ekstensi #5.

Scope pilihan dirancang agar bisa berkembang: kunci config tidak diasumsikan selalu
`tenant_id → key`. Struktur key dibuat cukup kaya untuk mendukung scope masa depan
(per-unit-kerja, per-gudang, per-transaksi) tanpa mengubah skema — lihat titik
ekstensi #2.

### Mekanisme 2 — Workflow as data (template-based)

Untuk susunan langkah, urutan, syarat transisi, SLA, dan notifikasi yang berbeda
antar tenant.

Keputusan desain yang dipilih untuk framework ini:
- Definisi workflow disimpan sebagai **data di database**, bukan file kode. Ini
  yang membuatnya benar-benar changeable per-tenant tanpa redeploy.
- File YAML dalam modul berperan sebagai **seed/baseline** — tervalidasi,
  ter-version-control di Git, dikirim bersama modul. Saat bootstrap, seed di-load
  ke DB. Yang di Git jadi default; yang di DB jadi yang aktif & bisa di-override.
- **Tahap sekarang (hanya developer/admin platform yang menulis):** workflow
  disediakan sebagai **template ber-key**. Developer menulis beberapa template
  lengkap; tenant memilih salah satu (persis seperti strategy). Yang disimpan di
  config tenant: key template + parameter binding (siapa konkret "pejabat X" di
  tenant itu — yang sebenarnya hidup di layer permission, bukan di workflow).
- **Tahap masa depan (pemda menulis sendiri, dengan otorisasi):** menggunakan
  **engine yang sama**, hanya berbeda siapa penulisnya dan seberapa ketat gerbang
  validasinya. Template = workflow yang kebetulan ditulis developer; workflow tenant
  = workflow yang ditulis pemda. Tidak perlu mesin baru — lihat titik ekstensi #3.

Setiap definisi workflow di DB:
- Divalidasi terhadap schema yang sama dengan seed (syntax & struktur) di pintu masuk.
- Disimpan ber-versi dengan effective date; bisa rollback; tercatat siapa mengubah
  kapan dari apa ke apa.
- Perubahan definisi workflow itu sendiri adalah aksi ber-permission yang melewati
  workflow persetujuannya sendiri (bila dikonfigurasi demikian).

### Mekanisme 3 — Expression DSL (guard & kondisi)

Untuk kondisi boolean: boleh-tidaknya transisi, terpenuhi-tidaknya constraint rule.

- DSL sempit tanpa side-effect: hanya membaca konteks (actor, entity, tenant),
  mengevaluasi jadi boolean atau nilai. Tidak bisa akses sistem, tidak bisa loop
  liar, tidak bisa memutasi, hasilnya deterministik.
- Di-compile sekali saat workflow/rule di-load → syntax error ketahuan di pintu
  masuk, bukan saat runtime. Guard wajib menghasilkan boolean (divalidasi tipe).
- DSL bukan tempat algoritma. Bila guard butuh logika lebih rumit dari perbandingan
  + boolean + akses field, itu sinyal logikanya harus pindah ke use case (Go).
- Konservatif secara sengaja: tidak ada fungsi custom yang didefinisikan admin.
  Untuk pemerintahan, auditabilitas > ekspresivitas.

---

## Titik ekstensi (open for extension, closed for modification)

Framework dirancang agar penambahan kemampuan ke depan **tidak menyentuh kode yang
sudah ada**. Berikut pola yang sudah dibangun masuk sejak awal. Semua mengikuti satu
ide: menambah = mendaftarkan sesuatu yang baru, bukan mengubah yang lama.

**#1 — Registry pattern (seragam untuk banyak hal).**
Strategy, workflow template, notification channel, rule evaluator, storage driver,
event bus driver — semuanya memakai bentuk yang sama: sebuah interface + registry
ber-key + fungsi `Register()`. Menambah varian baru = tulis implementasi + daftarkan
satu baris. Kode pemanggil tidak berubah karena ia bergantung pada interface, bukan
implementasi konkret. Ini penerapan Open/Closed Principle secara konsisten.

**#2 — Config ber-scope yang bisa diperdalam.**
Kunci konfigurasi tenant tidak di-hardcode sebagai `tenant_id → value`. Ia memakai
struktur scope bertingkat (`tenant_id[/unit_kerja_id[/resource_id]]`) dengan resolusi
"paling spesifik menang". Saat ini hampir semua scope = tenant; tapi skema sudah siap
untuk per-unit-kerja, per-gudang, atau per-transaksi tanpa migrasi. Use case cukup
menanyakan "config untuk konteks ini", resolver yang menentukan nilai mana yang berlaku.

**#3 — Satu workflow engine, dua sumber penulis.**
Engine tidak membedakan workflow yang ditulis developer (template) dari yang ditulis
pemda. Yang berbeda hanya `authoring_source` dan gerbang validasi yang dilewati saat
disimpan. Saat fitur "pemda menulis workflow" diaktifkan nanti, yang ditambah hanya
validator yang lebih ketat + UI editor — engine eksekusi tidak berubah.

**#4 — Tenant customization layer terpisah dari module layer.**
Kustomisasi tenant (custom field, pilihan strategy, pilihan/override workflow, label,
tampilan) hidup di layer terpisah dari definisi modul inti. Upgrade framework tidak
menimpa kustomisasi tenant; kustomisasi tenant tidak mengotori modul inti. Pola ini
(dipelajari dari Custom Field ERPNext) wajib ada sejak awal karena permintaan
"tambah field" / "ubah label" dari pemda pasti datang dan tidak boleh memerlukan
perubahan kode modul.

**#5 — Hook validasi koherensi (combination validator).**
Selain validasi "key terdaftar", registry strategy menyediakan titik untuk
mendaftarkan validator kombinasi lintas-pilihan (mis. pendekatan beban + metode
tertentu = tidak koheren). Belum tentu dipakai di awal, tapi titiknya sudah ada
sehingga menambah aturan koherensi nanti tidak mengubah alur pemilihan.

**#6 — Capability flags.**
Fitur masa depan di-gate lewat capability flag per-tenant, bukan lewat percabangan
kode yang menyebar. Fitur yang belum matang bisa hadir di kode (dormant) dan
diaktifkan per-tenant tanpa rilis terpisah. Mencegah long-lived feature branch.

**#7 — Versioned config dengan effective date (pola berulang).**
Rule, strategy choice, dan workflow definition semua memakai pola yang sama:
nilai berlaku per rentang tanggal, ada riwayat, bisa rollback, non-retroaktif untuk
periode terkunci. Karena polanya seragam, komponen baru yang butuh "berubah seiring
waktu dengan audit" tinggal memakai pola yang sama, bukan membuat mekanisme sendiri.

Aturan untuk Claude Code & developer: bila sebuah kebutuhan baru menggoda untuk
"mengubah kode yang sudah ada", periksa dulu apakah ia bisa diekspresikan sebagai
*registrasi* pada salah satu titik ekstensi di atas. Mengubah kode lama hanya
dibenarkan bila tidak ada titik ekstensi yang cocok — dan bila demikian, itu sinyal
perlu ADR untuk menambah titik ekstensi baru, bukan menambal ad-hoc.

---

## Entity tiers — progressive eject

Tidak semua entity butuh jalur hexagonal lengkap. Framework menyediakan tiga tier
yang bisa di-"eject" secara incremental: mulai sederhana, naik kompleksitas saat perlu.

**Tier 1 — Generated penuh, zero Go code.** Untuk data master dan entity sederhana.
Didefinisikan via YAML (atau UI admin nanti), framework handle semuanya: tabel DB,
endpoint CRUD, form UI, permission, audit, validasi deklaratif, search, filter,
pagination, soft delete. Tidak ada file Go yang ditulis developer.

**Tier 2 — Generated + hook.** Butuh validasi/logika ringan di atas CRUD. Developer
menjalankan `pamongctl eject hooks --entity=X`, dapat SATU file hook — CRUD tetap
generated.

**Tier 3 — Full hexagonal.** Untuk entity dengan business logic berat (SPM, SPP).
Developer menjalankan `pamongctl eject usecase --entity=X`, dapat struktur hexagonal
lengkap. YAML tetap jadi source of truth untuk schema.

YAML selalu source of truth untuk field/schema — di tier manapun. Eject menambah,
tidak mengganti. Setiap eject satu arah, tidak bisa di-un-eject.

```bash
# Semua entity mulai dari sini — YAML saja
pamongctl define entity --module=penatausahaan --name=JenisBelanja

# Butuh hook? Eject hook saja
pamongctl eject hooks --entity=penatausahaan.JenisBelanja

# Butuh full use case? Eject use case
pamongctl eject usecase --entity=penatausahaan.SPM
```

Proporsi realistis: ~60-70% Tier 1, ~15-20% Tier 2, ~10-15% Tier 3.

---

## Modular monolith & module packaging

Framework adalah **modular monolith**: satu binary, tapi module boundaries sekuat
service boundaries. Modul bukan plugin — ia di-compile bersama binary utama.

```go
// cmd/server/main.go — satu-satunya tempat modul "dipasang"
func main() {
    app := pamong.New(config.Load())
    app.Register(
        kepegawaian.Module{},
        persuratan.Module{},
    )
    app.Run()
}
```

Modul aktif/nonaktif per-tenant lewat capability flag — bukan unload binary.
Modul yang tidak aktif: route tidak ter-register, menu tidak muncul, event tidak
diproses.

**No-cross-module-join:** modul tidak boleh JOIN tabel dari schema modul lain dalam
satu query. Butuh data modul lain → lewat port. Kalau query butuh data dari dua
modul, itu sinyal: pakai port, atau buat read projection lewat event.

**Dependency antar modul harus DAG (directed acyclic graph).** Divalidasi dari
`DependsOn` di manifest oleh `pamongctl validate modules`. Siklus → reject.
Cara memutus siklus: tentukan modul yang "lebih rendah" (tidak import siapapun
di atas), arah balik selalu event + read projection.

Kesiapan microservice: hexagonal + event bus + no-cross-import + no-cross-join
sudah memberikan ~80% kesiapan. Yang belum ada: saga orchestrator dan data
replication. Dibangun kalau dan saat ada modul yang perlu di-extract.

---

## Data integrity — konvensi framework

Tiga mekanisme ini WAJIB disediakan framework, bukan diserahkan ke developer modul:

**Idempotency.** Setiap use case yang memutasi data menerima idempotency key
dari caller. Framework menyimpan key + response di tabel `gov.idempotency_keys`
(TTL 24 jam). Request duplikat mendapat response yang sama tanpa efek samping.
Developer modul tidak menulis logika idempotency — framework yang handle di
middleware level.

**Optimistic locking.** Setiap entity punya field `version` (integer, auto-increment
saat update). Framework auto-check `WHERE version = expected_version` pada setiap
update. Conflict → `ErrConflict` dengan detail siapa yang mengubah lebih dulu.
Developer modul tidak menulis logika locking.

**Bulk operations.** Framework menyediakan endpoint batch standar untuk approval,
update status, dan operasi lain yang sering dilakukan massal. Setiap item dalam
batch diproses lewat use case yang sama (menjaga validasi & permission), tapi
dalam satu DB transaction dengan partial-success reporting.

---

## Data lifecycle — annual cutoff vs continuous

Modul mendeklarasikan lifecycle datanya di manifest. Framework menyediakan mekanisme
untuk keduanya:

**annual_cutoff** — data dipecah per tahun fiskal (schema per tahun di dalam
tenant DB). Schema tahun lama menjadi read-only setelah hard-close. Modul wajib
mendefinisikan `CarryForwardSpec` (data apa yang dibawa ke tahun baru) dan
`AggregationSpec` (data apa yang dirangkum ke warehouse).

**continuous** — data di satu schema, semua tahun. Performa dijaga lewat Postgres
table partitioning by tahun. Query tahun berjalan cepat (satu partisi), query
lintas tahun tetap bisa.

Decision framework — tiga pertanyaan:
1. Apakah data periode lama masih dimutasi setelah tutup buku? Ya → continuous.
2. Apakah transaksi berjalan butuh data lama saat proses? Ya → continuous.
3. Apakah kehilangan akses cepat ke data lama berdampak operasional? Ya → continuous.

```yaml
# Di manifest modul
data_lifecycle: annual_cutoff    # atau: continuous
carry_forward:
  - entity: SisaPagu
    query: "SELECT rekening_id, sisa FROM pagu WHERE sisa > 0"
    target: pagu_awal
aggregation:
  - entity: RealisasiBulanan
    query: "SELECT bulan, sum(nilai) ..."
    target: warehouse.realisasi_tahunan
```

---

## Tenant tier & portabilitas

Tenant bisa "naik level" tanpa mengubah kode aplikasi:

**Tier 1 — Shared server, DB per-tenant.** Default untuk onboarding. Semua tenant
DB di satu Postgres instance. Isolasi sudah penuh karena DB terpisah.

**Tier 2 — Dedicated DB server.** Tenant dipindah ke Postgres instance sendiri
(masih satu mesin fisik atau VM terpisah).

**Tier 3 — Dedicated server/VPS.** Postgres di server milik pemda sendiri.

Kode aplikasi tidak berubah — tenant resolver membaca `tenant_registry` di identity
DB untuk menentukan di mana DB tenant berada. Proses naik tier:
`pg_dump → transfer → pg_restore → update registry → verify`.

Identity DB selalu sentral, tidak pernah ikut tenant. Data identity di-clone ke
tenant via event.

---

## Migration strategy

**Definisi migrasi hidup di kode modul** — `modules/{modul}/migrations/`.

**Tracking di dua tempat:**
- Tenant DB (`gov.migration_history`) — sumber kebenaran apa yang sudah jalan.
- Identity DB (`id.tenant_migrations`) — monitoring lintas tenant oleh admin platform.

**Backward-compatible migration wajib:** kode baru harus jalan di schema lama &
baru selama window migrasi. Breaking change butuh dua rilis.

**Rollout per-tenant:** canary dulu, batch sisanya, throttled, rollback per-tenant
independen.

---

## Data migration pipeline (legacy import)

Import data dari sistem lama mengikuti pipeline bertahap:

```
File mentah (xlsx/csv) → Staging table (data apa adanya)
    → Mapping & transform (aturan konversi per-pemda, sebagai config)
    → Validasi (referensi, format, duplikat)
    → Dry-run report (preview, belum commit)
    → Commit ke production (dengan audit: "sumber: migrasi sistem X")
```

Staging table terpisah dari tabel production. Mapping rules sebagai konfigurasi
per-pemda, bukan kode. Dry-run wajib sebelum commit.

---

## Aturan pengembangan — WAJIB diikuti

### 1. Mulai dari manifest, bukan dari kode

File pertama di modul baru adalah `manifest.go`. Jangan tulis `service.go`
atau `handler.go` sebelum manifest selesai dan `pamongctl validate module` lulus.

```go
type Module interface {
    Manifest() domain.Manifest
    Bootstrap(ctx context.Context, app *core.App) error
}
```

### 2. Handler hanya boleh parse, delegate, respond

```go
// DILARANG di adapter/http/handler.go:
db.Query(...)
repo.FindByID(...)

// BENAR — handler hanya orchestrate, use case yang bekerja:
func (h *Handler) Create(ctx *gateway.Context) error {
    if err := ctx.RequirePermission("surat_masuk:surat:buat"); err != nil {
        return err
    }
    var input usecase.CreateInput
    if err := ctx.Bind(&input); err != nil {
        return core.ErrValidation("body", err.Error())
    }
    result, err := h.createUC.Execute(ctx, input)
    if err != nil {
        return err
    }
    return ctx.JSON(http.StatusCreated, result)
}
```

### 3. Permission check wajib di baris pertama handler

Sebelum `Bind`, sebelum log, sebelum apapun.

### 4. Event publish hanya lewat port terdaftar

```go
// DILARANG — string arbitrary, tidak ada di manifest:
bus.Publish(ctx, "surat.sesuatu", map[string]any{...})

// BENAR — menggunakan const dari events.go modul sendiri:
h.publisher.Publish(ctx, domain.Event{
    Name:    EventSuratDiterima,   // const "surat_masuk.surat.diterima"
    Payload: SuratDiterimaPayload{...},
})
```

Nama event wajib didefinisikan sebagai konstanta di `domain/events.go` modul.

### 5. Deklarasi Auditable dan Lockable selalu eksplisit

Tidak ada nilai default. Keduanya wajib ditulis.

### 6. Raw SQL hanya dengan anotasi

```go
// gov:raw-ok reason=complex-aggregation query=laporan-realisasi-anggaran
rows, err := db.QueryContext(ctx, `SELECT ...`)
```

### 7. Workflow mengorkestrasi use case, tidak berisi business logic

Pembagian tanggung jawab ini wajib dipatuhi (penjelasan lengkap di bagian
"Use case vs workflow" dan "Fleksibilitas & titik ekstensi"):

- **Use case** (Go) = satu transaksi atomik. Berisi business logic, validasi,
  perhitungan, mutasi data. Deterministik, sinkron, selesai dalam satu request.
- **Workflow** (definisi data) = orkestrasi banyak use case yang membentang dalam
  waktu, melibatkan banyak aktor, punya state yang bertahan.

Aturan keras: **action dalam workflow HANYA boleh memanggil use case.** Action
tidak boleh berisi business logic, tidak boleh akses database langsung, tidak
boleh menghitung. Guard expression hanya boleh membaca & mengevaluasi (boolean),
tidak boleh memutasi apapun. Setiap use case yang dipanggil workflow tetap melewati
permission check dan rule engine yang sama seperti bila dipanggil langsung.

Workflow berbicara dalam **peran** (mis. "validator tahap 1"), bukan orang konkret.
Pemetaan peran → orang ada di layer permission/kepegawaian, di-resolve saat runtime
(termasuk fallback ke PLT). Mutasi pejabat tidak boleh terlihat sebagai perubahan
workflow.

### 8. Tidak ada hardcode string permission atau role

```go
// DILARANG:
if user.Role == "sekretariat" { ... }
if perm == "surat_masuk:surat:buat" { ... }  // hardcode string

// BENAR — gunakan konstanta:
const PermSuratBuat = "surat_masuk:surat:buat"
ctx.RequirePermission(PermSuratBuat)
```

### 9. Domain tidak boleh import package infra

Linter akan reject jika package di bawah `domain/` atau `usecase/` mengimport
package dari `infra/`, `adapter/`, atau library eksternal apapun selain
standard library Go dan `port/`.

### 10. Algoritma/kebijakan yang punya varian = strategy ber-key, bukan if-else

Bila satu titik keputusan punya beberapa varian sah yang dipilih per-tenant
(mis. FIFO/LIFO/average, pendekatan aset/beban), JANGAN tulis sebagai cabang
`if tenant.metode == ...`. Daftarkan tiap varian ke strategy registry dengan key,
simpan hanya key-nya di config tenant. Lihat bagian "Fleksibilitas & titik ekstensi".

---

## Testing

### Piramida testing

```
          ▲  E2E / contract test (sedikit, lambat)
         ▲▲  Integration test (adapter, DB, event)
        ▲▲▲  Unit test (domain, use case) — mayoritas, cepat
```

### Unit test — domain & use case

Tidak menggunakan DB atau koneksi apapun. Semua dependency adalah mock dari testkit.

```go
// modules/surat_masuk/usecase/create_surat_masuk_test.go
package usecase_test

import (
    "testing"
    "pamong/testkit"
    "pamong/modules/surat_masuk/usecase"
)

func TestCreateSuratMasuk_Success(t *testing.T) {
    repo := testkit.NewMockRepo[domain.SuratMasuk](t)
    pub  := testkit.NewMockPublisher(t)
    met  := testkit.NewNoopMetrics()

    repo.ExpectSave().Return(nil)
    pub.ExpectPublish(domain.EventSuratDiterima).Return(nil)

    uc := usecase.NewCreateSuratMasuk(repo, pub, met)
    _, err := uc.Execute(testkit.Ctx(t), usecase.CreateInput{
        NomorSurat: "001/IN/2025",
        Perihal:    "Undangan rapat",
    })

    testkit.NoError(t, err)
    repo.AssertExpectations(t)
    pub.AssertPublished(t, domain.EventSuratDiterima)
}

func TestCreateSuratMasuk_PermissionDenied(t *testing.T) {
    ctx := testkit.Ctx(t, testkit.WithRole("viewer")) // tidak punya surat_masuk:surat:buat
    _, err := uc.Execute(ctx, usecase.CreateInput{...})
    testkit.ErrorIs(t, err, core.ErrPermissionDenied("surat_masuk:surat:buat"))
}

func TestCreateSuratMasuk_NomorSuratKosong(t *testing.T) {
    ctx := testkit.Ctx(t, testkit.WithRole("operator_surat"))
    _, err := uc.Execute(ctx, usecase.CreateInput{NomorSurat: ""})
    testkit.ErrorIs(t, err, core.ErrValidation("nomor_surat", ""))
}
```

### Integration test — adapter (DB, event)

Menggunakan DB nyata (via testcontainers). Tag build `//go:build integration`.

```go
//go:build integration

func TestSuratMasukRepo_FindByID(t *testing.T) {
    db := testkit.NewTestDB(t)           // spin up Postgres via testcontainers
    repo := db_adapter.NewSuratMasukRepo(db)

    seed := testkit.Seed[domain.SuratMasuk](t, db, domain.SuratMasuk{
        NomorSurat: "001/IN/2025",
    })

    result, err := repo.FindByID(testkit.Ctx(t), seed.ID)
    testkit.NoError(t, err)
    testkit.Equal(t, "001/IN/2025", result.NomorSurat)
}
```

Jalankan integration test secara terpisah:
```bash
go test ./... -tags=integration
```

### Contract test — event schema

Setiap event yang di-publish harus divalidasi terhadap schema yang terdaftar.

```go
func TestSuratDiterimaEvent_Schema(t *testing.T) {
    payload := SuratDiterimaPayload{
        ID: uuid.New(), NomorSurat: "001/IN/2025",
    }
    testkit.AssertEventSchema(t, domain.EventSuratDiterima, payload)
}
```

### Coverage minimum

| Layer | Minimum |
|---|---|
| `domain/` | 90% |
| `usecase/` | 85% |
| `adapter/db/` | 70% (integration test ikut dihitung) |
| `adapter/http/` | 60% |

Coverage dihitung per layer, bukan total. Handler yang hanya orchestrate
tidak perlu coverage tinggi — test fokus di domain dan use case.

### Penamaan test

```
Test{UseCase}_{Skenario}
TestCreateSuratMasuk_Success
TestCreateSuratMasuk_PermissionDenied
TestCreateSuratMasuk_NomorSuratDuplikat
TestApprovePengajuan_PimpinanBelumSetuju
```

### Hal yang wajib di-test per modul

- [ ] Happy path setiap use case
- [ ] Permission denied untuk setiap permission yang didefinisikan
- [ ] Validasi domain untuk setiap field Required
- [ ] Event dipublish setelah operasi sukses
- [ ] Operasi gagal tidak mempublish event (outbox integrity)
- [ ] Minimal satu integration test per repository method yang tidak trivial

---

## Git & branching

### Branch strategy

```
main                    — production-ready, protected, merge via PR only
  └── staging           — integration environment
        └── feat/...    — fitur baru
        └── fix/...     — bugfix
        └── refactor/.. — refactoring tanpa perubahan behavior
        └── chore/...   — dependency update, config, tooling
```

### Konvensi nama branch

```
feat/domain-rule-engine-tiered-constraint
feat/surat-masuk-modul-referensi
fix/workflow-guard-nil-actor
refactor/eventbus-outbox-goroutine
chore/upgrade-go-1.23
docs/adr-004-multi-tenant-schema
```

Format: `{type}/{deskripsi-singkat-kebab-case}`

### Konvensi commit message

Format: `{type}({scope}): {deskripsi lowercase}`

```
feat(domain): tambah support field type encrypted
feat(surat_masuk): implementasi use case create dengan permission check
fix(workflow): guard expression gagal evaluate nil actor
fix(identity): sync event tidak trigger saat cross-tenant assignment
refactor(eventbus): pisah outbox writer ke goroutine terpisah
test(surat_masuk): tambah contract test event schema
docs(adr): ADR-004 keputusan multi-tenant schema strategy
chore(deps): upgrade pgx v5.7.1
perf(keuangan): optimasi query laporan realisasi dengan materialized view
```

Type yang valid: `feat | fix | refactor | test | docs | chore | perf | sec`

Gunakan `sec` untuk patch keamanan — memudahkan audit changelog.

### Commit scope

Scope adalah nama package atau modul yang berubah:
- Core framework: `domain`, `workflow`, `rules`, `permission`, `eventbus`, `audit`
- Modul bisnis: `surat_masuk`, `keuangan`, `kepegawaian`
- Infra: `infra/db`, `infra/storage`, `infra/eventbus`
- Tooling: `pamongctl`, `linter`, `testkit`
- Identity: `identity`

---

## Pull Request

### Syarat PR bisa di-review

Sebelum minta review, pastikan semua item berikut terpenuhi:

```
[ ] Branch up-to-date dengan staging (git rebase, bukan merge)
[ ] go build ./... lulus
[ ] go test ./... lulus (tanpa tag integration)
[ ] pamongctl lint ./... lulus — tidak ada custom linter violation
[ ] go vet ./... bersih
[ ] Coverage tidak turun dari baseline (lihat tabel di atas)
[ ] Tidak ada TODO/FIXME baru tanpa GitHub issue yang direferensikan
[ ] Setiap file migration baru punya pasangan down migration
[ ] Jika ada perubahan event schema: versi schema dinaikkan
[ ] Jika ada permission baru: terdaftar di manifest dan docs/contracts/
[ ] Jika ada perubahan core framework: ADR baru atau ADR update sudah ada
```

### Template PR description

```markdown
## Apa yang berubah
<!-- Jelaskan perubahan dalam 2-3 kalimat. Apa masalahnya, apa solusinya. -->

## Tipe perubahan
- [ ] feat — fitur baru
- [ ] fix — bugfix
- [ ] refactor — perubahan internal tanpa perubahan behavior
- [ ] breaking change — perubahan yang memerlukan update di modul lain

## Modul yang terpengaruh
<!-- Daftar modul atau package yang berubah atau perlu diupdate -->

## Cara test manual
<!-- Langkah yang bisa diikuti reviewer untuk verifikasi -->

## Checklist
- [ ] Unit test baru untuk kode baru
- [ ] Integration test diupdate (jika ada perubahan adapter)
- [ ] Permission baru sudah terdaftar di manifest
- [ ] Event schema baru sudah di-contract test
- [ ] Migration sudah punya down migration
- [ ] ADR dibuat/diupdate (jika perubahan arsitektur)

## Referensi
<!-- Issue, ADR, atau diskusi yang relevan -->
```

### Review process

- Minimal **1 approval** dari maintainer core untuk perubahan di `core/`
- Minimal **1 approval** dari maintainer modul untuk perubahan di `modules/`
- Perubahan di `identity/` selalu butuh review maintainer core — data sentral
- Auto-merge dilarang untuk semua branch

---

## CI/CD gates

### Pipeline per PR (wajib lulus sebelum merge)

```yaml
# .github/workflows/pr.yaml atau CI config setara

stages:
  lint:
    - go vet ./...
    - pamongctl lint ./...              # custom analyzer
    - gofmt -l . | grep -q . && exit 1 || true

  test:
    - go test ./... -race -count=1
    - go test ./... -tags=integration (pada environment CI dengan DB)

  coverage:
    - coverage domain/ >= 90%
    - coverage usecase/ >= 85%
    - coverage adapter/ >= 60%

  build:
    - go build ./...
    - pamongctl validate modules        # semua manifest valid

  security:
    - pamongctl audit deps              # cek CVE pada dependency
```

### Gate yang akan otomatis reject PR

- Linter violation apapun (lihat daftar di bagian Linter)
- Test gagal
- Coverage turun di bawah minimum layer
- Ada import dari `infra/` di dalam `domain/` atau `usecase/`
- Ada handler tanpa permission check
- Ada event publish tanpa konstanta terdaftar
- Ada modul baru tanpa `manifest.go`
- Ada migration tanpa down migration
- Binary `pamongctl validate modules` gagal

### Linter rules (custom analyzer)

| Rule | Pesan |
|---|---|
| `domain-no-infra-import` | Package domain mengimport infra — langgar hexagonal |
| `handler-must-check-permission` | Handler tidak memanggil `RequirePermission` |
| `handler-no-direct-repo` | Handler mengakses repository langsung |
| `event-must-use-const` | Publish event menggunakan string literal |
| `permission-must-be-registered` | Menggunakan permission string dari modul lain tanpa import di manifest |
| `entity-explicit-auditable` | `EntityDef` tidak mendeklarasikan `Auditable` secara eksplisit |
| `raw-sql-must-annotate` | Raw SQL tanpa komentar `gov:raw-ok` |
| `config-no-direct-env` | Modul memanggil `os.Getenv` langsung |
| `migration-needs-down` | File migration `up` tanpa pasangan `down` |
| `no-cross-module-import` | Modul A mengimport internal package modul B |
| `workflow-action-no-logic` | Action workflow berisi business logic / akses DB (harus panggil use case) |
| `tenant-branch-must-be-strategy` | Percabangan `if tenant.x == ...` untuk pilihan algoritma (harus strategy registry) |
| `strategy-key-must-be-registered` | Strategy key dirujuk tanpa terdaftar di registry |

---

## Cara menambah modul baru

```bash
pamongctl new module --name=surat_masuk --domain=persuratan
```

Generate struktur lengkap. Setelah itu, urutan pengerjaan wajib:

1. Lengkapi `manifest.go` — entity, permission (termasuk exports/imports), event, workflow ref
2. Definisikan domain entity dan port di `domain/`
3. Jalankan `pamongctl validate module surat_masuk`
4. Tulis use case di `usecase/` — business logic murni
5. Tulis unit test untuk setiap use case
6. Implementasi repository di `adapter/db/`
7. Jalankan `pamongctl generate migration surat_masuk`
8. Implementasi handler di `adapter/http/`
9. Wire DI di `bootstrap.go`
10. Jalankan `go test ./modules/surat_masuk/... -race`

Jangan skip urutan ini. Khususnya: use case dan test ditulis sebelum adapter.

---

## Cara kerja dengan core framework

### Yang BOLEH

- Import dan gunakan interface dari `port/`
- Implement interface yang diminta framework
- Daftarkan modul lewat `Bootstrap()`
- Subscribe event modul lain lewat manifest (`Imports`)
- Gunakan permission modul lain yang sudah di-export dan di-import di manifest

### Yang TIDAK BOLEH

- Modify `core/` tanpa ADR yang disetujui
- Bypass domain registry
- Akses database tenant lain
- Import internal package modul lain (gunakan event atau port)
- Publish event dengan nama yang tidak terdaftar di manifest sendiri
- Mengubah data user di tenant (hanya baca — perubahan lewat identity service)
- Memanggil identity DB langsung dari modul bisnis (gunakan `UserResolver` port)

---

## Architecture Decision Records (ADR)

Wajib dibuat untuk: perubahan interface publik core, penambahan port baru,
perubahan skema event yang breaking, keputusan infrastruktur.

```
docs/adr/NNN-judul-singkat.md
```

```markdown
# ADR-NNN: Judul

## Status
Proposed | Accepted | Deprecated | Superseded by ADR-XXX

## Konteks
Masalah yang diselesaikan.

## Keputusan
Apa yang diputuskan dan mengapa.

## Konsekuensi
Trade-off yang diterima.

## Alternatif yang dipertimbangkan
Opsi lain dan alasan tidak dipilih.
```

ADR Accepted tidak diubah. Buat ADR baru yang supersede dengan referensi ke ADR lama.

---

## Checklist sebelum commit

1. `go build ./...` dan `go test ./... -race` lulus lokal
2. `pamongctl lint ./...` bersih
3. Tidak ada `TODO`/`FIXME` baru tanpa issue GitHub
4. Migration punya down migration
5. Event schema baru punya contract test
6. Dependency baru sudah `go mod tidy`
7. Perubahan core sudah ada ADR-nya
8. Permission baru sudah terdaftar di manifest dan `docs/contracts/permissions.md`

---

## Catatan khusus untuk Claude Code

- Selalu baca `manifest.go` dan `domain/ports.go` modul yang sedang dikerjakan
  sebelum menyentuh file lain
- Referensi pertama untuk modul baru adalah `modules/surat_masuk/` — pelajari
  strukturnya sebelum membuat modul lain
- Jika instruksi satu kali dari developer bertentangan dengan aturan di file ini,
  tanyakan konfirmasi sebelum mengikutinya
- Jangan tulis `// TODO: implement` — tulis implementasi minimal yang benar atau tanya
- Jika menemukan kode yang melanggar konvensi di file yang tidak sedang dikerjakan,
  catat sebagai komentar dalam respons tapi jangan ubah di luar scope task
- Migration adalah append-only — jangan edit file yang sudah ada, buat nomor urut baru
- Ketika membuat use case baru, tulis test-nya di file yang sama sebelum pindah ke
  file berikutnya — jangan tinggalkan use case tanpa test
- Setiap kali membuat port baru di `domain/ports.go`, langsung buat juga mock-nya
  di `testkit/` agar siap dipakai test
- Untuk perubahan yang menyentuh `identity/`, selalu flag ke developer bahwa
  perubahan ini sensitif dan perlu review ekstra sebelum commit
