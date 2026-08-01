# ADR-017: Realm kunci sentral untuk data identity (`_central`)

## Status
Accepted — **memperluas ADR-010 §2 (hierarki DEK) dan ADR-009 §2 (blind index)**; tidak
men-supersede ADR-009, ADR-010, ADR-015, maupun ADR-016. Seluruh keputusannya tetap
berlaku apa adanya untuk data tenant.

## Konteks
ADR-010 §2 menetapkan hierarki kunci **KEK → DEK per-tenant per-purpose → data**, dengan
alasan yang tetap benar: DB-per-tenant (ADR-004) dan Tier 3 menuntut satu tenant bocor
tidak membuka tenant lain.

PR-3.8.6 memasang enkripsi field pada **identity DB** — `id.persons.nik`,
`id.employments.nip`, `id.credentials.cred_value`, `id.persons.no_hp`/`email` — dan
menutup jalur audit identity (`id.audit_logs.diff`, REVIEW_BACKLOG E2). Di sinilah
hierarki itu kehabisan sumbu: **data identity tidak punya tenant.**

Bukan "tenant-nya belum diketahui", melainkan tidak ada:

- **Person ada sebelum penugasan tenant mana pun.** Persona `citizen` tersedia untuk semua
  orang dan tidak pernah punya `tenant_assignment` (CLAUDE.md §Identity). NIK sudah harus
  tersimpan sebelum ada tenant untuk dijadikan kunci.
- **Satu person bisa melintas banyak tenant** (penugasan cross-tenant, mis. PJ Bupati).
  Tidak ada satu tenant yang "memiliki" NIK-nya.
- **Chain audit identity sudah mengakui ini** sejak ADR-003: seluruh entry dirantai dalam
  satu partisi sentinel `tenant_id = "central"` karena operasi identity sentral.

Ada satu batasan yang mempersempit pilihan sampai tinggal satu, dan ia bukan selera:

> **`UNIQUE(nik)` berlaku global se-identity-DB.** Setelah UNIQUE pindah ke `nik_bidx`
> (ADR-009 §2), kunci blind index yang per-tenant akan menghasilkan `nik_bidx` **berbeda**
> untuk NIK yang sama di tenant berbeda. Akibatnya UNIQUE berhenti menangkap duplikat —
> satu NIK bisa terdaftar berkali-kali — dan `FindByNIK` harus menyebut tenant yang ia
> memang tidak punya. Realm identity **wajib** memakai tepat satu kunci per purpose.

Jadi pertanyaan yang sebenarnya bukan "kunci tenant mana yang dipakai", melainkan
**bagaimana menyatakan realm sentral** di dalam sumbu partisi `(tenant_id, purpose, kind)`
yang sudah ada di `id.data_keys`.

Konteks kedua yang ikut menentukan: **`tenantIDRe = ^[a-z][a-z0-9-]{2,99}$` menerima string
`"central"`.** Sentinel chain audit hari ini persis nilai itu. Selama registry masih kosong
tidak ada akibatnya, tapi begitu sebuah pemda didaftarkan dengan `tenant_id = "central"`,
audit tenant itu melebur ke chain sentral — dan bila nilai yang sama juga dipakai sebagai
partisi DEK, ia ikut berbagi **ruang kunci**. ADR ini memilih bentuk yang menutup keduanya.

## Keputusan

### 1. Sumbu partisi kunci dibaca ulang sebagai **key realm**, bukan tenant

Kolom `id.data_keys.tenant_id` dan field `port.FieldRef.TenantID` **tidak berubah** —
tidak ada migrasi skema, tidak ada perubahan kontrak port. Yang berubah adalah cara
membacanya: nilai di sana adalah **identitas realm kunci**, dan realm ada dua jenis.

```
realm tenant   = <tenant_id>   ← seluruh data tenant; persis ADR-010 §2, tak berubah
realm sentral  = "_central"    ← data identity + chain audit identity
```

Sumbu ini sejak awal tidak pernah punya FK ke `id.tenant_registry` (lihat komentar migrasi
007), jadi memuat nilai non-tenant bukan penyimpangan dari desainnya — hanya penamaan yang
selama ini kebetulan selalu tenant.

### 2. Token realm sentral **tidak bisa dipalsukan**, dan itu properti struktural

Nilainya `_central`: diawali garis bawah, sehingga **gagal** `^[a-z]` pada `tenantIDRe` dan
mustahil menjadi `tenant_id` yang sah. Tidak ada CHECK constraint tambahan, tidak ada daftar
nama terlarang yang harus dijaga tetap sinkron — ketidakmungkinannya berasal dari validator
yang sudah ada.

Ini alasan `"central"` polos ditolak: ia nama tenant yang sah. Keamanan yang bergantung pada
"semoga tidak ada pemda bernama central" bukan keamanan.

Konstanta hidup di satu tempat (`infra/crypto`) dan dipakai bersama oleh jalur kripto
identity **dan** sentinel chain audit identity, sehingga keduanya tak bisa menyimpang.
Perpindahan sentinel chain dari `"central"` ke `_central` ikut menutup cacat laten di atas.

