# ADR-016: Pengikatan BARIS ciphertext lewat AAD (`FieldRef`/`RowRef`)

## Status
Accepted — **memperluas ADR-009 §3/§4 dan menutup sisa risiko yang dicatat ADR-015**;
tidak men-supersede ADR-009, ADR-010, maupun ADR-015. Seluruh keputusan ketiganya tetap
berlaku, termasuk pemeriksaan `PurposeOf` di lapis repository (ADR-015) yang tetap
dibutuhkan dan tidak digantikan oleh ADR ini.

## Konteks
ADR-015 menutup perpindahan ciphertext antar **KOLOM** dengan memeriksa
`PurposeOf(ct) == purpose kolom` di lapis repository, dan mencatat sisa risikonya secara
eksplisit: pengikatan itu per-kolom, **bukan per-baris**.

Yang masih lolos setiap pemeriksaan hari ini:

```sql
-- dua pegawai dalam tenant & tabel yang sama
UPDATE kepegawaian.pegawais a
SET nik_enc  = b.nik_enc,
    nik_bidx = b.nik_bidx
FROM kepegawaian.pegawais b
WHERE a.id = '…budi…' AND b.id = '…siti…';
```

Sesudah itu aplikasi membaca NIK Siti sebagai NIK Budi. Purpose blob tetap `nik`
(pemeriksaan ADR-015 lolos), tenant tetap sama (AAD lolos), tag GCM tetap sah (isinya
tidak diubah). Blind index ikut dipindah sehingga pencarian pun konsisten dengan
kebohongannya — tidak ada satu pun lapis yang protes.

Ini kelas ancaman yang sama dengan ADR-015 (L3: pihak ber-akses DB tanpa akses kunci),
dan akibatnya lebih berat daripada perpindahan antar kolom: nilainya benar, **atribusinya**
yang palsu. Untuk sistem pemerintahan, "NIK ini milik siapa" adalah justru pertanyaan yang
dijawab data pengenal.

Penyebabnya satu: AAD hanya mengikat `(tenant, purpose, key_version)`. Tidak ada satu pun
komponen AAD yang berbeda antar baris.

**Kenapa sekarang.** Retrofit ini murah hanya selama belum ada data produksi. Ia mengubah
kontrak `Encrypt`/`Decrypt`, sehingga setiap jalur baru yang memanggil kripto (identity
PR-3.8.6, payload event & idempotency PR-3.8.5, driver KMS PR-3.8.8) adalah satu tempat
lagi yang harus ikut berubah bila ditunda. Hari ini pemanggil produksinya tepat empat.

## Keputusan

### 1. Identitas baris masuk ke AAD, disuplai pemanggil

AAD berubah dari `(tenant, purpose, key_version)` menjadi
`(tenant, purpose, key_version, record_id)`.

`record_id` **tidak** disimpan di dalam blob. Ia harus datang dari pemanggil pada saat
dekripsi — persis seperti `tenantID` — karena di situlah letak penegakannya: nilai yang
dipasang di baris lain akan diminta dibuka dengan `record_id` baris itu, dan AEAD menolak.
Menyimpannya di dalam blob justru membatalkan seluruh gunanya (blob yang dipindah akan
membawa serta "bukti" identitasnya sendiri).

### 2. Bentuk kontrak: dua tipe koordinat, bukan deretan parameter string

```go
// FieldRef adalah koordinat lengkap satu nilai terenkripsi — dipakai saat MENULIS.
type FieldRef struct {
    TenantID string // hierarki kunci (ADR-010 §2)
    Purpose  string // konteks kunci; default framework = nama kolom
    RecordID string // identitas BARIS pemilik nilai
}

// RowRef adalah bagian koordinat yang WAJIB disuplai pemanggil saat MEMBACA.
// Purpose & key_version tidak ada di sini: keduanya dibaca dari blob (ADR-009 §3).
type RowRef struct {
    TenantID string
    RecordID string
}

Encrypt(ctx context.Context, ref FieldRef, plain []byte) ([]byte, error)
Decrypt(ctx context.Context, ref RowRef, ct []byte) ([]byte, error)
PurposeOf(ct []byte) (string, error)                                      // tak berubah
BlindIndex(ctx context.Context, tenantID, purpose string, plain []byte)   // tak berubah
```

Struct bernama, bukan `Encrypt(ctx, tenantID, purpose, recordID string, …)`: tiga parameter
string berurutan di seam keamanan adalah undangan tertukar, dan tertukarnya `purpose`
dengan `recordID` menghasilkan kunci per-baris (satu DEK per pegawai) — kesalahan yang
akan tampak seperti "lambat", bukan seperti "salah".

