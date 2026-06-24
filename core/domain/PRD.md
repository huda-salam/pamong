# PRD: Domain Engine

## Tujuan
Menyediakan mekanisme agar developer modul mendefinisikan domain secara eksplisit dan
deklaratif, sehingga framework dapat auto-generate infrastruktur (tabel, endpoint CRUD,
permission default, audit, fiscal check) tanpa boilerplate manual. Ini fondasi
"convention over configuration" dan basis dari sistem tier yang membuat 60-70% entity
bisa dibuat tanpa menulis Go.

## Konteks & batasan

### Jadi tanggung jawab
- Kontrak modul (Module interface) dan registry yang mengelolanya
- Definisi entity & field yang self-describing dan tervalidasi
- Lifecycle hooks yang dipanggil framework
- Sistem tier (1/2/3) dan resolusinya
- Orkestrasi generation (memanggil infra untuk DDL, gateway untuk endpoint)

### BUKAN tanggung jawab
- Eksekusi SQL atau koneksi DB (delegasi ke infra/db lewat port)
- HTTP serving (delegasi ke gateway)
- Business logic spesifik modul
- Workflow, rules, permission evaluation (komponen core terpisah; domain hanya
  menyediakan titik integrasi)

## Model data / tipe kunci

```go
type Module interface {
    Manifest() Manifest
    Bootstrap(ctx context.Context, app *App) error
}

type Manifest struct {
    Name        string          // unik, snake_case, mis. "penatausahaan"
    Version     string          // semver
    Domain      string          // pengelompokan, mis. "keuangan"
    DependsOn   []string         // nama modul lain — divalidasi DAG
    Entities    []EntityDef
    Events      EventManifest    // produces + consumes
    Permissions PermissionManifest
    Workflows   []WorkflowRef
    Config      ConfigSpec       // field config tambahan modul
    DataLifecycle string         // "annual_cutoff" | "continuous"
    CarryForward  CarryForwardSpec // bila annual_cutoff
}

type EntityDef struct {
    Name            string
    Schema          string       // schema Postgres = nama modul
    Tablename       string       // {schema}.{plural}
    Tier            Tier         // Tier1 | Tier2 | Tier3
    Audit           AuditPolicy  // WAJIB eksplisit: Audited{} | NotAudited{Reason}
    Lockable        LockPolicy   // WAJIB eksplisit: Lockable{FiscalField} | NotLockable{}
    HasAttachments  bool
    Searchable      []string     // field untuk global search
    Fields          []FieldDef
    Hooks           HookSet
}

type FieldDef struct {
    Name      string
    Type      FieldType   // Text|Date|DateTime|Enum|Link|File|Decimal|Boolean|Integer|JSON
    Required  bool
    Unique    bool
    Default   *string
    Options   []string    // untuk Enum
    LinkTo    string      // untuk Link: "modul.Entity"
    MaxSizeMB int         // untuk File
    Precision int         // untuk Decimal
}

type HookSet struct {
    BeforeSave   []HookFunc
    AfterSave    []HookFunc
    BeforeSubmit []HookFunc
    AfterSubmit  []HookFunc
    BeforeDelete []HookFunc
}
type HookFunc func(ctx port.AuthContext, e *Entity) error
```

## Kebutuhan fungsional

### F1 — Module manifest & registry
- Setiap modul implement `Module`. Compile-time assertion `var _ Module = (*Module)(nil)`.
- `Register(modules ...Module)` mendaftarkan; discovery membaca semua manifest.
- Validasi DAG dari `DependsOn`: dependency sirkular → panic saat boot, pesan menyebut
  siklus konkret (mis. "A -> B -> A").
- Dependency ke modul tak terdaftar → panic saat boot.
- Registry bisa list semua modul + metadata untuk UI admin / dokumentasi.
- Edge: dua modul dengan `Name` sama → panic. Modul tanpa `Name` → panic.

### F2 — Entity definition & validasi
- Validasi struktural saat register: nama tabel mengikuti `{schema}.{plural}`; field
  `Link` punya `LinkTo` valid (modul.Entity yang ada); `Enum` punya `Options` non-kosong;
  `Decimal` punya `Precision`.
- `Required` tanpa `Default` → field wajib diisi saat create (divalidasi runtime saat
  CRUD).
- `Unique` → constraint unik di DDL + cek saat create/update.
- Dua entity beda modul tidak boleh klaim tabel sama (cek lintas-modul saat boot).
- Edge: field bernama sama dalam satu entity → reject. Nama field reserved (id, version,
  created_at, updated_at, deleted_at) tidak boleh didefinisikan ulang.

### F3 — Lifecycle hooks
- Hook: before_save, after_save, before_submit, after_submit, before_delete.
- Dipanggil framework di titik yang tepat dalam pipeline CRUD/submit — modul tidak
  memanggil sendiri.
