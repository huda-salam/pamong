# ADR-005: Data residency entity — tenant vs central

## Status
Accepted

## Konteks
Model persistensi framework mengasumsikan **semua entity tinggal di tenant DB**
(schema-per-module, DB-per-tenant). `EntityDef` hanya punya `Schema` + `Tablename`,
tanpa cara mendeklarasikan di DB mana datanya tinggal.

Asumsi ini tidak cukup. Akan ada **data master/referensi nasional** yang dipakai SEMUA
tenant: kode wilayah (Kemendagri), bagan akun standar, jenis belanja nasional, satuan
ukur, dll. Menggandakan data ini ke setiap tenant DB salah — ia harus **sentral, shared**.
Identity (person/employment/credential) lolos dari masalah ini hanya karena ditulis
tangan di `identity/`, bukan lewat `EntityDef`. Begitu data referensi memakai jalur
EntityDef (Tier 1 generated CRUD), framework harus tahu residency-nya untuk merutekan
koneksi & migrasi ke DB yang benar.

## Keputusan
`EntityDef` mendapat atribut **residency**: di mana data entity tinggal.

```go
type DataResidency int
const (
    ResidencyTenant  DataResidency = iota // default: per-tenant DB (schema-per-module)
    ResidencyCentral                       // DB sentral, shared semua tenant (master/referensi)
)
```

- **Default `ResidencyTenant`** (kasus ~95%). Berbeda dari Audit/Lockable yang wajib
  eksplisit: salah residency = data tak ter-share (bug fungsional), bukan bahaya
  keamanan/kepatuhan — jadi default aman dapat diterima.
- **`ResidencyCentral`** opt-in untuk entity master/referensi.
- Residency **ortogonal** terhadap penamaan tabel: `{schema}.{plural}` tetap berlaku;
  residency hanya menentukan DATABASE/pool mana yang dipakai.

**Penempatan fisik "central":** dimodelkan sebagai residency logis + satu **central pool**
yang dikonfigurasi terpisah (`CentralDBConfig`, env `GOV_CENTRAL_DB_*`). Secara fisik
**dimulai dari instance identity DB** (`gov_identity` — "satu-satunya yang shared" per
CLAUDE.md) untuk menghindari infra baru: bila `GOV_CENTRAL_DB_*` kosong, central pool
jatuh ke koneksi identity DB. Abstraksi ini memungkinkan pemisahan ke `gov_central`
khusus nanti tanpa mengubah kode domain.

## Konsekuensi
- `EntityDef.Residency` + helper `IsCentral()` ditambahkan sekarang (metadata murni);
  belum ada perubahan perilaku karena konsumennya (routing koneksi, migrasi, CRUD
  generator) dibangun kemudian. Menambah field sekarang mencegah retrofit EntityDef.
- `TenantConnManager` (PR-2.2.3) akan memilih **central pool vs tenant pool** berdasarkan
  `Residency`. Migration runner menjalankan migrasi entity central sekali ke central DB;
  entity tenant ke tiap tenant DB.
- `CentralDBConfig` ditambahkan ke config dengan fallback ke identity DB bila kosong —
  ops tidak wajib mengkonfigurasi dua blok identik sekarang.
- `surat_masuk` & entity lain yang sudah ada tidak terdampak (default tenant).

## Alternatif yang dipertimbangkan
- **Tanpa atribut residency; perlakukan data referensi sebagai kasus khusus hand-coded
  (seperti identity).** Ditolak: data referensi pas dengan Tier 1 generated CRUD; memaksa
  hand-code tiap tabel referensi membuang nilai utama framework.
- **DB sentral terpisah (`gov_central`) wajib sejak awal.** Ditolak untuk sekarang:
  menambah satu koneksi & ops tanpa kebutuhan mendesak. Residency logis + fallback ke
  identity DB memberi jalan upgrade tanpa biaya di muka.
- **Residency wajib eksplisit (seperti Audit/Lockable).** Ditolak: tenant adalah default
  yang benar untuk mayoritas entity; salah residency tidak berbahaya secara keamanan.
  Default-tenant lebih ergonomis.
