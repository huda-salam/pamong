# ADR-009: Klasifikasi data & enkripsi field selektif (blind index)

## Status
Accepted

## Konteks
Regulasi pemerintah (UU 27/2022 PDP, PP 71/2019 PSTE, arahan kriptografi BSSN/SPBE)
dan praktik industri (SNI ISO/IEC 27001 kontrol kriptografi) menuntut "langkah teknis
pelindungan yang memadai" atas data pribadi. Tidak ada regulasi yang menyebut "enkripsi
kolom database" secara eksplisit — yang diminta adalah pelindungan memadai. Karena itu
strateginya dipilih berlapis, dan lapisan yang menyentuh desain domain diputuskan di sini.

"Enkripsi sampai level DB" sebenarnya empat lapis dengan biaya & dampak berbeda:

- **L1 in-transit** — TLS ke Postgres (`sslmode=verify-full`). Biaya ~0, tanpa dampak kode.
- **L2 at-rest fisik** — LUKS/dm-crypt + backup terenkripsi (mis. `pgBackRest
  --repo-cipher-type`). Biaya rendah, murni ops, tanpa dampak kode. Postgres community
  tak punya TDE bawaan; L2 secara praktis setara untuk tujuan kepatuhan.
- **L3 field-level** — enkripsi app-side per-kolom. **Satu-satunya lapis yang menyentuh
  domain definition.** Melindungi dari DBA nakal, `pg_dump` bocor, log query, replikasi.
- **L4 key management** — envelope, KMS, rotasi. Menentukan apakah L3 bernilai. Diputuskan
  terpisah di **ADR-010** sebagai seam pluggable (KMS = driver ber-registry, custody =
  kebijakan per-tenant), sehingga pilihan KMS/custody konkret tak memblokir desain di sini.

L1+L2 memenuhi mayoritas checklist auditor dengan biaya nyaris nol dan **bukan** subjek
ADR ini (murni ops). ADR ini memutuskan **L3**: bentuknya, cakupannya, dan pengaruhnya
ke domain definition.

Masalah inti L3: kolom terenkripsi kehilangan `WHERE =`, `LIKE`, `ORDER BY`, index range,
`JOIN`, `UNIQUE`, dan agregasi. Ini bentrok langsung dengan kode yang sudah ada — `id.persons`
(`nik UNIQUE`), `id.employments` (`nip UNIQUE`), `id.credentials` (`UNIQUE(cred_type,
cred_value)`) semuanya melakukan lookup equality + menegakkan keunikan atas pengenal.

Kebutuhan yang disampaikan: **tidak semua data dienkripsi — yang perlu umumnya nomor
identifikasi.** Ini menolak pendekatan "enkripsi semua PII" (yang mahal dan mematikan
pencarian nama) dan mengarah ke enkripsi **selektif berbasis klasifikasi**.

## Keputusan

### 1. Satu sumbu klasifikasi `DataClass`, bukan flag per-concern

Alih-alih menambah boolean terpisah (`Sensitive`, `Encrypted`, `NoLog`, `NoExport`) yang
mudah salah dikombinasikan developer modul, ditambahkan **satu** penanda deklaratif di
`FieldDef`. Semua perilaku (enkripsi, perlakuan audit, log, export) diturunkan darinya
lewat **tabel kebijakan milik framework** — bukan keputusan per-field milik modul.

```go
type DataClass string
const (
    DataPublic     DataClass = "public"      // default (zero value diperlakukan sbagai ini)
    DataInternal   DataClass = "internal"
    DataPersonal   DataClass = "personal"    // PDP umum: nama, alamat, jabatan
    DataPersonalID DataClass = "personal_id" // pengenal unik: NIK, NIP, no_hp, email, no rekening
    DataSpecific   DataClass = "specific"    // PDP spesifik: kesehatan, keuangan pribadi, biometrik
)
```

Tabel kebijakan (ditegakkan framework, bukan modul):

| Class | Enkripsi | Audit diff | Log/trace | List/export API |
|---|---|---|---|---|
| `public` | – | apa adanya | ok | ok |
| `internal` | – | apa adanya | ok | permission |
| `personal` | – | raw, read-gated (ADR-002) | redacted | mask |
| `personal_id` | **enc + blind index** | raw **terenkripsi**, read-gated | redacted | mask 4 digit |
| `specific` | **enc, DEK terpisah** | referensi saja | never | permission ketat + audit **baca** |

