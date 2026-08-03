# ADR-018: Menutup jalur samping — menghapus nilai vs menyegelnya

## Status
Proposed — menunggu keputusan. Mengamandemen **mekanisme** ADR-009 §6 butir 2 (tidak
men-supersede keputusannya) dan mengganti **kurir** pada Keputusan #1 ADR-013 (keputusannya
tetap berlaku). Pola yang sama dengan ADR-009 yang memperbarui mekanisme ADR-002.

## Konteks

ADR-009 §6 mendaftar enam jalur samping yang wajib ditutup bersama enkripsi kolom, karena
"enkripsi satu kolom sia-sia bila nilai mentah bocor lewat jalur lain". Lima sudah tertutup
(diff audit, log/trace, clone `gov.user_profiles`, dan — sebagai bagian PR ini — cache
idempotency; staging migrasi menunggu pipeline-nya ada). Yang tersisa: **payload event**.

Untuk jalur itu ADR-009 §6 butir 2 menuliskan mekanismenya secara spesifik: *"pengenal di
payload di-mask/enc"*. Saat mekanisme itu hendak dijalankan, tiga hal yang belum diketahui pada
saat ADR-009 ditulis membuatnya mahal:

1. **ADR-016 (pengikatan baris) sudah mendarat.** Ciphertext kini terikat `(realm, record_id)`
   dan formatnya naik `0x01` → `0x02`; blob v1 **ditolak** `Decrypt`. Sebuah blob yang mengendap
   di stream retensi panjang atau di baris `gov.outbox_events` karena itu bukan data pasif — ia
   kewajiban dekripsi yang harus tetap dipenuhi melintasi rotasi kunci dan patahan format
   berikutnya. Kita sudah kehilangan kompatibilitas format satu kali.
2. **ADR-017 (realm kunci sentral) sudah mendarat.** Pengenal identity disegel realm `_central`,
   sedangkan clone tenant disegel realm tenant. Ciphertext identity karena itu **tidak bisa**
   diteruskan apa adanya ke clone — consumer harus membukanya lebih dulu, artinya penulis clone
   (sisi tenant) harus memegang kunci realm sentral. Itu persis pengaturan yang ADR-017 hindari.
3. **Consumer satu-satunya ada di sisi identity.** `identity/sync` adalah satu-satunya subscriber;
   `identity.person.dibuat` dan `identity.employment.dibuat` bahkan tak punya subscriber sama
   sekali, sehingga pengenal di kedua payload itu murni liabilitas tanpa pemakai.

Sementara itu doktrin "payload fat" (payload membawa semua kolom agar consumer mandiri dari skema
sentral) hanya hidup sebagai komentar kode dan satu kalimat di ADR-013 — tidak pernah diputuskan
lewat ADR.

## Keputusan

**1. Jalur samping ditutup dengan MENGHAPUS nilainya bila consumer bisa meresolusi sendiri;
menyegel hanya bila tidak bisa.**

Aturan pemilihnya: *apakah jalur ini menyimpan data, atau sekadar mengangkutnya?*

| | Payload event | `gov.idempotency_keys.response` |
|---|---|---|
| Sifat | pengangkut; nilainya ada di tempat lain | penyimpan; badan respons memang datanya |
| Tindakan | **hapus** dari payload | **segel** |
| Consumer | minta lewat port di sisi pemilik data | dibuka store yang sama saat replay |

**2. Payload event identity berhenti membawa nilai kelas `personal_id`.**
`PersonDibuatPayload.NIK`, `EmploymentDibuatPayload.NIP`, dan
`EmploymentDitugaskanPayload.{NIK,NIP,Email,NoHP}` dihapus. Payload menyisakan koordinat (id) +
atribut non-pengenal. `NamaLengkap` **tetap**: kelasnya `personal`, dan ADR-009 sengaja tidak
mengenkripsinya di kolom — mengeluarkannya dari payload akan menerapkan standar yang lebih ketat
di jalur angkut daripada yang berlaku at-rest.

**3. Consumer meresolusi lewat port di sisi PEMILIK data, bukan dengan membaca DB-nya sendiri.**
`identity/sync.CloneSource` diimplementasi di atas repo identity — repo itulah yang membuka realm
sentral. Kunci sentral tak pernah keluar dari sisi identity. Ini yang membedakannya dari
"consumer membaca identity DB": yang dipindahkan adalah panggilan, bukan kunci.

**4. Tak-ditemukan / gagal baca = gagal LANTANG, bukan clone berpengenal kosong.**
Handler mengembalikan error sehingga event di-retry. Clone berpengenal kosong tak bisa dibedakan
dari non-ASN yang memang tak punya NIP, dan ia melumpuhkan `ResolveByNIK` serta routing
notifikasi tanpa satu pun gejala.

**5. `gov.idempotency_keys.response` disegel — realm tenant, tanpa blind index.**
Nilai ini tak pernah menjadi kunci pencarian; blind index deterministik atasnya hanya menjadi
oracle kesamaan antar respons API plus satu kunci lagi untuk dirotasi (`FieldSealer.SealOpaque`).
Koordinat AAD diturunkan deterministik dari KEDUA bagian PK
(`uuid.NewSHA1(person_id, "gov.idempotency_keys\0"+key)`) karena tabel ini tak punya kolom UUID —
dari `person_id` saja, respons boleh dipindah antar key milik orang yang sama dan tetap terbuka,
dan `fingerprint` tak menolong karena ia ikut berpindah dalam baris yang sama.

