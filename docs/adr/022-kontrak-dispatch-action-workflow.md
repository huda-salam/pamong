# ADR-022: Kontrak dispatch action workflow & perakitan engine per-tenant

## Status
Accepted

## Konteks

Workflow engine (`core/workflow`) lengkap dan teruji sejak PR-3.2.x: state machine, guard DSL,
SLA, template ber-slot, notifikasi transisi. Adapter DB-nya (`infra/workflow.DBStore`,
`DBTemplateStore`, `SchedulerDeadlines`, `NotifierEscalator`, `NotifierTransition`) juga lengkap
dan teruji integrasi. Tak satu pun terpasang di server hidup — `cmd/server` merakit
`workflowActions`, sebuah `map[string]any` yang menampung apa yang didaftarkan modul saat
Bootstrap **lalu tidak pernah dibaca siapa pun**.

Saat hendak merakitnya (PR-W4a), tiga hal menghalangi, dan ketiganya bukan soal urutan wiring:

**(a) Tak ada kontrak dispatch.** `domain.WorkflowRegistry.RegisterAction(name string, useCase any)`
menerima `any`. Engine memanggil action lewat `ActionDispatcher.Dispatch(ctx, action, instance)`.
Di antara keduanya tak ada satu pun tipe bersama: `*usecase.DisposisiSurat` punya
`Execute(ctx, DisposisiSuratInput) (*Disposisi, error)`, dan tak ada cara memanggilnya dari `any`
tanpa refleksi. `core/workflow` tidak boleh mengimpor modul (hexagonal + no-cross-module-import),
jadi kontraknya harus turun ke `port/`.

**(b) Action butuh argumen yang tak ada di jalur engine.** `DisposisiSurat` perlu
`KepadaJabatan` & `Instruksi` — datang dari request transisi, bukan dari definisi workflow dan
bukan dari instance. `Engine.ExecuteWithComment` hanya membawa `entity map[string]any` (snapshot
untuk guard) dan `comment`. Tanpa jalur argumen, satu-satunya action yang bisa dipanggil adalah
action tanpa input.

**(c) Store definisi & template terikat SATU pool, dan port-nya tanpa `ctx`.**
`DefinitionStore.Get(id)` / `GetVersion(id, version)` tak punya parameter tenant maupun context;
`infra/workflow.DBStore` dan `DBTemplateStore` masing-masing memegang satu `*db.Pool`. Definisi
workflow hidup di **tenant DB** (`gov.workflow_definitions`). Jadi satu engine proses-lebar
mustahil melayani banyak tenant — dan `TenantRoutingConn` (yang menyelamatkan jalur repo lain)
tak menolong, karena tak ada `ctx` yang bisa dibaca `port.TenantFrom`.

## Keputusan

**1. Action workflow adalah PORT bertipe, bukan `any`.**

```go
// port/workflow.go
type WorkflowActionInput struct {
    TenantID   string
    InstanceID uuid.UUID
    EntityID   uuid.UUID
    Action     string
    Params     map[string]any
}

type WorkflowAction interface {
    RunWorkflowAction(ctx AuthContext, in WorkflowActionInput) error
}
```

`domain.WorkflowRegistry.RegisterAction` berubah menjadi
`RegisterAction(name string, action port.WorkflowAction) error`. Modul menulis **adapter tipis**
(`modules/{m}/adapter/workflow/`) yang memetakan `Params` ke input use case-nya yang bertipe.
Pemetaan itu adalah pekerjaan adapter — sama seperti handler HTTP memetakan JSON ke input use
case — sedangkan business logic tetap seluruhnya di use case (linter `workflow-action-no-logic`
tetap berlaku: adapter tak boleh menghitung, memvalidasi bisnis, atau menyentuh DB).

`RegisterAction` kini mengembalikan `error` (nama ganda / action nil ditolak) alih-alih diam.
Salah-wiring adalah kegagalan boot, bukan kejutan pada transisi pertama di produksi.

**2. Argumen action mengalir lewat `TransitionRequest`, terpisah dari snapshot guard.**

```go
// Engine.ExecuteRequest(ctx, inst, TransitionRequest{...})
type TransitionRequest struct {
    Action  string
    Entity  map[string]any // snapshot entity — HANYA untuk guard
    Params  map[string]any // argumen action — HANYA untuk dispatcher
    Comment string
}
```

`Execute`/`ExecuteWithComment` tetap ada sebagai pembungkus (`Params` nil), sehingga pemanggil
lama tak berubah. `ActionDispatcher.Dispatch` bertambah parameter `params map[string]any`.

Keduanya sengaja **tidak** disatukan menjadi satu map. Guard adalah pembacaan **read-only atas
keadaan yang sudah ada**; params adalah **niat aktor pada request ini**. Menyatukannya membuat
guard bisa dievaluasi terhadap nilai yang dikirim aktor sendiri — yaitu aktor menulis sendiri
angka yang menentukan apakah ia boleh lewat.

