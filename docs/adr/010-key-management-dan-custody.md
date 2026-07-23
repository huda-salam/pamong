# ADR-010: Key management — provider pluggable & custody sebagai kebijakan

## Status
Accepted

## Konteks
ADR-009 memutuskan enkripsi field selektif (L3) dengan AES-256-GCM + blind index, di atas
envelope encryption KEK→DEK. Enkripsi hanya bernilai sebaik pengelolaan kuncinya (L4). ADR
ini memutuskan **bentuk** key management.

Dua pertanyaan sempat muncul: (1) KMS mana yang dipakai (Vault Transit, AWS/GCP KMS,
infrastruktur kripto BSSN, dll), dan (2) di Tier 3 (server milik pemda) siapa pegang KEK —
platform atau pemda. Keduanya bergantung pada konteks pengadaan & kontrak yang **belum
tersedia saat desain** dan bisa berbeda antar pemda.

Memaksa jawaban tunggal sekarang salah dua arah: menebak KMS bisa keliru saat pengadaan
menentukan lain; menetapkan custody global mengabaikan bahwa tiap pemda bisa punya
kebijakan berbeda. Keputusan yang benar: **jangan putuskan nilainya — putuskan seam-nya**,
sehingga nilai konkret bisa di-plug belakangan tanpa mengubah kode. Ini penerapan titik
ekstensi #1 (registry/port) pada kripto.

Konteks arsitektur yang mengikat: DB-per-tenant (ADR-004); tenant tier 1→2→3 (kode aplikasi
tak berubah antar tier); data residency (ADR-005).

## Keputusan

### 1. KMS = driver ber-registry, persis pola eventbus/storage
KMS tidak di-hardcode. Ia adalah **driver yang dipilih lewat config**, seperti
`GOV_EVENTBUS_DRIVER` dan `GOV_STORAGE_DRIVER` yang sudah ada:

```
GOV_CRYPTO_KMS_DRIVER=static    # static | local | vault | aws-kms | gcp-kms | bssn | ...
GOV_CRYPTO_KMS_ENDPOINT=...      # untuk driver yang butuh (vault/aws-kms/...)
```

Driver yang disediakan framework:
- **`static` — KMS-alike bawaan, default produksi Tier 1/2.** Master KEK dari secret source
  (`GOV_CRYPTO_MASTER_KEY`, base64 32-byte, atau file/secret manager — **bukan** dari config
  ter-commit), ber-versi untuk rotasi. Envelope dilakukan in-app; DEK ter-wrap disimpan
  sentral (`id.data_keys`). Tanpa dependensi eksternal → pemda tanpa HSM/Vault tetap jalan.
  Postur keamanan: keamanan turun ke perlindungan master key (ops L2 — secret store, file
  perms, tak di log/DB), jauh di atas plaintext. Naik ke HSM/Vault = ganti driver, tanpa kode.
- **`local`** — dev/test saja (kunci config, tanpa versi), ditolak bila `GOV_ENV=production`.
- **`vault` / `aws-kms` / `gcp-kms` / `bssn` / ...** — di-plug saat pengadaan menentukan.

Seam internal (di balik `port.CryptoPort` ADR-009), didaftarkan ke registry:

```go
// KeyProvider — abstraksi KMS. Membungkus/membuka DEK; tak pernah membocorkan KEK.
type KeyProvider interface {
    WrapDEK(ctx context.Context, keyRef KeyRef, dek []byte) ([]byte, error)
    UnwrapDEK(ctx context.Context, keyRef KeyRef, wrapped []byte) ([]byte, error)
    GenerateDEK(ctx context.Context, keyRef KeyRef) (plain, wrapped []byte, err error)
}
```

Menambah KMS baru = tulis satu implementasi + `Register("nama", provider)`. Kode kripto
(ADR-009) tidak berubah — ia bergantung pada interface, bukan vendor. Framework mengapal
driver `static` yang **layak produksi tanpa infra tambahan** sebagai default, sehingga
enkripsi bisa aktif sejak tenant pertama; driver KMS eksternal di-plug bila/ketika
pengadaan menentukannya, tanpa migrasi kode.