`fingerprint` **tidak** disegel: ia SHA-256 atas (method+path+body), bukan nilai mentah, dan
menyegelnya mematikan satu-satunya gunanya (dibandingkan saat `Reserve`, sebelum baris apa pun
dibuka). Ia tetap oracle kesamaan atas request utuh — diterima secara sadar.

## Konsekuensi

- **Semantik clone bergeser dari snapshot ke live** untuk keempat pengenal: nilainya diambil saat
  event DITANGANI, bukan saat terbit. Itu justru kontrak `gov.user_profiles` yang berlaku (clone
  HIDUP, "siapa user ini sekarang"), dan ia menggeser item DEFERRED clone-freshness satu langkah
  maju. `NamaLengkap` tetap snapshot — clone karenanya bercampur dua basis waktu sampai
  clone-freshness dibangun. Diterima: `nama_lengkap` sudah tidak fresh sejak sebelum PR ini.
- **`identity/sync` berhenti mandiri dari skema identity** (doktrin fat event dibalik untuk
  pengenal). Kopling ke identity DB masuk ke jalur sync — bukan ke jalur kirim notifikasi.
- **Satu query per event penugasan.** Penugasan adalah operasi admin yang jarang; bukan hot path.
- **Identity DB menjadi jalur kritis sync.** Identity DB tak terbaca = clone tertahan (retry),
  bukan clone cacat. Dipilih sadar (Keputusan #4).
- **Tak ada perubahan bentuk tabel**, tapi arti dua kolom bergeser total — dicatat di
  DB_CHANGELOG justru karena tak terlihat dari `\d`.
- **Baris idempotency lama menjadi tak terbaca** (`PurposeOf` gagal pada plaintext) → `Reserve`
  error → middleware 503 → klien retry. TTL 24 jam membuat dampaknya habis sendiri.
- **Yang mengendap tidak dibersihkan mesin ini**: pesan di stream NATS retensi & baris outbox
  lama tetap memuat plaintext. Runbook, bukan kode — kelas yang sama dengan catatan
  `DROP COLUMN`/heap-tuple di PR-3.8.5a.

## Alternatif yang dipertimbangkan

- **Segel payload dengan realm `_central` (bunyi harfiah ADR-009 §6 butir 2).** Ditolak: realm
  clone adalah tenant, jadi consumer harus buka-lalu-segel-ulang — penulis clone di sisi tenant
  memegang kunci realm sentral, persis yang ADR-017 hindari. Ditambah kewajiban dekripsi permanen
  atas blob yang mengendap di stream/outbox.
- **Segel payload dengan realm TENANT TUJUAN saat publish** (payload membawa `nik_enc`+`nik_bidx`
  yang sudah dalam realm tujuan, terikat `record_id = PersonID` yang kebetulan juga `id` baris
  clone; writer menulisnya verbatim, nol kripto). Paling rapi secara operasional dan sempat
  menjadi kandidat kuat. Ditolak karena dua hal: (a) ia menaruh kripto di sisi PUBLISH, yaitu
  `identity/usecase` — menabrak aturan mengikat "domain/usecase nol-dependency kripto"; jalan
  keluarnya (dekorator `EventPublisher` penyegel yang digerakkan tag/refleksi di lapis bus) adalah
  mesin baru, bukan pemakaian mesin yang ada; (b) `PersonDibuat`/`EmploymentDibuat` tak punya
  `TenantID`, jadi opsi ini tak punya jawaban untuk keduanya. Kewajiban dekripsi permanen tetap
  berlaku.
- **Masking (bukan enkripsi) di payload.** Ditolak: consumer butuh nilai penuh untuk menulis
  clone; nilai ter-mask membuat event tak berguna sekaligus tetap membocorkan bentuk & prefiks.
- **Menghapus `NamaLengkap` sekalian.** Ditolak: menerapkan standar `personal_id` pada field
  kelas `personal`, tidak konsisten dengan kolomnya sendiri yang sengaja plaintext.
- **Membiarkan payload apa adanya dan mengandalkan enkripsi disk (L2).** Ditolak dengan alasan
  yang sama seperti ADR-009 menolaknya untuk kolom: L2 tidak melindungi terhadap dump logis,
  akses baca DB yang sah, maupun `pg_dump` — dan seluruh pekerjaan 3.8 berangkat dari premis itu.

## Hubungan dengan ADR lain

- **ADR-009** — keputusan intinya (klasifikasi + enkripsi selektif + §6 wajib menutup jalur
  samping) **tetap berlaku**. Yang diperbarui hanya mekanisme butir 2: "di-mask/enc" → "dihapus,
  consumer meresolusi lewat port". Butir 3 (cache idempotency) dijalankan sesuai bunyinya.
- **ADR-013** — Keputusan #1 (kontak di-clone ke tenant; jalur KIRIM notifikasi tetap satu join
  same-schema) **tidak berubah**. Yang berganti kurirnya: dari field di payload menjadi baca-balik
  saat sync. Opsi yang ditolak ADR-013 ("opsi B") adalah baca live **saat kirim**, di sisi tenant,
  per notifikasi — beda sumbu dari yang diputuskan di sini (sekali per penugasan, di sisi
  identity). Alasan penolakan dulu (kopling runtime notifikasi + latensi per-kirim) tidak berlaku
  di sini.
- **ADR-016/017** — keduanya adalah sebab ADR ini ada; tak satu pun diubah.
