# identity/ — Identity Module

Modul sentral identitas: person, employment, credential, central role, tenant assignment,
auth flow. DB TERPISAH (gov_identity). Data di-clone ke tenant via event. SENSITIF —
setiap perubahan butuh review ekstra (lihat aturan PR).

## Bergantung pada
- port/, core/domain, core/permission

## Tidak boleh diimport modul bisnis
- Akses dari modul bisnis HANYA lewat port (UserResolver) + event. Import package
  identity/ dari modul bisnis -> linter tolak.

## Tanggung jawab
- Model: person (anchor NIK), employment (opsional, NIP untuk ASN), credential (banyak)
- Persona: konteks login (employee | citizen) — BUKAN tipe orang
- Central role: global (semua tenant) + scoped (tenant_scope[])
- Tenant assignment: penugasan employment ke tenant; cross-tenant (PLT/PJ) ber-otorisasi
- Auth flow: tiga jalur (employee sentral, employee daerah, citizen publik)
- JWT issue/verify, revocation (jti)
- Sync engine: clone person/employment ke tenant DB via event. Clone `gov.user_profiles`
  membawa KONTAK (email/no_hp) untuk routing notifikasi email/SMS (PR-N3b, ADR-013) — masih tanpa
  kredensial/password. Sejak PR-3.8.5 nik/nip/email/no_hp pada clone **TERENKRIPSI dengan realm
  TENANT** (lihat §Enkripsi pengenal). Freshness kontak/nama (update-on-change lintas-tenant)
  DEFERRED.

## BUKAN tanggung jawab
- Evaluasi permission (itu core/permission; identity menyimpan central role master)
- Data operasional tenant (modul bisnis)

## File kunci
- domain/ — person, employment, credential, central role, assignment + ports
- usecase/ — create person, attach employment, assign role, cross-tenant assign, login
- adapter/ — http (auth endpoints), db (identity DB)
- sync/ — clone engine (subscribe event, tulis ke tenant DB)

## Konvensi khusus
- ASN = masyarakat yang punya employment. Bisa login publik sebagai citizen (token tanpa
  role internal). Persona ditentukan portal, bukan tipe orang.
- NIK anchor global unik. NIP di employment (unik, wajib untuk ASN).
- **Pengenal tersimpan TERENKRIPSI + blind index** (PR-3.8.6, ADR-009/015/016/017) — lihat
  §Enkripsi pengenal di bawah.
- Cross-tenant assignment (is_home_tenant=false) butuh permission khusus.
- Identity DB selalu sentral; tenant (termasuk dedicated server) tetap connect untuk auth.

## Enkripsi pengenal (PR-3.8.6 · ADR-017)
`nik`, `nip`, `cred_value`, `no_hp`, `email` tersimpan sebagai `{f}_enc` + `{f}_bidx`.
Kolom plaintext-nya **tidak ada** — bukan sekadar berhenti diisi.

- **Realm kunci = `crypto.RealmCentral` (`_central`), bukan tenant.** Data identity tak punya
  tenant, dan `UNIQUE(nik)` berlaku global se-identity-DB: kunci bidx per-tenant membuat NIK
  yang sama menghasilkan bidx berbeda → UNIQUE berhenti menangkap duplikat. Token `_central`
  gagal `tenantIDRe` (`^[a-z]…`) sehingga mustahil bertabrakan dengan tenant nyata.
- **Lookup equality WAJIB lewat `_bidx`.** `{f}_enc` memakai nonce acak — `WHERE nik_enc = $1`
  tak akan pernah cocok. `FindByNIK`/`FindByNIP`/`FindByTypeValue` menghitung bidx dulu.
- **Purpose kredensial diturunkan dari `cred_type`**, bukan satu purpose `cred_value`
  (ADR-017 §4) — supaya kredensial email ikut normalisasi framework. Akibat yang disengaja:
  login lewat email **case-insensitive**. `oauth` tidak di-fold.
- **Identitas baris untuk AAD diambil dari BARIS ITU SENDIRI** (`p.ID` hasil scan), tak pernah
  dari argumen pemanggil — itulah yang membuat pemindahan ciphertext antar baris gagal.
- **`PurposeOf` diperiksa sebelum `Decrypt`** (ADR-015): AAD tak mengikat kolom, jadi tanpa itu
  `no_hp_enc` bisa disalin ke `email_enc` pada baris yang sama dan tetap terbuka.
- **Diff audit ikut disegel** (`sealIdentityDiff` → `infra/db.SealAuditDiff`, realm sentral).
  Snapshot diambil dari ENTITY (plaintext), jadi mengenkripsi kolom saja hanya MEMINDAHKAN
  kebocoran ke `id.audit_logs.diff`.
