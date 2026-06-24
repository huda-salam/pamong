# CODE_CONVENTION.md — Konvensi Kode Pamong

Standar konkret penulisan kode Go di Pamong. Setiap aturan di sini berakar pada
`CODING_PHILOSOPHY.md`. Banyak yang ditegakkan linter (ditandai `[linter]`); sisanya
ditegakkan lewat review dan kebiasaan.

Aturan dasar: jalankan `gofmt` dan `goimports`. Apa pun yang sudah diatur tooling
standar Go, ikuti tooling — dokumen ini hanya menambah yang tidak dicakup tooling.

---

## 1. Layout & struktur paket

### Struktur modul (hexagonal) — wajib
```
modules/{modul}/
├── domain/        # entity, value object, port (interface), domain service, errors
├── usecase/       # orchestrator; satu file per use case
├── adapter/
│   ├── http/      # driving: handler
│   ├── db/        # driven: implementasi repository port
│   └── event/     # driven: consumer event dari modul lain
├── workflows/     # seed YAML workflow
├── migrations/    # SQL up/down
├── manifest.go    # identitas modul
└── bootstrap.go   # wiring DI — satu-satunya tempat port di-bind ke adapter
```

### Aturan import lintas-layer `[linter: domain-no-infra-import]`
- `domain/` dan `usecase/` hanya boleh import: stdlib + `port/`.
- Tidak boleh import `infra/`, `adapter/`, `gateway/`, atau modul lain.
- `adapter/` boleh import `domain/`, `usecase/`, `port/`, `infra/`.
- Wiring (`bootstrap.go`) boleh import semuanya — ini satu-satunya pengecualian.

### Aturan lintas-modul `[linter: no-cross-module-import]`
- Modul A tidak boleh import package internal modul B.
- Butuh kemampuan modul lain → lewat port yang di-inject, atau event.

---

## 2. Penamaan

| Konteks | Konvensi | Contoh |
|---|---|---|
| Package | `snake_case`, singular, pendek | `surat_masuk`, `penatausahaan` |
| File | `snake_case.go` | `create_spm.go`, `repository.go` |
| File use case | `{verb}_{entity}.go` | `sahkan_spm.go` |
| File test | `{nama}_test.go` | `create_spm_test.go` |
| Interface | `PascalCase`, tanpa prefix `I` | `Repository`, `EventPublisher` |
| Port interface | suffix `Port` bila lintas layer | `StoragePort`, `MetricsPort` |
| Struct domain | `PascalCase` | `SuratMasuk`, `NomorSurat` |
| Use case struct | `{Verb}{Entity}` | `CreateSPM`, `ApprovePengajuan` |
| Konstruktor | `New{Tipe}` | `NewCreateSPM`, `NewSPMRepo` |
| Error var | `Err{Deskripsi}` | `ErrPaguTidakCukup` |
| Konstanta event | `Event{Entity}{Kejadian}` | `EventSPMDisahkan` |
| Konstanta permission | `Perm{Aksi}` | `PermSPMBuat` |
| Interface method | kata kerja | `FindByID`, `Save`, `CekKetersediaan` |
| Boolean | prefix `is/has/can` | `isActive`, `hasAttachment` |

### Identifier domain (string) — format baku
```
Event       : {modul}.{entity}.{kejadian_past_tense}   penatausahaan.spm.disahkan
Permission  : {modul}:{entity}:{aksi}                   penatausahaan:spm:sahkan
Strategy key: {modul}.{titik}.{varian}                  keuangan.persediaan.fifo
Workflow key: {modul}.{alur}.{varian}                   pengadaan.approval.tiga_tahap
Tabel       : {schema}.{entity_plural}                  penatausahaan.spms
```

### Bahasa
- Identifier domain bisnis: **bahasa Indonesia** (`SPM`, `NomorSurat`, `pagu`) — ini
  domain pemerintahan Indonesia, istilahnya tidak diterjemahkan.
- Konstruksi teknis generik: **bahasa Inggris** (`Repository`, `Publish`, `Context`).
- Komentar & dokumentasi: **bahasa Indonesia**.

---

## 3. Error handling

