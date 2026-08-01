# ADR-015: Pengikatan kolom ciphertext lewat `PurposeOf` (perluasan `port.CryptoPort`)

## Status
Accepted — **memperluas ADR-009 §4** (menambah satu metode pada `port.CryptoPort`);
tidak men-supersede ADR-009 maupun ADR-010, seluruh keputusan keduanya tetap berlaku.

> **Sisa risiko di §Konsekuensi ("per-kolom, bukan per-baris") sudah DITUTUP oleh
> [ADR-016](016-pengikatan-baris-ciphertext-aad.md)** (PR-3.8.9): identitas baris kini masuk
> ke AAD. Keputusan ADR ini TIDAK digantikan — `PurposeOf` tetap satu-satunya yang menangkap
> perpindahan blob antar KOLOM pada baris yang sama (AAD-nya identik di situ).

## Konteks
ADR-009 §4 menetapkan seam kripto `port.CryptoPort` dengan tiga metode (`Encrypt`,
`Decrypt`, `BlindIndex`), dan §3 menetapkan format ciphertext **self-describing**:
`v1 | key_id (purpose) | key_version | nonce | ct+tag`. Format itu dipilih agar rotasi
kunci tidak butuh migrasi data dan `Decrypt` tak perlu diberi purpose — ia membacanya
dari blob.

Konsekuensi yang baru terlihat saat enkripsi di-wire ke lapis repository (PR-3.8.3):
karena purpose dibaca **dari blob itu sendiri**, dan AAD hanya mengikat **tenant**,
ciphertext yang dipindah antar kolom **di dalam satu tenant** tetap terbuka. Contoh
konkret: `UPDATE pegawais SET no_rekening_enc = nik_enc` — setelah itu NIK seseorang
terbaca aplikasi sebagai nomor rekeningnya yang sah, tanpa satu pun lapis yang protes.

Ini persis kelas ancaman yang dituju L3 (enkripsi field): pihak ber-akses DB tanpa akses
kunci. Enkripsi kolomnya benar, tapi **pengikatan nilai ke kolomnya** belum ada.

Pengikatan itu hanya bisa ditegakkan di lapis yang tahu ia sedang membaca kolom apa —
yaitu repository. Untuk menegakkannya, repository harus bisa mengetahui purpose sebuah
ciphertext **sebelum** mendekripsinya (tanpa kunci, tanpa I/O ke KMS).

## Keputusan
`port.CryptoPort` bertambah satu metode:

```go
// PurposeOf membaca purpose (konteks kunci) dari ciphertext TANPA mendekripsinya —
// tanpa kunci, tanpa I/O. ErrCiphertextInvalid untuk blob asing/rusak.
PurposeOf(ct []byte) (string, error)
```

Lapis repository (`infra/db/field_crypto.go`) memanggilnya pada setiap pembacaan kolom
terenkripsi dan **menolak** bila purpose blob ≠ purpose kolom, sebelum dekripsi dicoba.

Metode ini bagian dari **kontrak port**, bukan detail satu implementasi. Alasannya:
kebutuhan "tahu purpose tanpa kunci" lahir dari keputusan format self-describing yang
dibuat ADR-009 untuk SEMUA implementasi, jadi setiap driver KMS yang dipasang kelak
(Vault, AWS KMS, BSSN) harus tetap menyediakannya agar penegakan di repo tidak lenyap
begitu driver diganti.

## Konsekuensi
- Setiap implementasi `CryptoPort` — termasuk mock di `testkit/` dan driver KMS eksternal
  masa depan — wajib mengimplementasi `PurposeOf`. Ini biaya yang disadari: satu metode
  tambahan di seam yang sengaja dijaga sempit.
- Kontrak menuntut `PurposeOf` bebas-kunci & bebas-I/O, karena ia dipanggil **per baris
  per kolom** saat membaca daftar. Driver yang hanya bisa menjawab lewat panggilan KMS
  melanggar kontrak ini dan akan terasa sebagai regresi latensi, bukan sebagai kesalahan.
- Ciphertext menjadi terikat pada **purpose**, bukan pada nama kolom. Dua kolom yang
  sengaja berbagi purpose tetap saling menerima blob — pengikatan sekuat granularitas
  purpose yang dipilih, dan default framework adalah `purpose = nama kolom` sehingga
  granularitasnya per-kolom kecuali seseorang memutuskan sebaliknya.
