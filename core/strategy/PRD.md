# PRD: Strategy Registry

## Tujuan
Menyediakan mekanisme "selectable strategy": satu titik keputusan dengan beberapa varian
algoritma/kebijakan sah yang dipilih per-tenant, tanpa menyimpan logika di database dan
tanpa percabangan if-else yang menyebar. Logika tetap kode Go ter-test; yang configurable
hanya pilihan (identifier).

## Konteks & batasan

### Jadi tanggung jawab
- Registry varian ber-key + resolusi pilihan tenant
- Validasi key & filter opsi (irisan dengan rule tier)
- Versioning pilihan (effective date, non-retroaktif)
- Hook validator koherensi kombinasi

### BUKAN tanggung jawab
- Implementasi algoritma itu sendiri (ditulis di modul masing-masing, didaftarkan ke sini)
- Susunan langkah/alur (itu workflow)
- Penyimpanan config mentah (delegasi ke core/config)

## Model data / tipe kunci

```go
// Decision point: titik keputusan, mis. "keuangan.persediaan.metode"
// Strategy key:    varian, mis. "keuangan.persediaan.fifo"

type StrategyRegistry interface {
    Register(key string, impl any) error
    Resolve(ctx context.Context, tenantID, decisionPoint string) (any, error)
    AvailableOptions(ctx context.Context, tenantID, decisionPoint string) ([]string, error)
}

type StrategyChoice struct {
    TenantID      string
    DecisionPoint string
    SelectedKey   string
    EffectiveFrom time.Time
    Version       int
}

// Validator koherensi: memeriksa kombinasi pilihan lintas decision point
type CoherenceValidator func(choices map[string]string) error
```

## Kebutuhan fungsional

### F1 — Registry ber-key
- Developer mendaftarkan implementasi: `Register("keuangan.persediaan.fifo", fifoImpl)`.
- Implementasi mematuhi interface yang didefinisikan modul untuk decision point itu.
- Resolve(tenant, decisionPoint) → ambil pilihan tenant → kembalikan implementasi.

### F2 — Validasi key
- Tenant hanya bisa memilih key yang terdaftar untuk decision point tsb.
- Key tak terdaftar → reject (tidak ada fallback diam-diam).
- Decision point tanpa pilihan tenant → gunakan default yang ditandai developer (bila
  ada), atau error bila wajib pilih.

### F3 — Filter opsi (irisan dengan rule tier)
- Opsi yang ditawarkan ke tenant = (varian terdaftar) ∩ (diizinkan rule tier nasional)
  ∩ (diizinkan rule tier provinsi).
- Contoh: LIFO terdaftar, tapi rule nasional melarang LIFO → LIFO tidak muncul sebagai
  opsi untuk tenant manapun.
- AvailableOptions mengembalikan hasil irisan ini.

### F4 — Versioning pilihan
- Perubahan pilihan = versi baru dengan effective_from.
- Resolusi pilihan untuk transaksi pakai pilihan yang berlaku pada tanggal transaksi.
- Non-retroaktif: ganti metode tidak mengubah perhitungan periode terkunci. Metode baru
  berlaku periode baru.
- Perubahan ber-permission + ter-audit.

### F5 — Validator koherensi kombinasi
- Titik untuk mendaftarkan CoherenceValidator: memeriksa apakah kombinasi pilihan tenant
  lintas decision point koheren (mis. pendekatan beban + metode tertentu = tak koheren).
- Dipanggil saat tenant mengubah pilihan; kombinasi tak koheren → tolak.
- Titik ini disiapkan meski belum tentu dipakai di awal (open for extension).

## Kebutuhan non-fungsional
- Resolve: < 2ms (pilihan di-cache per-tenant, invalidasi saat berubah).
- Register: saat boot/registrasi modul.

## Dependency
- port/strategy.go
- core/rules — untuk F3 (filter opsi); bisa di-stub dulu, lengkapi setelah rules siap
- core/config — penyimpanan & resolusi pilihan ber-scope
- Event bus — invalidasi cache saat pilihan berubah

## Anti-pattern / yang harus dihindari
- Menyimpan logika algoritma di DB (hanya key).
- if tenant.metode == "fifo" {...} else {...} — itu yang digantikan pola ini.
- Lupa memfilter opsi dengan rule tier → tenant memilih yang dilarang regulasi.
- Pilihan retroaktif → mengubah perhitungan periode terkunci.
- Mengira strategy untuk susunan langkah (itu workflow).

## Keputusan tertunda
- Scope pilihan: sekarang per-tenant; struktur key disiapkan untuk per-unit-kerja /
  per-resource (mis. metode persediaan per-gudang) tanpa ubah skema (lihat core/config).
- Bentuk default strategy (developer menandai satu varian default) — sementara opsional.

## Acceptance criteria
- [ ] Dua strategy ter-register; Resolve mengembalikan implementasi sesuai pilihan tenant.
- [ ] Key tak terdaftar → ditolak.
- [ ] AvailableOptions = irisan terdaftar ∩ rule tier; yang dilarang rule tak muncul.
- [ ] Ganti metode → periode terkunci tetap pakai metode lama; periode baru pakai baru.
- [ ] Perubahan pilihan ter-audit.
- [ ] Kombinasi tak koheren yang didaftarkan → ditolak.