### Gunakan error types framework, bukan errors.New/fmt.Errorf bebas
```go
return core.ErrNotFound("SPM", id)
return core.ErrPermissionDenied("penatausahaan:spm:sahkan")
return core.ErrValidation("nilai", "harus lebih dari 0")
return core.ErrConflict("nomor_spm sudah ada")
```
Error types ini auto-map ke HTTP status oleh gateway. Jangan tangani mapping di handler.

### Wrapping
- Bungkus error dengan konteks saat menyeberang batas layer: `fmt.Errorf("simpan SPM: %w", err)`.
- Selalu pakai `%w` (bukan `%v`) agar `errors.Is`/`errors.As` bekerja.
- Jangan log lalu return error yang sama (double logging). Pilih satu: log di batas
  terluar (gateway/handler), return di dalam.

### Yang dilarang
- `panic` di alur normal. Panic hanya untuk: kesalahan programmer (invariant violation)
  dan kegagalan saat boot (config/manifest invalid). Tidak pernah untuk error request.
- Menelan error: `_ = doSomething()` tanpa alasan terdokumentasi.
- `if err != nil { return nil }` — menelan diam-diam. Selalu return atau tangani.

---

## 4. Context

- Setiap fungsi yang melakukan I/O atau bisa dibatalkan menerima `context.Context`
  sebagai parameter **pertama**.
- Untuk use case, parameter pertama adalah `port.AuthContext` (yang embed context +
  identitas + tenant).
- Jangan simpan `context.Context` dalam struct. Selalu lewat parameter.
- Jangan pakai `context.Background()`/`context.TODO()` di dalam request path — propagasi
  context dari caller.

---

## 5. Aturan domain & use case

### Use case `[linter: handler-must-check-permission]`
```go
func (uc *CreateSPM) Execute(ctx port.AuthContext, in CreateSPMInput) (*domain.SPM, error) {
    // 1. Permission — WAJIB baris pertama, sebelum apa pun
    if err := ctx.RequirePermission(domain.PermSPMBuat); err != nil {
        return nil, err
    }
    // 2. Validasi input
    // 3. Business logic (lewat port untuk dependency)
    // 4. Persist
    // 5. Publish event
}
```

### Handler `[linter: handler-no-direct-repo]`
Handler hanya: cek permission → bind input → panggil use case → tulis response.
Tidak ada business logic, tidak ada akses repository langsung, tidak ada query DB.

### Repository
- Interface didefinisikan di `domain/ports.go`, implementasi di `adapter/db/`.
- Method menerima `context.Context`, bukan `AuthContext` (repository tidak cek permission —
  itu tugas use case).

### Raw SQL `[linter: raw-sql-must-annotate]`
```go
// gov:raw-ok reason=agregasi-kompleks query=laporan-realisasi
rows, err := db.QueryContext(ctx, `SELECT ...`)
```
Tanpa anotasi `gov:raw-ok` + alasan, linter menolak. JOIN lintas-schema modul lain
tetap dilarang meski beranotasi `[linter: no-cross-schema-join]`.

### Event `[linter: event-must-use-const]`
```go
// Definisikan di domain/events.go
const EventSPMDisahkan = "penatausahaan.spm.disahkan"

// Publish pakai konstanta, bukan string literal
publisher.Publish(ctx, port.Event{Name: domain.EventSPMDisahkan, Payload: p})
```

### Strategy & workflow `[linter: tenant-branch-must-be-strategy, workflow-action-no-logic]`
- Pilihan algoritma per-tenant → strategy registry, bukan `if tenant.x == ...`.
- Action workflow hanya memanggil use case, tidak berisi business logic.

---

## 6. Tipe & data

- Uang & nilai finansial: **selalu** `decimal.Decimal`, tidak pernah `float64`.
  Float untuk uang = pembulatan salah = temuan audit.
- ID: `uuid.UUID`, di-generate aplikasi (`uuid.New()`), bukan auto-increment DB.
- Waktu: `time.Time` dengan timezone eksplisit (`TIMESTAMPTZ` di DB). Simpan UTC,
  tampilkan lokal.
- Enum: tipe string bernama + konstanta, bukan int ajaib.
  ```go
  type StatusSPM string
  const (
      StatusDraft    StatusSPM = "draft"
      StatusDiajukan StatusSPM = "diajukan"
  )
  ```
