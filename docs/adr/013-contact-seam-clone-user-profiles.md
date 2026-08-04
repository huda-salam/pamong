# ADR-013: Contact seam — kontak (email/no_hp) di clone tenant untuk routing notifikasi

## Status
Accepted — keputusan tetap berlaku. **Amended by ADR-018** — kurir pada Keputusan #1 diganti: kontak tetap
di-clone ke `gov.user_profiles` dan jalur KIRIM notifikasi tetap satu join same-schema (itulah
keputusannya), tapi nilainya tidak lagi menumpang field di `EmploymentDitugaskanPayload` —
`identity/sync` memintanya lewat port saat menangani event, karena payload adalah jalur samping
yang wajib ditutup (ADR-009 §6). Ini **bukan** "opsi B" yang ditolak di bawah: opsi B membaca
live **saat kirim**, di sisi tenant, per notifikasi. Tidak di-supersede.

## Konteks
PR-N1 (`infra/notification.DBRecipientDirectory`) membaca pemegang role tenant nyata dan
menghasilkan `Recipient{PersonID: ...}` — cukup untuk channel in-app. Tapi channel email/SMS
butuh alamat kontak (`Recipient.Email/Phone`) yang saat itu SELALU kosong: kontak (email/no_hp)
hidup di `id.persons` (identity DB), sedangkan clone tenant `gov.user_profiles` dan
`port.UserProfile` sengaja TIDAK memuatnya (komentar clone: "JANGAN tambah kolom credential").

Akibatnya channel email/SMS (termasuk OTP citizen) tak pernah punya alamat tujuan. PR-N3b
menutup ini. Dua opsi dipertimbangkan (lihat Alternatif); yang dipilih adalah **opsi A —
perluas clone**.

## Keputusan

**1. Kontak (email, no_hp) di-clone ke `gov.user_profiles` tenant, bukan dibaca live dari identity.**
Kolom `email VARCHAR(255)` dan `no_hp VARCHAR(15)` ditambahkan ke `gov.user_profiles`. Nilainya
mengalir lewat jalur clone yang SUDAH ADA — event "fat" `identity.employment.ditugaskan` —
bukan jalur baru. `EmploymentDitugaskanPayload` bertambah field `Email` & `NoHP` (aditif,
JSON-compatible; SchemaRegistry mencocokkan per-tipe Go sehingga tak dianggap versi baru). Use
case `AssignEmploymentToTenant` mengisinya dari `person.Email/NoHP` (person sudah di-load, tanpa
query tambahan). Sync writer meng-upsert kontak; clone tetap read-only bagi modul.

**2. Penambahan kolom lewat ensure-on-write (idempotent ALTER), bukan file migrasi.**
`gov.user_profiles` memang dibuat lewat ensure-on-write (`CREATE TABLE IF NOT EXISTS` di
`identity/sync`), bukan migrasi framework formal. Konsisten dengan itu, kolom kontak ditambah via
`ALTER TABLE ... ADD COLUMN IF NOT EXISTS` di DDL ensure yang sama — idempoten, aman dipanggil
berulang, dan menambah kolom ke tabel yang sudah dibuat versi lama.

**3. `DBRecipientDirectory` mengisi kontak BEST-EFFORT, dijaga keberadaan KOLOM.**
`HoldersOf` tetap menghasilkan pemegang dari `gov.user_role_assignments` (tak berubah), lalu
`fillContacts` mengisi Email/Phone dari `gov.user_profiles`. Karena (a) pemegang role BISA eksis
tanpa profil (`user_role_assignments` sengaja tanpa FK ke `user_profiles`, tenantrole/schema.go)
dan (b) selama window rollout tabel bisa ada TANPA kolom kontak, keberadaan kolom `email`
diperiksa dulu (`information_schema.columns`). Bila absen → kontak dibiarkan kosong; resolusi
pemegang (jalur in-app) TIDAK ikut gagal. Kontak kosong/NULL → channel email/SMS gagal anggun
(`INVALID_RECIPIENT`) — perilaku yang memang dirancang.