- Mengganti nama kolom terenkripsi kini menyeret purpose-nya: data lama akan ditolak
  sampai di-re-encrypt. Ini disengaja (rename bukan operasi rutin di kolom pengenal),
  tapi harus tercatat agar tak mengejutkan saat terjadi.
- `PurposeOf` membocorkan purpose ke pemanggil tanpa kunci. Itu memang sudah tersimpan
  apa adanya di dalam blob (konsekuensi format ADR-009 §3), jadi tak ada informasi baru
  yang terbuka — nama konteks kunci bukan rahasia, isinya yang rahasia.
- ~~**Sisa risiko yang DISADARI: pengikatan ini per-kolom, bukan per-baris.**~~ **DITUTUP
  ADR-016** (PR-3.8.9). Teks aslinya dipertahankan sebagai catatan bagaimana risiko itu
  dikenali dan dijadwalkan: yang ditutup ADR ini adalah perpindahan blob antar KOLOM;
  menukar `nik_enc` + `nik_bidx` antar BARIS pada tabel & tenant yang sama tetap lolos
  setiap pemeriksaan dan mendekripsi bersih — NIK seseorang terbaca sebagai milik orang
  lain. Penyebabnya sama: AAD hanya mengikat tenant. Penutupnya adalah mengikat identitas
  baris ke AAD, dan itu **bukan tambalan di lapis repo** — ia mengubah kontrak
  `Encrypt`/`Decrypt`, merambat ke seluruh driver, mock, dan jalur baca audit (yang harus
  ikut membawa `EntityID`), jadi ia PR tersendiri ber-ADR sendiri. Catatan penjadwalannya:
  retrofit ini murah hanya selama belum ada data produksi — sesudahnya ia berarti
  re-enkripsi seluruh baris berpengenal. Karena itu gerbangnya keras: **sebelum tenant
  produksi pertama** (lihat ROADMAP 3.8).

## Alternatif yang dipertimbangkan

**Memasukkan nama kolom ke AAD.** Paling langsung: GCM akan menolak sendiri blob yang
dipindah. Ditolak karena AAD harus dapat direproduksi persis saat dekripsi, sehingga
`Decrypt` wajib menerima kolom/purpose sebagai parameter — membatalkan sifat
self-describing yang justru dipilih ADR-009 §3 agar rotasi & lazy re-encrypt murah.
Ia juga mengubah **format** setelah formatnya dikunci, hal yang ADR-009 nyatakan mahal.

**Repository mem-parse header ciphertext sendiri.** Nol perubahan port: byte 1..N sudah
memuat purpose. Ditolak karena mengunci `infra/db` pada tata letak byte satu implementasi;
driver KMS yang membungkus dengan amplopnya sendiri akan membuat parsing itu salah baca —
dan salah bacanya senyap (purpose "cocok" karena kebetulan), justru pada pemeriksaan yang
seharusnya menjaga keamanan.

**Membiarkan tak ditegakkan, andalkan hak akses DB.** Ditolak karena bertentangan dengan
model ancaman L3 di ADR-009: seluruh alasan mengenkripsi kolom adalah mengasumsikan pihak
ber-akses DB dapat menulis. Membiarkannya berarti mengenkripsi kolom sambil mempertahankan
jalur pemindahan nilai antar kolom — perlindungan yang tampak ada tapi bisa dilangkahi
dengan satu `UPDATE`.

**Menaruh pemeriksaan di `Decrypt` (menerima purpose yang diharapkan).** Ditolak karena
menggabungkan dua tanggung jawab dalam satu metode dan memaksa SEMUA pemanggil `Decrypt`
tahu purpose yang diharapkan — termasuk jalur baca audit yang justru tidak tahu kolom asal
nilai (lihat `core/audit.Reader`, yang mengenali nilai sensitif dari bentuk ciphertext-nya).

## Rujukan
- ADR-009 §3 (format ciphertext), §4 (seam `CryptoPort`), §6 (jalur kebocoran samping)
- ADR-010 (custody & hierarki kunci per-tenant/purpose)
- `port/crypto.go`, `infra/db/field_crypto.go` (penegakan), `infra/crypto/crypto.go` (format)
