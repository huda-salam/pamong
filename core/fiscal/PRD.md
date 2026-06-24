# PRD: Fiscal Period & Locking

## Tujuan
Menegakkan disiplin periode fiskal pemerintahan: data periode yang sudah ditutup tidak
boleh diubah sembarangan (koreksi hanya via jurnal koreksi yang ter-audit), dan periode
yang sudah hard-close (pasca-audit BPK) terkunci total. Juga mengelola pergantian tahun
anggaran (cutoff/continuous) dan carry-forward saldo.

## Konteks & batasan

### Jadi tanggung jawab
- Definisi & status periode fiskal
- Enforcement otomatis penguncian pada mutasi entity Lockable
- Proses tutup buku (soft/hard close) & reopen terkontrol
- Annual cutoff: schema per tahun, carry-forward, aggregation ke warehouse

### BUKAN tanggung jawab
- Logika akuntansi/jurnal (itu modul akuntansi)
- Keputusan entity mana yang Lockable (itu EntityDef di core/domain)
- Penyimpanan data historis lintas tahun (warehouse skema; fiscal hanya memicu agregasi)

## Model data / tipe kunci

```go
type FiscalPeriodStatus string
const (
    FiscalOpen       FiscalPeriodStatus = "open"
    FiscalSoftClosed FiscalPeriodStatus = "soft_closed"
    FiscalHardClosed FiscalPeriodStatus = "hard_closed"
)

type FiscalPeriod struct {
    TenantID     string
    Tahun        int
    Bulan        int             // 1-12, unit terkecil
    Status       FiscalPeriodStatus
    ClosedBy     *uuid.UUID
    ClosedAt     *time.Time
    ReopenReason *string         // wajib bila pernah dibuka kembali
}

// FiscalChecker dipanggil framework otomatis sebelum mutasi entity Lockable
type FiscalChecker interface {
    CheckPeriod(ctx context.Context, tenantID string, date time.Time) (FiscalPeriodStatus, error)
}
```

## Kebutuhan fungsional

### F1 — Definisi periode
- Unit terkecil: bulan (tenant, tahun, bulan). Triwulan/semester/tahun = agregasi query,
  bukan tabel terpisah.
- Status awal periode: open.

### F2 — Status & transisi
- Transisi satu arah: open → soft_closed → hard_closed.
- soft_closed: transaksi normal ditolak; jurnal koreksi diizinkan dengan permission
  khusus + audit ekstra.
- hard_closed: tidak ada mutasi apapun, termasuk jurnal koreksi.
- Tidak bisa "mundur" status tanpa proses reopen khusus.

### F3 — Enforcement otomatis
- Untuk entity Lockable dengan FiscalDateField: sebelum setiap save/update, framework
  ambil tanggal dari field tsb, cari periode, cek status.
- hard_closed → tolak otomatis (ErrPeriodLocked).
- soft_closed → izinkan hanya bila actor punya permission jurnal koreksi; selain itu tolak.
- open → lanjut normal.
- Modul TIDAK menulis cek ini — framework via FiscalChecker.

### F4 — Tutup buku (soft/hard close)
- Soft close: tandai periode soft_closed (mis. tutup bulanan/triwulanan).
- Hard close: tandai hard_closed (tutup tahunan pasca-audit). Butuh approval pimpinan.
- Keduanya ber-permission + ter-audit.

### F5 — Reopen terkontrol
- Reopen hard_closed = pengecualian. Butuh approval pimpinan daerah + berita acara +
  alasan tercatat (reopen_reason). Melewati workflow approval tersendiri.
- Reopen soft_closed lebih ringan tapi tetap ber-permission + ter-audit.

### F6 — Annual cutoff & carry-forward
- Untuk modul data_lifecycle = annual_cutoff: saat tutup tahun, buat schema tahun baru,
  jalankan CarryForwardSpec (copy saldo/data yang dibawa), set schema lama read-only.
- Jalankan AggregationSpec: rangkum data ke warehouse untuk insight lintas tahun sebelum
  schema lama di-archive.
- `pamongctl fiscal close --tahun=Y --tenant=X` mengeksekusi seluruh proses.

## Kebutuhan non-fungsional
- Cek periode: < 3ms (status periode di-cache, invalidasi saat berubah).
- Cutoff: operasi batch, bisa lama; harus idempoten & bisa di-resume bila gagal di tengah.
- Semua perubahan status & reopen ter-audit dengan hash chain.

## Dependency
- port/fiscal.go
- core/domain — entity Lockable & FiscalDateField; hook sebelum mutasi
- core/permission — permission jurnal koreksi, approval close/reopen
- core/audit — pencatatan perubahan status & reopen
- core/workflow — approval reopen hard_closed
- infra/db — pembuatan schema tahun baru saat cutoff

## Anti-pattern / yang harus dihindari
- Modul menulis cek periode sendiri (bypass FiscalChecker).
- Reopen hard_closed yang mudah/tanpa approval berlapis.
- Lupa carry-forward → saldo tahun lama hilang di tahun baru.
- Cutoff tidak idempoten → gagal di tengah meninggalkan state inkonsisten.

## Keputusan tertunda
- Apakah soft close per-bulan otomatis menutup saat bulan berganti, atau manual —
  sementara manual (dikontrol bendahara/admin keuangan).
- Detail format berita acara reopen — diserahkan ke modul keuangan/UI.

## Acceptance criteria
- [ ] Mutasi entity Lockable di periode hard_closed → ditolak otomatis.
- [ ] Mutasi normal di soft_closed → ditolak; jurnal koreksi (dengan permission) → izin.
- [ ] Mutasi di periode open → normal.
- [ ] Hard close butuh approval pimpinan + ter-audit.
- [ ] Reopen hard_closed butuh approval berlapis + berita acara + alasan tercatat.
- [ ] Annual cutoff membuat schema tahun baru & menjalankan carry-forward.
- [ ] Schema tahun lama menjadi read-only setelah cutoff.
- [ ] Cutoff idempoten: dijalankan ulang setelah gagal di tengah tidak merusak.