**Cakupan enkripsi L3 = hanya `personal_id` dan `specific`.** Nama/alamat (`personal`)
**tidak** dienkripsi: harus bisa dicari, dan nama pegawai negeri semi-publik — rasio
proteksi/biaya buruk. Ini realisasi "tidak semua data, yang perlu nomor identifikasi".

### 2. Enkripsi = AES-256-GCM; equality & UNIQUE lewat blind index

Satu field logis terenkripsi → **dua kolom fisik**:

```
{field}_enc   BYTEA   -- AES-256-GCM, nonce acak per-nilai → tak bisa dicari, bisa dibaca
{field}_bidx  BYTEA   -- HMAC-SHA256(key_bidx, normalize(nilai)) → UNIQUE + lookup equality
```

- Kunci HMAC hidup di KMS, **tidak** di DB → penyerang yang hanya dapat dump tak bisa
  membangun dictionary (ruang NIK 16 digit cukup kecil untuk brute-force bila kunci ikut bocor).
- Kunci blind-index **terpisah** dari kunci enkripsi. Rotasi kunci bidx mahal (reindex
  seluruh baris) — disadari sebagai konsekuensi, bukan kejutan.
- Kolom `_bidx` yang memegang `UNIQUE`, bukan `_enc` (GCM nonce acak membuat dua ciphertext
  dari plaintext sama berbeda — mustahil untuk unique constraint).

Yang **tetap tidak bisa** meski blind index: range/sort atas kolom terenkripsi
(`tgl_lahir BETWEEN`), pencarian parsial (`nama LIKE '%budi%'`). Konsekuensi diterima:
enkripsi **wajib selektif**; field yang butuh range/partial tak boleh diklasifikasi terenkripsi.

### 3. Format ciphertext self-describing (mengunci rotasi sejak awal)

Ciphertext menyimpan metadata untuk memungkinkan rotasi kunci tanpa migrasi format:

```
v1 | key_id | key_version | nonce | ciphertext+tag
```

Tanpa ini rotasi kunci mustahil. Formatnya dikunci sejak baris pertama karena mahal
diubah setelah ada data.

### 4. Enkripsi hidup di lapis repository, digerakkan `EntityDef.Class` — bukan use case

Enkripsi/dekripsi dipanggil **otomatis** oleh lapis repository berdasarkan klasifikasi
field, persis seperti audit & optimistic locking yang sudah otomatis di
`infra/db/audited_repository.go`. **Bukan** dipanggil use case — bila use case yang
memanggil, developer modul pasti lupa. Konsisten dengan "rails, bukan kebebasan".

Seam kripto: `port/crypto.go` (port baru, lintas-modul — perubahan butuh ADR, ini ADR-nya):

```go
type CryptoPort interface {
    Encrypt(ctx context.Context, tenantID, purpose string, plain []byte) ([]byte, error)
    Decrypt(ctx context.Context, tenantID string, ct []byte) ([]byte, error)
    BlindIndex(ctx context.Context, tenantID, purpose string, plain []byte) ([]byte, error)
}
```

Implementasi di `infra/crypto/` (vault-transit, local-dev untuk test/dev, KMS lain kelak).
Domain & use case tetap nol-dependency kripto — mengikuti pola `OTPCodec`/`PasswordVerifier`
(ADR-008). `purpose` memisahkan konteks kunci (mis. `nik` vs `no_rekening`) tanpa mengubah port.

### 5. Perubahan `FieldDef` & DDL generator

- `FieldDef` menambah `Class DataClass` + `Searchable bool`. Struct literal lama tetap
  kompilasi (field baru zero-value → `public`, `Searchable=false`).
- `Validate()` menolak: `Searchable` pada tipe non-`Text`; `Unique` pada field terenkripsi
  yang `!Searchable` (mustahil ditegakkan tanpa blind index).
- DDL generator (`infra/db/ddl.go`): `columnDef` berubah dari 1 kolom → **N kolom** untuk
  field terenkripsi (`_enc` + `_bidx`). Ini perubahan paling invasif — memecah asumsi
  "1 field = 1 kolom". Dipanggil di satu titik (`ddl.go` loop `e.Fields`).
- Generated CRUD Tier 1: field terenkripsi tak masuk daftar sortable/filterable kecuali
  equality; ditolak saat `pamongctl validate`, bukan gagal runtime.