### 3. Custody realm sentral adalah **invarian kode**, bukan baris kebijakan

ADR-010 §3 menetapkan custody KEK sebagai kebijakan **per-tenant** yang dibaca dari
`id.tenant_registry.key_custody`. Itu benar untuk tenant: tiap pemda bisa punya kontrak
berbeda soal siapa memegang kunci atas datanya sendiri.

Realm sentral tidak punya lawan bicara kontraktual. Identity DB adalah DB platform, memuat
data seluruh pemda sekaligus; tidak ada satu pemda yang berwenang memegang KEK-nya. Karena
itu:

```go
custody(_central) = platform    // selalu, tanpa membaca registry
custody(<tenant>) = registry.key_custody   // apa adanya, ADR-010 §3
```

Ditegakkan sebagai dekorator `CustodyResolver` — realm sentral dijawab sebelum query
registry pernah terjadi. Bukan sekadar penghematan query: resolver registry **fail-closed**
untuk tenant tak terdaftar, jadi tanpa ini realm sentral akan ditolak.

### 4. Purpose & granularitas kunci realm sentral

Satu purpose per kolom logis, mengikuti default framework (`FieldCryptoSpec.Purpose` =
nama kolom):

Satu purpose per **jenis pengenal**, bukan per kolom fisik:

| Purpose | Dipakai kolom | `kind` |
|---|---|---|
| `nik` | `id.persons.nik_enc`/`_bidx` · `id.credentials` (`cred_type='nik'`) · diff audit `identity.Person` | `enc`, `bidx` |
| `nip` | `id.employments.nip_enc`/`_bidx` · `id.credentials` (`cred_type='nip'`) · diff audit `identity.Employment` | `enc`, `bidx` |
| `email` | `id.persons.email_enc`/`_bidx` · `id.credentials` (`cred_type='email'`) · diff audit | `enc`, `bidx` |
| `no_hp` | `id.persons.no_hp_enc`/`_bidx` · `id.credentials` (`cred_type='no_hp'`) · diff audit | `enc`, `bidx` |
| `oauth` | `id.credentials` (`cred_type='oauth'`) | `enc`, `bidx` |

Kolom dan diff audit-nya berbagi purpose **dengan sengaja**: keduanya nilai yang sama pada
baris yang sama, jadi memisahkannya hanya menggandakan kunci tanpa memperkecil blast radius,
sekaligus membuat rotasi harus menyapu dua tempat alih-alih satu.

**`id.credentials.cred_value` memakai purpose dari `cred_type`-nya, bukan satu purpose
`cred_value` untuk semua tipe.** Alasannya bukan granularitas kunci melainkan
**normalisasi**: tabel kebijakan framework (`crypto.caseFoldedPurposes`) mengenali purpose
`email` sebagai nilai yang setara tanpa memandang besar-kecil huruf. Satu purpose gabungan
akan mengeluarkan kredensial email dari kebijakan itu, sehingga "email yang sama" punya dua
definisi — case-insensitive di `id.persons.email_bidx`, case-sensitive di `id.credentials` —
perbedaan yang tak bergejala sampai ada fitur yang menyilangkan keduanya, lalu salah
diam-diam.

Konsekuensi yang disadari & diterima: **login lewat email menjadi case-insensitive**
(sebelumnya equality SQL atas `VARCHAR`, case-sensitive), dan `UNIQUE` mulai menangkap
`Budi@x.id` vs `budi@x.id` sebagai duplikat. Ini perubahan semantik autentikasi, gratis
sekarang karena identity DB kosong dan mahal sesudah ada akun. `oauth` sengaja **tidak**
ikut di-case-fold: subject dari provider bersifat opaque.

`cred_type` sendiri tetap plaintext (ia jenis kredensial, bukan pengenal orang), sehingga
`UNIQUE (cred_type, cred_value_bidx)` tetap menegakkan keunikan per-tipe seperti sebelumnya
dan `FindByTypeValue` tetap satu query.

Karena purpose diturunkan dari jenis pengenal, nomor HP yang sama di `id.persons.no_hp` dan
di kredensial ber-`cred_type='no_hp'` menghasilkan `_bidx` yang **sama** — korelasi antar
tabel menjadi mungkin bila kelak dibutuhkan, tanpa migrasi kunci.

### 5. Pengikatan baris (ADR-016) berlaku penuh di realm sentral

`FieldRef.RecordID` diisi id baris pemiliknya — `persons.id`, `employments.id`,
`credentials.id` — dan pada diff audit diisi `AuditEntry.EntityID`, persis pola
`infra/db.auditedRepo`. Jalur baca mengambil identitas baris **dari baris itu sendiri**,
tidak pernah dari parameter pemanggil. Tidak ada pengecualian untuk realm sentral: menukar
`nik_enc` dua person lewat SQL langsung tetap membuat pembacaan gagal.

