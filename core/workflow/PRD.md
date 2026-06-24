# PRD: Workflow Engine

## Tujuan
Mengorkestrasi rangkaian use case yang membentang dalam waktu dan melibatkan banyak
aktor (disposisi, approval berlapis, pengesahan). Definisi workflow sebagai data yang
bisa berbeda antar tenant dan diubah tanpa redeploy — sambil menjaga bahwa logika
sesungguhnya tetap di use case (Go), bukan di workflow.

## Konteks & batasan

### Jadi tanggung jawab
- Eksekusi state machine: kelola state, transition, guard, action dispatch
- Definisi sebagai data: simpan di DB, ber-versi, seed dari YAML
- Pilihan/komposisi alur per-tenant (tahap ini: template selection)
- Guard expression evaluation (boolean, sempit, aman)
- SLA & eskalasi

### BUKAN tanggung jawab
- Business logic langkah (use case modul)
- Resolusi peran ke orang (core/permission + kepegawaian)
- Pengiriman notifikasi aktual (core/notification)

## Model data / tipe kunci

```go
type WorkflowDefinition struct {
    ID            string         // key: {modul}.{alur}.{varian}
    Entity        string         // "penatausahaan.SPM"
    Version       int
    EffectiveFrom time.Time
    InitialState  string
    States        []State
    Transitions   []Transition
    AuthoringSource string       // "developer" (template) | "tenant" (future)
}

type State struct {
    Name       string
    Label      string
    SLAHours   int              // 0 = tanpa SLA
    EscalateToRole string
    IsTerminal bool
    Actions    []string         // aksi yang tersedia di state ini
}

type Transition struct {
    From    string
    To      string
    On      string              // nama event/aksi pemicu
    Guards  []string            // expression boolean, di-AND-kan
    Action  string              // nama use case yang dipanggil (opsional)
    Notify  *NotifySpec         // peran + template (opsional)
}

type WorkflowInstance struct {
    ID            uuid.UUID
    DefinitionID  string
    DefinitionVersion int        // versi saat instance dimulai (dikunci)
    EntityID      uuid.UUID
    CurrentState  string
    StartedAt     time.Time
}
```

## Kebutuhan fungsional

### F1 — State machine core
- Mulai instance: set ke initial_state, catat versi definisi.
- Eksekusi transisi: cek transisi ada dari current_state dengan aksi tsb; evaluasi
  semua guard (AND); bila lolos, panggil action (use case), pindah state, catat history.
- Transisi ilegal (tidak ada dari state ini, atau guard gagal) → tolak, state tak berubah.
- Action gagal (use case return error) → transisi dibatalkan, state tak berubah, error
  dipropagasi.
- Edge: aksi pada state terminal → tolak. Transisi ke state tak terdefinisi → tolak
  saat load (bukan runtime).

### F2 — Definition store (DB-backed)
- CRUD definisi di gov.workflow_definitions.
- Versioned: setiap perubahan = versi baru dengan effective_from; versi lama tetap ada.
- Audit: siapa mengubah, kapan, dari versi apa ke apa.
- Validasi struktur saat simpan: semua transisi merujuk state yang ada; ada minimal satu
  state terminal yang reachable; initial_state ada; tidak ada state unreachable (warning).

### F3 — YAML seed loader
- Saat bootstrap, baca file YAML workflow dari modul.
- Validasi schema. Bila definisi belum ada di DB → simpan sebagai versi 1
  (authoring_source = developer). Bila sudah ada → tidak menimpa (DB adalah yang aktif).
- YAML invalid → gagal bootstrap dengan pesan jelas.

### F4 — Template selection per-tenant
- Developer menulis beberapa template lengkap (varian alur berbeda).
- Tenant memilih template ber-key, disimpan di gov.tenant_configs.
- Parameter binding: pemetaan peran generik dalam workflow (mis. "validator_tahap_1")
  ke jabatan/role konkret di tenant. Disimpan bersama pilihan.
- Resolusi: saat instance jalan, peran di-resolve ke orang via core/permission +
  kepegawaian (termasuk fallback PLT).

### F5 — Guard expression DSL
- Sintaks: `actor.has_permission('x')`, `actor.has_role('y')`, `entity.field > 100`,
  `entity.field != ''`, operator boolean (&&, ||, !), perbandingan.
- Konteks evaluasi: actor (dari AuthContext), entity (data dokumen), tenant.
- Di-compile saat definisi di-load → syntax error / referensi tak dikenal ditolak saat
  load, bukan runtime.
- Output WAJIB boolean (divalidasi tipe saat compile).
- Tanpa side-effect: tidak bisa memanggil fungsi yang memutasi, tidak bisa akses I/O,
  tidak bisa loop tak terbatas. Deterministik.
- TIDAK ada fungsi custom yang didefinisikan tenant (konservatif untuk auditabilitas).

### F6 — SLA & eskalasi
- State dengan sla_hours > 0: daftarkan deadline ke scheduler saat masuk state.
- Saat lewat SLA: jalankan eskalasi (notify escalate_to_role) via core/notification.
- Transisi keluar state membatalkan timer SLA-nya.

### F7 — History
- Setiap transisi dicatat immutable: from, to, action, actor, timestamp, komentar.
- Bisa di-query per instance untuk ditampilkan (workflow visualizer di UI).

## Kebutuhan non-fungsional
- Evaluasi guard: < 5ms (sudah ter-compile).
- Eksekusi transisi (di luar use case): < 20ms.
- Load + compile semua definisi saat bootstrap: < 200ms untuk ratusan definisi.

## Dependency
- port/workflow.go — kontrak publik
- port/auth.go — AuthContext untuk guard & permission
- port/eventbus.go — transisi bisa dipicu event dari modul lain
- core/scheduler — SLA timer (via port; bisa stub awal)
- core/notification — eskalasi (via port; bisa stub awal)
- core/permission — resolusi peran (binding)

## Anti-pattern / yang harus dihindari
- Logika bisnis di guard atau action. Guard = boolean read-only; action = panggil use case.
- Guard yang membaca DB atau memanggil service — hanya baca konteks yang diberikan.
- Instance tidak mengunci versi definisi → perubahan workflow merusak proses berjalan.
- Menyimpan orang konkret di definisi — selalu peran, resolusi saat runtime.
- Membuat DSL terlalu ekspresif (Turing-complete) — jaga tetap sempit & aman.

## Keputusan tertunda / open questions
- Tahap future "tenant menulis workflow sendiri": pakai engine yang sama, tambah
  validator lebih ketat + UI editor. Gerbang validasi tenant-authored belum dispesifikasi.
- Parallel gateway (approval paralel: semua harus setuju) — desain awal fokus sekuensial;
  parallel ditambah sebagai ekstensi state machine, titiknya disiapkan.

## Acceptance criteria
- [ ] Transisi valid → action terpanggil + state berubah + history tercatat.
- [ ] Transisi ilegal / guard gagal → ditolak, state tak berubah.
- [ ] Action (use case) gagal → transisi batal, state tak berubah.
- [ ] Action yang bukan pemanggilan use case → ditolak (linter + runtime guard).
- [ ] Guard expression dievaluasi benar (boolean); syntax error ketahuan saat load.
- [ ] Guard dengan output non-boolean → ditolak saat compile.
- [ ] Dua tenant dengan template berbeda jalan; use case identik dipanggil keduanya.
- [ ] Perubahan definisi (versi baru) tidak mengubah instance yang sedang berjalan.
- [ ] SLA lewat → eskalasi terkirim ke role yang benar.
- [ ] History transisi immutable & dapat di-query per instance.