**3. Engine dirakit PER-TENANT (lazily, di-cache), bukan satu engine ber-`ctx`.**

Alih-alih menambahkan `ctx` ke `DefinitionStore`/`TemplateStore` (dan mengubah setiap
implementasi + test yang ada), composition root membangun satu "tumpukan workflow" per tenant:
pool tenant → `DBStore` + `DBTemplateStore` + `DBInstanceStore` → `Engine`. Dibangun saat tenant
pertama kali dipakai, lalu di-cache selama umur proses. Presedennya sudah ada dan hidup:
`cmd/server/evaluator_factory.go` (catalog role per-tenant) dan
`infra/notification.DBRecipientDirectory` (pool + tenantID tetap).

Isolasi tenant karenanya **struktural** — bukan hasil kolom `tenant_id` yang benar di setiap
query. Objek yang melayani tenant A secara fisik tak terhubung ke DB tenant B.

**4. Instance workflow mendapat persistensi; `InstanceStore` adalah port core.**

Engine stateless terhadap instance (caller yang menyimpan), tapi sampai kini **tak ada satu pun
caller yang bisa menyimpan** — tak ada tabel, repo, maupun implementasi `InstanceStateReader`.
Akibatnya dua kemampuan yang sudah ditulis tak bisa berjalan sama sekali: transisi atas instance
yang dimulai di request sebelumnya, dan guard race eskalasi SLA (yang menanyakan "instance masih
di state itu?"). PR-W4a menambahkan `gov.workflow_instances` + `core/workflow.InstanceStore` +
adapter Postgres-nya.

**5. Transisi DIKUNCI per instance, bukan sekadar ber-optimistic-lock.**

`InstanceStore.TryLockInstance` (Postgres: `pg_try_advisory_xact_lock` di transaksi yang dipegang
selama transisi) diambil SEBELUM instance dibaca dan action dijalankan. Tidak menunggu: instance
yang sedang bertransisi menjawab `409`.

Optimistic locking pada penulisan SAJA tidak menutup ini, dan itu kekeliruan yang mudah dibuat
(termasuk sebagai perbaikan setengah jalan: "klaim" berupa write ber-guard versi sebelum action —
ia hanya menyaring request yang membaca instance pada versi yang sama, sementara request yang tiba
SESUDAH klaim membaca versi baru, ikut mengklaim, dan berjalan paralel). Guard versi melindungi
BARIS instance; yang berbahaya di alur pemerintahan adalah EFEKNYA — dua baris disposisi dan dua
event untuk satu surat, dengan satu jejak di history. Guard versi tetap dipertahankan pada Save
sebagai jaring kedua.

Transisi yang SUDAH otoritatif tetap disimpan meski langkah sesudahnya gagal (penjadwalan SLA,
notifikasi — keduanya sengaja mempropagasi error tanpa membatalkan transisi). Membuang instance di
jalur itu berarti kehilangan transisi yang efeknya sudah terjadi, sekaligus mengizinkan action-nya
dijalankan ulang.

**6. Satu instance per (definisi, entitas), ditegakkan DB.**

Tanpa `uq_wfinst_entity_definition`, siapa pun yang memegang `workflow:instance:mulai` dapat
memulai instance BARU di `initial_state` untuk dokumen yang alurnya sudah selesai, lalu menjalankan
ulang seluruh action-nya — mendisposisi ulang surat yang sudah ditutup, berkali-kali, tanpa satu
pun gerbang yang dilanggar. Konsekuensi yang diterima: perulangan yang sah dimodelkan DI DALAM
definisi (self-loop terkontrol), bukan dengan instance kedua.

**7. Guard yang membaca entity FAIL-CLOSED tanpa snapshot.**

`Program.Eval` menolak ekspresi ber-`entity.<field>` bila snapshot entity tidak disediakan sama
sekali (nil map). Sebelumnya penolakan itu hanya terjadi pada operator numerik: `entity.status !=
'dibatalkan'` bernilai TRUE terhadap nil, sehingga guard yang justru dimaksudkan menjaga dokumen
dibatalkan malah meloloskan transisinya. "Fail-closed sebagian" adalah bentuk terburuk — ia
terlihat aman di test yang kebetulan membandingkan angka. Peta KOSONG tetap dievaluasi: itu
pernyataan pemanggil "snapshot ada, fieldnya memang tak terisi".

**8. Seed baseline bersifat "daftarkan bila belum ada" secara ATOMIK.**

`DefinitionSeeder.SeedIfAbsent` (satu pernyataan SQL ber-`WHERE NOT EXISTS` + `ON CONFLICT DO
NOTHING`), bukan `Get` lalu `Register`. Sejak seed pindah ke jalur request (cold-start tumpukan
tenant), check-then-act punya dua ujung buruk: dua replika bertabrakan di
`PRIMARY KEY (workflow_id, version)` sehingga request pertama tenant itu gagal, atau keduanya
lolos dan baseline developer masuk sebagai versi TERBARU yang menggantikan definisi hasil
kustomisasi tenant. Pada jalur cadangan (store tanpa seeder), kegagalan membaca store TIDAK lagi
diperlakukan sebagai "belum ada" — satu error koneksi sesaat tak boleh berarti reset alur tenant.

## Konsekuensi

- **Perubahan yang memaksa penyesuaian:** `RegisterAction` (tipe + `error`) dan
  `ActionDispatcher.Dispatch` (+`params`). Pemakai `Dispatch` di repo ini seluruhnya test/stub;
  pemakai `RegisterAction` hanya `modules/surat_masuk`. Biaya ditanggung sekarang, saat modul
  bisnis masih satu.
- **Setiap transisi memegang satu koneksi tambahan** selama action berjalan (transaksi pemegang
  advisory lock). Itu harga saling-meniadakan yang benar; `try` (bukan `lock`) menjaga lonjakan
  request pada satu instance tidak berubah menjadi habisnya pool.
- **Otorisasi tingkat ENTITAS belum ada.** `workflow:instance:*` berlaku se-tenant lintas modul:
  pemegangnya bisa memulai alur atas entitas apa pun dan membaca riwayat instance mana pun di
  tenantnya (termasuk komentar & id aktor). Menutupnya menuntut seam yang sama dengan snapshot
  entity untuk guard — "bolehkah aktor ini menyentuh entitas itu?" — dan karenanya ditunda ke
  PR-W4c bersamanya, bukan ditambal dengan pagar setengah di handler.
- **Params adalah `map[string]any` di batas engine.** Tipenya baru tegak di adapter modul. Itu
  disengaja: engine tak boleh tahu bentuk input use case modul mana pun. Kegagalan konversi
  (field wajib kosong / tipe salah) menjadi error transisi — action gagal ⇒ transisi batal ⇒
  state tidak berubah, jalur yang sudah dijamin engine.
- **Satu tumpukan per tenant menambah objek per tenant** (2 store + engine + repo). Semuanya
  struct tipis di atas pool yang memang sudah per-tenant; yang mahal (pool) tak digandakan.
- **Definisi workflow tetap di tenant DB**, jadi seed YAML modul di-load per tenant saat
  tumpukan tenant itu pertama dibangun (idempoten — `SeedYAML` melewati ID yang sudah ada).
- **Cache tumpukan tak punya invalidasi.** Definisi & pilihan template dibaca dari DB pada setiap
  Start/Execute (store tidak meng-cache isi), jadi yang di-cache hanya perakitan — perubahan
  definisi/template tetap langsung terlihat. Yang TIDAK ikut adalah tenant yang DB-nya dipindah
  saat proses hidup (naik tier); itu sudah menuntut restart pada jalur lain juga.

## Alternatif yang dipertimbangkan

**Refleksi atas `any`** — dispatcher mencari `Execute(ctx, In)` lewat refleksi lalu membangun
`In` dari `Params` via JSON. Nol perubahan seam. Ditolak: salah nama field gagal dalam diam
(`json.Unmarshal` ke field tak dikenal bukan error), sehingga action "berhasil" dengan input
kosong — dan kegagalan itu baru muncul di produksi pada transisi pertama, persis kelas kegagalan
senyap yang Sub-phase 5.0 ada untuk membayarnya.

**Menambah `ctx context.Context` ke `DefinitionStore`/`TemplateStore`** — satu engine
proses-lebar yang me-route per tenant lewat `port.TenantFrom(ctx)`. Ditolak untuk PR ini: ia
mengubah empat port core + setiap implementasi & test yang ada, demi keuntungan yang tak dituntut
siapa pun sekarang (jumlah tenant per proses kecil, dan perakitan per-tenant sudah jadi pola
repo). Bila kelak jumlah tenant per proses membesar sampai perakitan per-tenant terasa, jalur ini
tetap terbuka tanpa membatalkan keputusan mana pun di atas.

**Params ikut di `entity` map** — hemat satu field. Ditolak karena alasan di Keputusan 2: guard
akan mengevaluasi nilai yang dikirim aktor sendiri.

**Menyimpan instance di memori proses** — cukup untuk demo DoD. Ditolak: instance workflow adalah
state proses pemerintahan yang membentang berhari-hari; kehilangannya saat restart berarti
kehilangan riwayat siapa-mendisposisi-kapan yang justru menjadi alasan modul ini ada.

## Referensi
- ADR-011 (seam SLA/eskalasi), ADR-012 (bridge notifikasi + RoleBindings)
- ROADMAP PR-W4, DoD 11 (tidak ada komponen "selesai tapi dorman")
- CLAUDE.md §7 (workflow mengorkestrasi use case, tidak berisi business logic)