Blind index tetap row-independent (ADR-016 §3) — kalau tidak, `UNIQUE(nik_bidx)` tidak akan
pernah menangkap duplikat, yang justru properti utama yang dibeli PR ini.

## Konsekuensi

- **Isolasi antar-tenant tidak melemah.** Yang masuk realm sentral hanya data yang memang
  sentral (identity DB). Tak ada satu pun kolom di tenant DB yang berpindah realm; DEK
  per-tenant tetap satu-satunya kunci yang membuka data tenant.
- **Realm sentral adalah blast radius baru yang nyata dan diterima sadar.** Bocornya DEK
  `(_central, nik, bidx)` membuka pencarian NIK seluruh person di semua pemda sekaligus —
  konsekuensi tak terhindarkan dari `UNIQUE(nik)` yang memang global. Mitigasinya sama
  seperti kunci lain: DEK hanya tersimpan ter-wrap, KEK tak pernah keluar KeyProvider
  (ADR-010 §1), dan `kind` enc terpisah dari `kind` bidx.
- **Ruang NIK kecil (16 digit) tetap membuat kunci bidx aset bernilai tinggi** — catatan
  H2 di REVIEW_BACKLOG berlaku penuh di sini, kini atas data lintas-pemda. Ini menaikkan
  taruhan pada perlindungan master key (ops), bukan mengubah desainnya.
- **`id.tenant_registry` tetap murni berisi tenant.** Tidak ada baris palsu yang harus
  disaring `TenantRegistry.List`, alur pemilihan tenant saat login, atau
  `pamongctl tenant provision`.
- **Custody realm sentral tidak bisa diubah lewat SQL.** `UPDATE ... SET key_custody` tidak
  punya baris untuk disentuh; mengubahnya menuntut perubahan kode + review.
- **Sentinel chain audit identity berubah nilai** dari `"central"` menjadi `_central`.
  Aman hari ini karena `id.audit_logs` kosong di semua environment (diverifikasi saat
  PR-3.8.6: tak ada deployment staging/production, dev & CI tanpa schema `id`). Setelah ada
  data, perubahan yang sama akan memutus hash chain dan menuntut migrasi tersendiri.
- **Mode `key_custody='tenant'` (PR-3.8.8) tidak pernah berlaku untuk realm sentral.**
  Menambah driver custody pemda kelak tidak menyentuh keputusan ini.

## Alternatif yang dipertimbangkan

- **Baris cadangan `tenant_id='central'` di `id.tenant_registry`** (`is_active=false`,
  `key_custody='platform'`). Nol perubahan kode kripto — DEK store & custody resolver jalan
  apa adanya. Ditolak: ia mengubah custody yang **tidak bisa dinegosiasikan** menjadi baris
  data yang bisa di-`UPDATE`/`DELETE` operator (`SET key_custody='tenant'` membuat identity
  DB tak terbaca, dan gejalanya baru muncul saat kripto dipakai), serta membocorkan tenant
  palsu ke permukaan produk yang membaca registry. Pengecualian ditaruh di data, bukan di
  kode — tempat yang paling sulit di-review dan paling mudah berubah tanpa jejak.

- **Pakai sentinel `"central"` apa adanya** + bypass custody untuk nilai itu. Paling sedikit
  diketik. Ditolak: `"central"` lolos `tenantIDRe`, sehingga pemda yang kebetulan didaftarkan
  dengan nama itu berbagi ruang kunci **dan** chain audit dengan realm sentral. Cacat laten
  yang hari ini hanya soal urutan chain akan naik taruhannya menjadi soal kunci kripto.

- **Kunci per-tenant, dipilih dari home tenant person.** Ditolak dua kali: person tanpa
  penugasan (citizen) tak punya home tenant sama sekali, dan `UNIQUE(nik_bidx)` global
  langsung mati begitu dua tenant memakai kunci bidx berbeda (lihat §Konteks).

- **Turunkan kunci realm sentral dari KEK secara langsung, tanpa baris `id.data_keys`.**
  Menghemat satu konsep. Ditolak: ia membuang rotasi ber-versi, lazy re-encrypt, dan jejak
  `kek_driver`/`custody` yang sudah dibangun ADR-010 §4 — realm sentral justru yang paling
  butuh semuanya, karena datanya paling panjang umur.

- **Sumbu realm sebagai kolom/parameter BARU** (`port.FieldRef.Realm` terpisah dari
  `TenantID`, kolom `realm` di `id.data_keys`). Paling eksplisit secara tipe. Ditolak untuk
  sekarang: ia menyentuh kontrak port yang baru saja diubah ADR-016 dan merambat ke seluruh
  driver KMS, `testkit.MockCrypto`, `decryptingScanner`, serta jalur baca audit — biaya besar
  untuk membedakan dua nilai yang sudah tak mungkin tertukar secara struktural (§2). Bila
  kelak realm bertambah jenis (mis. realm per-region), ADR baru boleh menaikkannya jadi
  sumbu tersendiri.
