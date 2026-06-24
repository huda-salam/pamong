# PRD: Rule Engine

## Tujuan
Memisahkan aturan regulasi (yang berubah tiap tahun) dari logika bisnis (yang stabil),
sehingga perubahan Permendagri/Permenkeu/PP bisa diterapkan tanpa redeploy. Menegakkan
constraint bertingkat di mana aturan level lebih tinggi tidak bisa dilanggar level
lebih rendah.

## Konteks & batasan

### Jadi tanggung jawab
- Penyimpanan rule sebagai data, ber-versi, dengan effective date
- Evaluasi rule (boolean/nilai) terhadap konteks transaksi
- Hierarki tier & penegakan constraint bertingkat
- Deteksi konflik antar rule
- Titik registrasi custom evaluator (Go) untuk logika di luar DSL

### BUKAN tanggung jawab
- Algoritma perhitungan (itu strategy registry atau use case)
- Workflow/approval (itu workflow engine; rule bisa jadi guard, tapi engine terpisah)

## Model data / tipe kunci

```go
type RuleTier int
const (
    TierNasional RuleTier = iota   // tertinggi, tak bisa di-override
    TierProvinsi
    TierKabKota
)

type Rule struct {
    ID            string
    Name          string
    Domain        string          // "keuangan.belanja"
    Tier          RuleTier
    TenantScope   []string        // tenant mana yang terikat (untuk provinsi/kabkota)
    EffectiveFrom time.Time
    EffectiveTo   *time.Time      // nil = berlaku selamanya
    Expression    string          // "belanja_perjadin / total_belanja <= 0.30"
    ErrorMessage  string
    Severity      Severity        // Error | Warning | Info
    Strict        bool            // true = tier bawah tak bisa melonggarkan
}
```

## Kebutuhan fungsional

### F1 — Rule store
- CRUD rule di gov.rule_versions.
- Setiap perubahan = entri baru (versioned), tidak overwrite.
- Query rule aktif untuk (domain, tenant, tanggal).

### F2 — Expression evaluator
- Evaluasi expression terhadap konteks data (field transaksi, agregat).
- Output boolean (constraint terpenuhi/tidak) atau nilai (untuk batas terhitung).
- Sama dengan guard DSL: tanpa side-effect, deterministik, di-compile saat load.
- Severity menentukan aksi: Error → tolak transaksi; Warning → izinkan + catat; Info → catat.

### F3 — Tiered constraint
- Hierarki: Nasional > Provinsi > KabKota.
- Tier bawah boleh MENAMBAH constraint (memperketat), TIDAK boleh melonggarkan tier atas.
- Resolusi: untuk satu domain, kumpulkan rule semua tier yang berlaku untuk tenant ini;
  tier bawah hanya valid bila tidak melonggarkan tier atas (divalidasi saat aktivasi).
- Contoh: Nasional "perjadin <= 30%"; Provinsi boleh set 25% (lebih ketat, OK); KabKota
  di provinsi itu tak bisa set 28% (melonggarkan provinsi, ditolak).

### F4 — Versioning & effective date
- Rule berlaku per rentang tanggal. Transaksi dievaluasi terhadap rule yang berlaku
  pada tanggal transaksi (bukan tanggal sekarang).
- Backtest: bisa evaluasi data historis terhadap rule yang berlaku saat itu.
- Non-retroaktif: rule baru tidak mengubah validitas data periode terkunci.

### F5 — Conflict detector
- Sebelum aktivasi, cek apakah rule baru bertentangan dengan rule aktif lain
  (mis. dua rule pada domain sama dengan batas yang mustahil dipenuhi bersamaan).
- Konflik terdeteksi → tolak aktivasi dengan pesan menyebut rule yang bertabrakan.

### F6 — Custom evaluator (Go)
- Untuk logika yang tak bisa diekspresikan DSL sederhana, registrasi Go evaluator:
  `rules.Register("custom.nama", evaluator)`.
- Engine memanggil custom evaluator seperti rule biasa.
- Ini titik ekstensi — menambah evaluator tidak mengubah engine.

## Kebutuhan non-fungsional
- Evaluasi rule: < 5ms (ter-compile).
- Reload rule (runtime, setelah aktivasi): tanpa downtime.
- Audit setiap aktivasi/perubahan rule.

## Dependency
- port/ (stdlib + interface umum)
- core/strategy — opsi strategy difilter oleh rule tier (irisan)
- Event bus — broadcast perubahan rule untuk invalidasi cache

## Anti-pattern / yang harus dihindari
- Menaruh algoritma di expression rule (bukan kondisi). Algoritma → strategy/custom eval.
- Rule tanpa effective date → retroaktif, berbahaya untuk data terkunci.
- Mengizinkan tier bawah melonggarkan tier atas.
- Mengeksekusi kode arbitrary dari DB — hanya DSL boolean/nilai.

## Keputusan tertunda
- Bentuk konkret "konteks data" yang dibawa ke evaluator (snapshot entity + agregat
  terhitung) — dispesifikasi saat integrasi dengan modul keuangan.
- Apakah Warning severity memblokir submit atau hanya mencatat — sementara: tidak
  memblokir, hanya catat + tampilkan ke user.

## Acceptance criteria
- [ ] Rule dievaluasi benar terhadap konteks (boolean & nilai).
- [ ] Severity Error menolak transaksi; Warning mengizinkan + catat.
- [ ] KabKota tidak bisa set constraint lebih longgar dari Provinsi (ditolak aktivasi).
- [ ] Provinsi bisa memperketat constraint Nasional.
- [ ] Transaksi dievaluasi terhadap rule yang berlaku pada tanggalnya (bukan tanggal kini).
- [ ] Rule baru tidak mengubah validitas data periode terkunci.
- [ ] Rule konflik terdeteksi & ditolak saat aktivasi dengan pesan jelas.
- [ ] Custom Go evaluator terdaftar & terpanggil engine.