`RowRef` sengaja TIDAK memuat `Purpose`. Menaruhnya di sana akan menggoda `Decrypt` untuk
sekaligus menegakkan pengikatan kolom, dan itu justru alternatif yang sudah ditolak
ADR-015: jalur baca audit tidak tahu kolom asal sebuah nilai, ia mengenali nilai sensitif
dari bentuknya. Pengikatan kolom tetap di repository, pengikatan baris di AEAD.

### 3. `BlindIndex` TIDAK ikut terikat baris

Ini bukan kelalaian melainkan syarat: blind index harus row-independent, kalau tidak
`WHERE nik_bidx = $1` tak akan pernah cocok dan UNIQUE tak akan pernah menangkap duplikat.
Asimetri antar dua metode ini adalah dokumentasinya sendiri — `BlindIndex` menerima
`(tenantID, purpose)` telanjang, tanpa `FieldRef`, sehingga tak ada tempat untuk
menyelipkan `RecordID` tanpa sadar.

### 4. AAD ber-length-prefix, bukan gabungan ber-pemisah

AAD lama disusun `fmt.Sprintf("pamong/field/v1|%s|%s|%d", …)`. Dengan bertambahnya
komponen ber-nilai bebas, pemisah `|` menjadi ambigu: `RecordID` tidak selamanya UUID
(jalur idempotency di PR-3.8.5 ber-kunci string dari klien), dan nilai yang memuat `|`
bisa menghasilkan AAD yang sama untuk dua koordinat berbeda. Setiap komponen karena itu
ditulis sebagai `uint32(len) || bytes`.

### 5. Versi format ciphertext naik `0x01` → `0x02`

Tata letak blob tidak berubah (`record_id` ada di AAD, bukan di blob), jadi secara teknis
byte versi bisa dibiarkan. Ia tetap dinaikkan supaya blob pra-pengikatan **dikenali**,
bukan sekadar gagal: tanpa itu, ciphertext lama gagal dengan pesan tag GCM generik yang
tak bisa dibedakan dari "blob rusak" atau "kunci salah" — persis saat operator butuh tahu
bahwa yang diperlukan adalah re-enkripsi.

`parseCiphertext` tetap MENGENALI v1 (sehingga `PurposeOf` menjawab dan jalur baca audit
menampilkan penanda "tidak dapat dibuka", bukan blob mentah), tapi `Decrypt` MENOLAK-nya.
Menerima v1 sama dengan menerima ciphertext tak terikat baris — yaitu membiarkan celah ini
terbuka lewat pintu belakang "kompatibilitas".

### 6. `RecordID` kosong = gagal keras

Sama seperti `TenantID` kosong. Pemanggil yang tidak tahu baris mana yang ia enkripsi
tidak boleh mengenkripsi: hasilnya akan menjadi nilai yang bisa dipindah ke mana saja,
dan kegagalan itu tak akan terlihat sampai seseorang memindahkannya.

### 7. Dari mana `RecordID` diambil di tiap jalur

| Jalur | RecordID | Catatan |
|---|---|---|
| `infra/db` tulis (`writeValues`) | `Mapper.ID(entity)` | tersedia sebelum INSERT/UPDATE |
| `infra/db` baca (`decryptingScanner`) | kolom `id` baris itu sendiri | dibaca dari `dest[0]` setelah `Scan`, sebelum dekripsi |
| diff audit (`auditedRepo.sealPair`) | id entity yang dimutasi | sama dengan `RecordInput.EntityID` |
| baca audit (`audit.Reader`) | `AuditEntry.EntityID` | sudah ada di entry |

Jalur baca repository mengambil identitas baris **dari baris itu sendiri**, dan itu justru
yang membuatnya bekerja: blob yang dipindah ke baris lain akan diminta dibuka dengan id
baris tujuan, yang bukan id saat ia disegel.

## Konsekuensi

- Seluruh implementasi `CryptoPort` berubah tanda tangannya: `infra/crypto.Service`,
  `testkit.MockCrypto`, dan setiap driver KMS yang dipasang kelak. Ini biaya yang
  disengaja dibayar sekarang, saat pemanggilnya empat, bukan nanti saat identity, event,
  dan idempotency ikut memanggil.
- Ciphertext yang ditulis sebelum ADR ini tidak dapat dibuka. Tidak ada backfill yang
  disediakan karena belum ada tenant produksi — itulah gerbang keras yang ditetapkan
  ROADMAP 3.8. Bila ADR ini ternyata dikerjakan setelah ada data, yang dibutuhkan adalah
  job re-enkripsi per-baris (baca dengan kode lama, tulis dengan kode baru) dan itu bukan
  bagian dari ADR ini.