- **Semua konstruktor repo & dekorator audit MENOLAK `CryptoPort` nil.** Jangan longgarkan:
  nil membuat pengenal mendarat plaintext tanpa satu pun gejala.
- **Pengenal tak ikut ke pesan error.** `ErrConflict`/`ErrNotFound` menyebut JENIS pencarian
  (`"nik"`, `"nip"`, `"email"`), bukan nilainya: pesan `FrameworkError` mengalir ke log DAN body
  HTTP — jalur samping yang sama (ADR-009 §6).
- **Normalisasi blind index mengubah arti "nilai yang sama" bagi SELURUH sistem.** Lookup kini
  trim + case-fold (`email`), jadi beberapa ejaan me-resolve ke satu baris. Apa pun yang di-key
  pada nilai pencarian — rate limiter, cache, idempotency — harus di-key ulang pada hasil
  resolusi (ID), bukan pada nilai mentah. Lihat REVIEW_BACKLOG A7.
- **Nilai yang MASUK wajib sudah kanonik — aturannya di DOMAIN, penegaknya di PINTU TULIS.**
  `Credential.Validate` & `Person.Validate` menolak control character + spasi tepi
  (`bentukPengenalRusak`), karena `seal()` mengenkripsi verbatim sementara `index()` menormalkan.
  Tanpa aturan ini nilai ber-CRLF bisa TERSIMPAN, dan alamat kanonik hasil dekripsi pun menjadi
  alamat yang ditolak SMTP. Jangan memindahkan ATURANNYA ke `seal()`: nilai terdekripsi jadi ≠
  nilai yang didaftarkan, dan kebijakan `infra/crypto` tersalin ke lapis repo. Yang dipasang di
  repo hanya PEMANGGILANNYA: `Save()` ketiga repo identity memanggil `Validate()` sebelum menyegel,
  sebab aturan yang menunggu tiap penulis use case baru mengingat memanggilnya bukan aturan —
  persis alasan enkripsi & audit juga dipasang di lapis repo, bukan di use case.
- **Clone tenant memakai realm TENANT, bukan `_central` (PR-3.8.5).** Aturan realm sentral berlaku
  untuk data identity yang memang tak punya tenant; `gov.user_profiles` hidup DI DALAM satu tenant
  DB, jadi ia dilindungi kunci yang sama dengan sisa DB itu. Realm sentral di sana berarti satu
  kunci membuka clone seluruh pemda. Akibat yang disengaja: `nik_bidx` orang yang sama berbeda
  antar tenant, jadi clone tak bisa dipakai mengorelasikan orang lintas tenant. Penulis
  (`identity/sync`) dan pembaca (`infra/user`, `infra/notification`) WAJIB memakai realm yang
  sama — realm yang salah tidak gagal, ia hanya membuat bidx tak pernah cocok.
- **Kebijakan seal/index/open punya SATU implementasi: `crypto.FieldSealer`.** Repo identity,
  writer clone, dan kedua pembacanya memanggilnya, bukan menyalin aturannya. Jangan menulis ulang
  "kosong → NULL", pengikatan baris, atau pemeriksaan `PurposeOf` di tempat baru.
- **Pengenal TIDAK ikut payload event (PR-3.8.5b, ADR-018).** Payload membawa koordinat (id) +
  atribut non-pengenal; `identity/sync` memintanya lewat `sync.CloneSource` saat event ditangani.
  Jangan "menambahkan kembali demi menghemat satu query": `gov.outbox_events.payload` plaintext
  JSONB dan stream NATS punya retensi, jadi field yang ditambahkan di sini mendarat di dump.
  `NamaLengkap` boleh ikut payload — kelasnya `personal`, bukan `personal_id`, dan ia yang
  membuat baris outbox/DLQ masih terbaca operator. Tapi **clone tidak memakainya**: writer
  mengambil nama dari `CloneSource` juga, supaya satu baris clone tidak mencampur nilai saat
  event terbit dengan nilai saat event ditangani. Di payload ia informasional, bukan sumber
  kebenaran — jangan "merapikannya" dengan memakai `p.NamaLengkap` di engine.
  Konsekuensi yang disengaja: clone menerima nilai saat HANDLING, bukan saat event terbit — dan
  itu memang kontrak `gov.user_profiles` (clone HIDUP, bukan sumber dokumen historis).
  `CloneSource` diimplementasi di atas repo identity **dengan sengaja**: repo-lah yang membuka
  realm sentral, sehingga kunci sentral tak pernah keluar dari sisi identity. Jangan
  memindahkannya ke sisi tenant (mis. membaca identity DB dari `infra/*`).
  Karena person & employment dibaca TERPISAH dari dua id yang datang di payload, pasangannya
  dibuktikan di `RepoCloneSource` (`e.PersonID == personID`) — bukan dipercaya. Tanpa itu satu
  event keliru/palsu menghasilkan clone bergabung (NIK satu orang + NIP orang lain) yang tampak
  sah dan menjadi jawaban `ResolveByNIK`/`ResolveByNIP` sesudahnya.