### 2. Envelope encryption — hierarki tiga lapis (vendor-agnostik)
```
KEK  — hidup di KeyProvider (KMS/HSM), tak pernah keluar
  └── DEK — per-tenant per-purpose; disimpan ter-wrap
        └── data field — AES-256-GCM dengan DEK (ADR-009)
```
- **DEK per-tenant per-purpose.** Per-tenant wajib (DB-per-tenant + Tier 3: satu tenant
  bocor tak membuka tenant lain). Per-purpose (`nik`, `no_rekening`) membatasi blast radius.
- DEK ter-wrap disimpan **sentral** (bukan tenant DB) → dump tenant DB tak memuat kunci.
- `KeyRef` mengidentifikasi (tenant, purpose, custody) tanpa mengasumsikan vendor —
  pemetaan `KeyRef` → lokasi kunci nyata adalah urusan tiap driver.

### 3. Custody = kebijakan per-tenant (config), bukan keputusan global di kode
Siapa pegang KEK ditetapkan sebagai **field kebijakan per-tenant**, bukan dibakar ke desain:

```
key_custody: platform | tenant      # di tenant_registry / tenant config
```

- `platform` — KEK di KeyProvider yang dikelola platform (default Tier 1/2).
- `tenant` — KEK di KeyProvider milik pemda (mis. Vault/HSM pemda pada Tier 3).

Resolver kripto membaca custody tenant lalu memilih `KeyProvider` yang sesuai. Menambah
mode/penyedia custody baru = registrasi driver + nilai config — **tanpa ubah kode kripto**.
Karena itu keputusan custody Tier 3 tak perlu diambil sekarang: begitu kontrak sebuah pemda
jelas, set `key_custody` + driver-nya. Trade-off (platform pegang → pemda tak bisa baca DB
sendiri tanpa platform; pemda pegang → pemda berdaulat tapi bertanggung jawab atas kunci)
menjadi pilihan **per-tenant saat onboarding**, tertulis di kontrak masing-masing —
bukan invariant framework.

### 4. Rotasi (didukung format ciphertext ADR-009)
- **Rotasi KEK**: murah — re-wrap DEK; data tak disentuh (`key_version` di ciphertext).
- **Rotasi DEK enkripsi**: lazy — baca lama pakai versi lama, tulis pakai versi baru.
- **Rotasi blind-index key**: mahal — reindex seluruh baris. Hanya saat kompromi,
  didokumentasikan, bukan rutin.

## Konsekuensi
- Tidak ada keputusan KMS/custody yang tertunda **memblokir** implementasi maupun produksi.
  Driver `static` bawaan sudah layak produksi Tier 1/2 tanpa infra tambahan; enkripsi
  (ADR-009) bisa aktif sejak tenant pertama. Driver KMS eksternal opsional, di-plug bila ada.
- Konsekuensi driver `static`: keamanan kunci = keamanan master key (ops). Ini postur yang
  diterima sadar untuk Tier 1/2; pemda yang menuntut HSM/Vault tinggal ganti driver.
- Tier 3 **tidak di-gate** oleh keputusan arsitektur — ia di-gate oleh tersedianya driver +
  nilai `key_custody` untuk tenant itu (urusan onboarding/kontrak per-pemda).
- `port.CryptoPort` (ADR-009) sudah menerima `tenantID`; `KeyProvider` + `KeyRef`
  menambah lapis di baliknya tanpa mengubah port yang dilihat repository.
- Custody menjadi item onboarding/kontrak per-tenant, bukan invariant global.
- Escrow/exit kunci (bila `key_custody=platform` pada Tier 3) tetap perlu klausa kontrak —
  itu urusan legal, dan desain tak menghalangi mode manapun.

## Alternatif yang dipertimbangkan
- **Pilih satu KMS sekarang (mis. Vault).** Menebak sebelum pengadaan; berisiko keliru dan
  memaksa refactor. Ditolak — driver ber-registry.
- **Custody global tunggal di kode.** Mengabaikan bahwa tiap pemda bisa beda kebijakan;
  memaksa keputusan tanpa info. Ditolak — custody sebagai kebijakan per-tenant.
- **Satu DEK global semua tenant.** Melanggar isolasi DB-per-tenant. Ditolak — DEK per-tenant.
- **Kunci disimpan di tenant DB.** Dump membuka data. Ditolak — kunci di KMS/sentral,
  tenant DB hanya memuat ciphertext.