- **Mengubah `id` sebuah baris merusak nilai terenkripsinya.** Di framework ini `id` adalah
  UUID yang tak pernah berubah setelah dibuat, jadi konsekuensi ini teoretis — tapi ia
  menutup pintu untuk pola "ganti primary key saat merge data duplikat" tanpa re-enkripsi
  lebih dulu.
- **Sisa risiko yang tetap disadari:** menukar `_bidx` (tanpa `_enc`) antar dua baris masih
  mungkin. Akibatnya pencarian menemukan baris yang salah, tapi nilai yang dibaca dari
  baris itu tetap nilainya sendiri — kegagalan integritas indeks, bukan kebocoran maupun
  atribusi palsu. Menutupnya butuh blind index ber-baris, yang menghapus seluruh
  kegunaannya (lihat Keputusan §3). Pertahanan untuk kelas ini adalah kontrol akses tulis
  ke DB dan rekonsiliasi berkala, bukan kripto.
- Diff audit ikut terikat ke `EntityID`, jadi memindahkan nilai antar entry audit **entity
  berbeda** kini gagal dibuka. Memindahkan antar entry pada entity yang SAMA tetap mungkin
  dari sisi kripto — itu sudah dijaga hash chain (ADR-002), yang putus bila baris audit
  disentuh. Dua lapis untuk dua sisi ancaman yang berbeda.
- `PurposeOf` tetap dibutuhkan. Pengikatan baris tidak menggantikannya: memindahkan
  `nik_enc` ke `no_rekening_enc` **pada baris yang sama** lolos AAD (record_id-nya sama)
  dan hanya tertangkap oleh pemeriksaan purpose ADR-015.

## Alternatif yang dipertimbangkan

**Menyimpan `record_id` di dalam blob (seperti purpose & key_version).** Konsisten dengan
format self-describing ADR-009 §3 dan tak mengubah tanda tangan apa pun. **Ditolak karena
tidak mengamankan apa pun**: blob yang dipindah membawa serta `record_id` aslinya, AAD
tetap cocok, dekripsi tetap berhasil. Ia hanya akan memungkinkan repo *membandingkan*
`record_id` blob dengan id baris — pemeriksaan tingkat aplikasi yang bisa dilangkahi jalur
tulis mana pun yang lupa memeriksanya, alih-alih jaminan kriptografis. Perbedaan inilah
alasan `tenantID` pun disuplai pemanggil sejak ADR-009.

**Mengalirkan `RecordID` lewat `context` (`port.WithRecordID`).** Nol perubahan tanda
tangan; mengikuti pola `port.WithTenant` yang sudah ada. Ditolak karena arah salahnya
berbahaya: nilai yang hilang dari context menghasilkan enkripsi tak-terikat yang tetap
sukses (atau, bila digagalkan, gagal di tempat yang jauh dari penyebabnya). Tenant di
context masih dapat dibela karena ia berlaku untuk seluruh request; identitas baris
berubah **per nilai** dalam satu request yang sama — persis kategori data yang tidak boleh
hidup di context.

**Kunci turunan per-baris (`DEK_row = KDF(DEK, record_id)`) alih-alih AAD.** Memberi
pengikatan yang sama kuatnya. Ditolak karena membunuh cache DEK: satu operasi KDF dan satu
entri cache per baris, pada jalur yang justru mendekripsi ratusan baris per halaman daftar.
AAD memberi properti keamanan yang sama dengan nol biaya kunci.

**Membiarkan terbuka, andalkan hash chain audit untuk mendeteksi.** Ditolak karena audit
mencatat mutasi yang lewat aplikasi; `UPDATE` langsung ke DB — yang justru merupakan model
ancaman L3 — tidak menghasilkan entry audit apa pun untuk dirantai.

**Menunda sampai ada kebutuhan konkret.** Ditolak karena biayanya monoton naik dan
gerbangnya sudah ditetapkan ROADMAP 3.8: setelah ada data produksi, ini berarti re-enkripsi
seluruh baris berpengenal di semua tenant, ditambah migrasi seluruh driver KMS yang
sementara itu sudah terpasang.

## Rujukan
- ADR-009 §3 (format ciphertext), §4 (seam `CryptoPort`), §6 (jalur kebocoran samping)
- ADR-010 (custody & hierarki kunci per-tenant/purpose)
- ADR-015 (pengikatan KOLOM lewat `PurposeOf` — sisa risikonya ditutup di sini)
- ADR-002 (audit diff sebagai bukti; hash chain)
- `port/crypto.go`, `infra/crypto/crypto.go` (AAD & format), `infra/db/field_crypto.go`
  (aliran RecordID), `core/audit/reader.go` (jalur baca)