- Custom field tenant (`core/customization`): UI wajib menanyakan class; default aman `internal`.
- Linter rule baru `encrypted-field-no-raw-query`: repo Tier 3 tulis-tangan yang menyentuh
  kolom `_enc` mentah tanpa helper framework → reject.

### 6. Menutup jalur kebocoran samping (prasyarat, bukan opsional)

Enkripsi satu kolom sia-sia bila nilai mentah bocor lewat jalur lain. Implementasi L3
**wajib** menutup semua ini sekaligus — bukan pekerjaan terpisah:

1. **`gov.audit_logs.diff` (JSONB)** — field class `personal_id`+ ikut terenkripsi di diff
   (lihat §Hubungan dengan ADR-002). Ini lubang E2 di REVIEW_BACKLOG.
2. **Payload event** (NATS stream) — pengenal di payload di-mask/enc.
3. **Cache idempotency** (`gov.idempotency_keys`) — response tersimpan tak boleh muat pengenal mentah.
4. **Staging table** pipeline migrasi legacy — data mentah; enkripsi saat commit ke production.
5. **Log & trace** (OTEL span, query log Postgres) — pengenal `redacted`/`never` per tabel §1.
6. **Clone `gov.user_profiles`** — NIK/NIP di clone tenant ikut terenkripsi.

### Hubungan dengan ADR-002
ADR-002 memutuskan **simpan nilai mentah + kontrol akses saat baca** (bukan mask saat tulis),
demi bukti before/after untuk pemeriksa (BPK). **Keputusan inti itu tetap berlaku.** Yang
diperbarui ADR ini hanya **mekanismenya**: penanda `Sensitive bool` yang direncanakan ADR-002
digantikan sumbu `DataClass` yang lebih kaya; dan untuk class `personal_id`+, kolom `diff`
yang memuat nilai raw **ikut terenkripsi** (raw tetap ada sebagai bukti, tapi tak terbaca
tanpa kunci + permission). ADR-002 diberi pointer "Rencana implementasi diperbarui oleh
ADR-009"; keputusannya tidak di-supersede.

## Konsekuensi
- Domain definition berubah minimal **sekarang** (dua field di `FieldDef`, `columnDef`
  multi-kolom) — nyaris gratis karena satu-satunya konsumen produksi `EntityDef` hari ini
  (`modules/surat_masuk`) tak punya kolom identifier. Biaya naik tiap entity ber-pengenal
  baru; karena itu `DataClass` masuk `FieldDef` sekarang meski enkripsi diimplementasi kelak.
- `UNIQUE` atas pengenal pindah ke kolom `_bidx` lewat migration baru. **Gratis selama belum
  ada data produksi**; setelahnya butuh dual-write + backfill (beda ordo besaran).
- Range/sort/partial-search atas field terenkripsi hilang permanen — mengikat desain: field
  demikian tak boleh diklasifikasi terenkripsi.
- Rotasi kunci bidx = reindex seluruh baris (mahal, jarang). Rotasi kunci enkripsi murah
  berkat format self-describing.
- Inventaris data (`docs/contracts/data-inventory.md`) bisa di-generate dari `Class` di
  manifest → artefak kepatuhan UU PDP yang tak pernah basi.

## Alternatif yang dipertimbangkan
- **Enkripsi semua PII (termasuk nama/alamat).** Mematikan pencarian nama, butuh
  order-preserving encryption yang membocorkan urutan, biaya besar untuk proteksi kecil pada
  data semi-publik. Ditolak — enkripsi selektif berbasis class.
- **TDE / enkripsi seluruh tablespace.** Tak ada di Postgres community; L2 (LUKS + backup enc)
  memberi proteksi at-rest setara untuk kepatuhan tanpa biaya kode. Dipakai sebagai L2, bukan
  pengganti L3 (tak melindungi dari DBA/`pg_dump`/replikasi).
- **Flag boolean per-concern (`Encrypted`, `Sensitive`, `NoLog`).** Kombinatorik salah,
  developer modul lupa mengkombinasikan. Ditolak — satu sumbu `DataClass`.
- **Enkripsi dipanggil di use case.** Developer modul pasti lupa; bocor lewat jalur yang tak
  memanggil. Ditolak — otomatis di lapis repository.
- **Deterministic encryption (tanpa blind index) untuk equality.** Membocorkan kesamaan nilai
  (dua orang NIK sama terlihat), rentan frequency analysis. Ditolak — GCM nonce acak + HMAC bidx.