**4. Freshness (update-on-change) tetap DEFERRED — sama seperti `nama_lengkap`.**
Kontak awal ter-clone saat penugasan. Propagasi perubahan kontak (person mengganti email/no_hp)
menuntut handler `identity.person.diperbarui` yang meng-update SEMUA tenant tempat person
ter-clone — mekanisme yang BELUM ADA dan juga belum ada untuk `nama_lengkap` (engine sync
menyebutnya "menyusul"). PR-N3b TIDAK membangun jalur khusus-kontak yang inkonsisten dengan
name; freshness kontak menumpang solusi umum clone-freshness kelak (satu handler untuk
nama+kontak sekaligus).

## Konsekuensi
- PII kontak (kelas `personal_id`, ADR-009) kini tersalin ke tiap tenant DB tempat person
  bertugas — bukan hanya di identity DB. Ini konsekuensi sadar opsi A (lihat Alternatif). Enkripsi
  field untuk kolom ini DEFERRED bersama `nik`/`no_hp` lain (ROADMAP 3.8) — konsisten dengan
  keadaan sekarang (kolom personal_id lain di clone pun masih plaintext).
  → **Sudah tidak DEFERRED**: kolom kontak di clone disegel PR-3.8.5a (realm tenant), dan
  nilainya berhenti melewati payload event di PR-3.8.5b (ADR-018).
- `gov.user_profiles` bertambah 2 kolom; `EmploymentDitugaskanPayload` bertambah 2 field (aditif).
- Kontak yang berubah di identity TIDAK otomatis ter-refresh di clone sampai clone-freshness
  dibangun (sama seperti nama). Dokumen historis tetap snapshot (memang begitu seharusnya);
  tapi rute NOTIFIKASI ("kirim ke Kadis sekarang") idealnya pakai kontak terbaru — gap ini
  diterima sementara karena identik dengan gap nama yang sudah ada.
- `DBRecipientDirectory` kini melakukan 1 query probe + 1 query kontak tambahan per `HoldersOf`
  saat ada pemegang. Notifikasi bukan hot-path; biaya diterima.

## Alternatif yang dipertimbangkan
- **Opsi B — port `ContactResolver` di identity, dibaca live saat kirim.** Tidak menduplikasi PII
  kontak ke tenant DB (lebih selaras "kontak = milik identity"), dan kontak selalu terbaru tanpa
  butuh clone-freshness. Ditolak (keputusan user 2026-07-27): lookup cross-DB (tenant→identity)
  saat kirim menambah kopling runtime notifikasi ke identity DB dan latensi per-kirim; opsi A
  membuat kontak tersedia lokal di tenant DB (satu join same-schema) seperti atribut clone lain.
  Trade-off yang diterima: PII kontak menyebar + butuh freshness kelak.
- **Ekspansi via event baru `identity.person.diperbarui` sekarang (freshness penuh).** Ditunda:
  update-propagation ke semua tenant belum terpecahkan bahkan untuk `nama_lengkap`; membangunnya
  khusus untuk kontak = scope-creep tak konsisten. Lihat Keputusan #4.
- **Tambah Email/Phone ke `port.UserProfile`/`UserResolver`.** Tidak dilakukan: belum ada
  pembaca `UserResolver` produksi yang butuh kontak; satu-satunya konsumen (DBRecipientDirectory)
  membaca clone langsung (same-schema `gov`). Menambah field port tanpa pembaca = spekulatif.

## Rujukan
- Plan: [[plan-notification-completion]] (arc N3), PR-N1 (ADR: DBRecipientDirectory), ADR-009
  (klasifikasi data & enkripsi field), ADR-012 (bridge workflow→notifikasi, konsumen kontak).
- Kode: identity/sync/{clone,engine,writer_tenantdb}.go, identity/domain/events.go,
  identity/usecase/assign_employment_tenant.go, infra/notification/directory.go.
