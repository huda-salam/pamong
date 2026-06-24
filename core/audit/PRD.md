# PRD: Audit Engine

## Tujuan
Menyediakan jejak audit yang lengkap dan tidak bisa dimanipulasi untuk setiap perubahan
data penting. Untuk pemerintahan, audit trail adalah kebutuhan hukum (pemeriksaan BPK/
BPKP) — harus membuktikan siapa mengubah apa, kapan, dari nilai apa ke apa, dan bahwa
catatan itu sendiri tidak diutak-atik.

## Konteks & batasan

### Jadi tanggung jawab
- Pencatatan otomatis mutasi entity Auditable (before/after, actor, waktu, konteks)
- Integritas: hash chain untuk deteksi manipulasi
- Query, replay, dan verifikasi audit trail

### BUKAN tanggung jawab
- Komentar/disposisi manusia (komponen terpisah; itu catatan manusia, bukan jejak mutasi)
- Logging operasional/teknis (itu observability/structured logging)
- Keputusan apakah entity diaudit (itu EntityDef di core/domain)

## Model data / tipe kunci

```go
type AuditEntry struct {
    ID          uuid.UUID
    TenantID    string
    Entity      string          // "penatausahaan.SPM"
    EntityID    uuid.UUID
    Action      string          // "create" | "update" | "submit" | "delete"
    ActorID     uuid.UUID
    ActorIP     string
    Diff        []FieldDiff     // hanya field yang berubah
    WorkflowFrom string         // bila bagian dari transisi workflow
    WorkflowTo   string
    Timestamp   time.Time
    PrevHash    string          // hash entry sebelumnya (chain)
    Hash        string          // H(PrevHash + konten entry)
}

type FieldDiff struct {
    Field  string
    Before any
    After  any
}
```

## Kebutuhan fungsional

### F1 — Audit writer
- Untuk setiap mutasi entity Auditable: hitung diff (field berubah), catat actor (dari
  AuthContext), IP, timestamp, dan transisi workflow bila ada.
- Append-only ke gov.audit_logs. Tidak ada UPDATE/DELETE.
- Ditulis dalam transaksi yang sama dengan mutasi (atau via outbox) agar konsisten:
  mutasi sukses tapi audit gagal tidak boleh terjadi diam-diam.

### F2 — Field-level diff
- Bandingkan before vs after, catat hanya field yang berubah.
- Untuk create: semua field sebagai "after" (before kosong). Untuk delete: sebaliknya.
- Field sensitif (ditandai di EntityDef) di-mask di audit bila perlu (mis. NIK).

### F3 — Hash chain
- Setiap entry menyimpan hash entry sebelumnya (per tenant, atau per entity — diputuskan
  saat implementasi; default per-tenant chain).
- Hash = H(prev_hash + konten kanonik entry). Entry pertama pakai seed konstan.
- Penulisan diserialisasi agar chain tidak putus oleh race (per tenant/entity).

### F4 — Auto-attach
- Framework meng-attach audit hook ke semua entity Auditable saat boot.
- Modul tidak menulis satu baris kode audit. Entity Auditable → otomatis ter-audit.
- Entity NotAudited → tidak ada overhead audit.

### F5 — Query & replay
- Query audit per entity (riwayat satu dokumen), per actor (apa saja yang dilakukan
  seseorang), per rentang waktu.
- Replay: rekonstruksi state entity pada titik waktu tertentu dari diff berurutan.

### F6 — Verifikasi integritas
- `pamongctl audit verify` menelusuri chain, menghitung ulang hash, mendeteksi bila ada
  entry yang dimodifikasi/dihapus/disisipkan.
- Output: posisi pertama chain putus (bila ada).

## Kebutuhan non-fungsional
- Overhead audit per mutasi: < 10ms (di luar latensi DB).
- Verifikasi chain: linear terhadap jumlah entry; bisa di-batch per periode.
- Audit log tidak boleh jadi bottleneck transaksi — pertimbangkan outbox bila perlu.

## Dependency
- core/domain — Auditable policy & hook attachment
- port/auth.go — actor & IP dari AuthContext
- port/eventbus.go — bila pakai outbox untuk penulisan audit

## Anti-pattern / yang harus dihindari
- Mengizinkan UPDATE/DELETE pada audit log.
- Mencatat field sensitif mentah tanpa masking.
- Penulisan audit paralel tanpa serialisasi → chain putus.
- Audit gagal diam-diam saat mutasi sukses.
- Mengira audit menggantikan comment/disposisi (terpisah).

## Keputusan tertunda
- Granularitas chain: per-tenant vs per-entity. Default per-tenant; per-entity bila
  volume tinggi menyebabkan kontensi serialisasi.
- Cold storage / archiving audit lama (terkait data lifecycle) — desain di Phase lanjut.

## Acceptance criteria
- [ ] Mutasi entity Auditable → audit entry dengan diff field yang benar.
- [ ] Create/update/delete menghasilkan diff yang tepat (before/after sesuai aksi).
- [ ] Entity NotAudited tidak menghasilkan audit entry.
- [ ] Hash chain tersambung; entry menyimpan hash sebelumnya.
- [ ] Modifikasi satu entry → `audit verify` mendeteksi chain putus di posisi itu.
- [ ] Query audit per entity & per actor mengembalikan hasil benar.
- [ ] Audit ditulis konsisten dengan mutasi (tidak ada mutasi sukses tanpa audit).
- [ ] Field sensitif ter-mask di audit.