- Urutan: hooks dalam satu fase dieksekusi sesuai urutan list (deterministik).
- before_* hook return error → operasi dibatalkan, transaksi rollback.
- after_* hook return error → operasi tetap commit, tapi error di-log + dilaporkan
  (kebijakan: after-hook tidak boleh membatalkan yang sudah commit; bila butuh
  pembatalan, gunakan before-hook).
- Hook menerima AuthContext (punya akses permission & tenant), bukan context biasa.

### F4 — Sistem tier
- **Tier 1**: entity didefinisikan via YAML (atau UI nanti), disimpan di registry/DB.
  Framework serve CRUD penuh saat runtime tanpa Go code: create, read, update (optimistic
  lock), soft delete, list (filter/sort/search/paginate). Validasi deklaratif dari FieldDef.
- **Tier 2**: Tier 1 + file hooks. `pamongctl eject hooks --entity=X` generate satu file
  hooks; CRUD tetap generated.
- **Tier 3**: full hexagonal. `pamongctl eject usecase --entity=X` generate struktur
  domain/usecase/adapter; YAML tetap source of truth schema.
- Tier ditentukan di EntityDef, bukan struktur folder. Naik tier = eject (menambah,
  tidak mengganti). Tidak bisa un-eject.
- YAML/registry selalu source of truth untuk schema di tier manapun.

### F5 — Generation pipeline
Dari EntityDef, framework (berkoordinasi dengan komponen lain via port) menyediakan:
- Tabel DB: CREATE TABLE di schema modul + kolom standar (id, version, timestamps,
  deleted_at) + index untuk Searchable & Unique.
- Endpoint CRUD standar (didaftarkan ke gateway).
- Permission default: `{modul}:{entity}:buat|baca|ubah|hapus`.
- Audit attachment untuk entity Audited (hook otomatis ke audit engine).
- Fiscal check untuk entity Lockable (cek sebelum mutasi via FiscalChecker port).
- Optimistic lock: kolom version, auto-increment, cek saat update.
- Idempotency: endpoint mutasi mengecek idempotency key (via middleware).
- Attachment endpoint untuk entity HasAttachments.

## Kebutuhan non-fungsional
- Registry resolve + validasi semua modul: < 100ms saat boot (untuk ~50 modul).
- Validasi satu EntityDef: < 10ms.
- Tier 1 CRUD response (simple entity, tanpa hook): p95 < 50ms (di luar latensi DB).
- Validasi & generation bersifat deterministik (urutan output stabil untuk diff).

## Dependency
- `port/repository.go` — untuk generation & Tier 1 runtime CRUD (di-stub di awal, infra
  nyata menyusul di Phase 1.2).
- `port/fiscal.go` — untuk entity Lockable (bisa di-stub; fiscal nyata di Phase 3 core/fiscal).
- `port/eventbus.go` — hook after_save bisa publish event.

## Anti-pattern / yang harus dihindari
- Memberi default diam-diam pada Audit/Lockable. Keduanya keputusan sadar.
- Membuat hook yang akses DB langsung (bypass port). Hook memanggil port/use case.
- Logika bisnis di dalam domain engine (engine generik; logika ada di modul).
- Menjadikan tier sebagai struktur folder berbeda — tier adalah properti EntityDef.
- Reflection berat di hot path CRUD Tier 1 — cache hasil introspeksi EntityDef saat boot.

## Keputusan tertunda / open questions
- Apakah perubahan EntityDef Tier 1 di runtime (via UI nanti) butuh migrasi otomatis
  atau hanya additive? Sementara: additive only (tambah kolom nullable); perubahan
  destruktif lewat migration eksplisit.
- Format penyimpanan EntityDef di DB untuk Tier 1 (JSON vs tabel ternormalisasi) —
  diputuskan saat implementasi UI admin (Phase 6).

## Acceptance criteria
- [ ] Dua modul dummy ter-register; registry list keduanya dengan metadata.
- [ ] Dependency sirkular terdeteksi saat boot dengan pesan menyebut siklus.
- [ ] Dependency ke modul tak terdaftar → panic saat boot.
- [ ] EntityDef invalid (Link tanpa target, Enum tanpa options, dst) ditolak dengan
      pesan jelas.
- [ ] EntityDef tanpa Audit/Lockable eksplisit → gagal (compile atau boot).
- [ ] Dua entity klaim tabel sama → ditolak saat boot.
- [ ] Tier 1 entity (YAML only) menghasilkan endpoint CRUD penuh yang berfungsi
      (create/read/update/softdelete/list) tanpa Go code.
- [ ] Hook before_save membatalkan operasi saat return error (transaksi rollback).
- [ ] Hook dieksekusi sesuai urutan dalam list.
- [ ] `pamongctl eject hooks` generate file hooks tanpa merusak definisi existing.
- [ ] `pamongctl eject usecase` generate struktur hexagonal; YAML tetap source of truth.
- [ ] Field reserved tidak boleh didefinisikan ulang.