- **Sesudah lookup, yang kanonik adalah BARIS yang ditemukan — bukan nilai permintaan.** Nilai
  permintaan berhenti layak dipakai sebagai alamat tujuan, parameter kirim, atau apa pun yang
  mengalir ke sistem luar. `TrimSpace` di `normalize()` ikut membuang CR/LF, jadi ejaan yang
  me-resolve dengan sukses bisa tetap ditolak transport di hilir — perbedaan respons itulah
  yang menjadi orakel. Pakai nilai hasil dekripsi (`cred.CredValue`).

## Pitfall umum
- Memodelkan user_type sebagai properti person (SALAH). Yang ada: employment (opsional)
  + persona (konteks login).
- Mengira citizen butuh tenant assignment (TIDAK). Hanya employee yang butuh.
- ASN login publik membawa role internal (SALAH, harus tanpa role internal).
- Menambah query baru yang menyaring/mengurutkan atas pengenal (`WHERE ... LIKE`, `ORDER BY nik`)
  — kolomnya tak ada lagi, dan `_bidx` hanya melayani equality. Butuh partial/range search =
  sinyal klasifikasi field-nya salah, bukan izin membuka kolom plaintext.
- Menyelipkan id baris ke `BlindIndex` "supaya konsisten dengan Encrypt". Ia mematikan lookup
  DAN UNIQUE tanpa satu pun error — hanya hasil yang salah (ADR-016 §3).
- **Meng-key kuota/limiter OTP pada nilai kredensial mentah.** Sejak lookup lewat blind index,
  `budi@x.id` / `Budi@x.id` / `" budi@x.id"` adalah SATU kredensial dengan tiga bucket limiter —
  kuota bisa dilipatgandakan tanpa batas dan anti-brute-force OTP praktis lumpuh. Pakai ID
  kredensial hasil resolusi (`otpCredRequestKey`/`otpCredVerifyKey`). Menormalkan nilai di lapis
  use case BUKAN jalan keluar: itu menyalin tabel kebijakan `infra/crypto` ke tempat yang memang
  tak boleh menyentuh kripto.
- Membuat habisnya kuota per-kredensial menjawab 429. Lapis itu berjalan SESUDAH lookup, jadi
  respons yang berbeda menjadikannya orakel keberadaan akun — habisnya harus meniru jalur normal
  (request → nil senyap, verify → 401 seragam).
- **Mengirim OTP ke `in.CredValue`.** Sebelum blind index nilai itu selalu identik dengan yang
  terdaftar; sekarang tidak. `"victim@x.id\n"` me-resolve ke kredensial nyata lalu ditolak SMTP
  sebagai header injection (500), sedangkan alamat asing menjawab 200 — satu probe per target,
  dan probe-nya sekalian menimpa OTP korban yang sedang berjalan.
- Menuliskan literal `"central"`/`"_central"` alih-alih `crypto.RealmCentral`. Partisi chain
  audit dan realm kunci HARUS nilai yang sama: `audit.Reader` membangun `RowRef.TenantID` dari
  `entry.TenantID` untuk membuka diff.

## Test
- Unit: resolve by NIK/NIP, persona resolution, central role scope, cross-tenant otorisasi.
- Integration: clone sync via event -> tenant punya data user; enkripsi pengenal ujung-ke-ujung
  (`adapter/db/field_crypto_integration_test.go`) — tabel dibangun dari file migrasi NYATA,
  jadi yang diuji SAMBUNGAN migrasi ↔ repo. Unit test tak bisa membuktikan bentuk tabel,
  UNIQUE yang ditegakkan Postgres, maupun isi dump.
- go test ./identity/... -race
- `PAMONG_TEST_DB_DSN=... go test ./identity/... -tags=integration -p 1`

## Rujukan
- PRD.md, core/permission/PRD.md, port/user.go, port/auth.go
- ADR-003 (audit identity), ADR-009 (klasifikasi & enkripsi field), ADR-015 (`PurposeOf`),
  ADR-016 (pengikatan baris), **ADR-017 (realm kunci sentral)**