- Hindari `interface{}`/`any` kecuali benar-benar perlu (payload event generik). Pakai
  generics bila bisa.

---

## 7. Concurrency

- Hindari goroutine "lepas" tanpa pengelolaan lifecycle. Goroutine harus punya cara
  berhenti (context cancellation) dan cara melaporkan error.
- Mutasi data bersama → lindungi dengan mutex atau channel; lebih baik desain agar tidak
  ada shared mutable state.
- Operasi pada DB tenant: jangan asumsikan satu koneksi dipakai bersama goroutine.
- Idempotency & optimistic locking ditangani framework — jangan implementasi sendiri.

---

## 8. Dependency & modul Go

- Tambah dependency baru hanya bila perlu dan beralasan. Pustaka untuk uang, uuid,
  decimal, pgx, dsb sudah standar — jangan tambah alternatif.
- `go mod tidy` sebelum commit.
- Pin versi; jangan pakai `latest`.
- Dependency baru yang masuk ke `domain/` hampir selalu salah — domain harus murni.

---

## 9. TODO & FIXME

Dua tipe penanda, dua format berbeda tergantung apakah pekerjaan sudah ada
di ROADMAP atau belum.

### Format baku

```go
// TODO: PR-1.2.1 implementasi koneksi DB nyata (sekarang stub)
// FIXME: #123 query ini double-count jika ada koreksi di hari yang sama
```

### Dua tipe dan formatnya

| Marker | Situasi | Format referensi | Contoh |
|---|---|---|---|
| `TODO` | Implementasi yang sudah dijadwalkan di ROADMAP | `PR-X.Y.Z` | `// TODO: PR-3.1.1 ganti memory driver dengan NATS` |
| `TODO` | Implementasi yang belum ada jadwalnya | `#issue` | `// TODO: #123 tambah validasi duplikat nomor surat` |
| `FIXME` | Bug/workaround yang diketahui — selalu di luar jadwal | `#issue` | `// FIXME: #456 race condition di concurrent tenant init` |

**Karena ROADMAP sudah terstruktur per-PR**, sebagian besar TODO cukup
referensi `PR-X.Y.Z` dan tidak perlu membuka issue baru — ROADMAP sudah
menjadi kontrak yang terlacak.

### Aturan

- `TODO: PR-X.Y.Z` — tidak perlu issue baru; ROADMAP adalah sumber kebenaran.
- `TODO: #issue` — buka issue dulu, baru tulis TODO. Jangan tulis `#?` atau `nanti`.
- `FIXME` **selalu** butuh nomor issue — ini bug yang diketahui, harus terlacak.
- **Jangan tulis `// TODO: implement`** tanpa konteks apapun. Tulis implementasi
  minimal yang benar, atau tentukan PR/issue-nya.
- `FIXME` pada kode yang menyentuh uang atau audit **wajib** sertakan dampak:

  ```go
  // FIXME: #789 agregasi double-count jika ada koreksi di hari yang sama.
  // Dampak: laporan bisa over-report maks 0.1%. Aman sampai volume > 50k SPM/hari.
  ```

- **FIXME tidak boleh bertahan lebih dari satu milestone** tanpa progress di issue-nya.

---

## 10. Yang dilarang (ringkas)

```
✗ float64 untuk uang
✗ panic untuk error request
✗ string literal untuk event/permission/role
✗ os.Getenv di dalam modul                      [linter: config-no-direct-env]
✗ akses repository di handler                    [linter: handler-no-direct-repo]
✗ import infra dari domain/usecase               [linter: domain-no-infra-import]
✗ import internal modul lain                     [linter: no-cross-module-import]
✗ JOIN lintas-schema modul lain                  [linter: no-cross-schema-join]
✗ publish event tanpa schema terdaftar           [linter: event-must-use-const]
✗ business logic di dalam action workflow        [linter: workflow-action-no-logic]
✗ if tenant.x untuk pilihan algoritma            [linter: tenant-branch-must-be-strategy]
✗ EntityDef tanpa Audit & Lockable eksplisit     [linter: entity-explicit-auditable]
✗ migration up tanpa down                        [linter: migration-needs-down]
✗ menelan error tanpa alasan terdokumentasi
✗ TODO/FIXME tanpa issue terkait
```
