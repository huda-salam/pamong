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
- usecase/ — create person, attach employment, **create credential** (`create_credential.go`,
  jalur TULIS password satu-satunya), assign role, cross-tenant assign, login
  (`password_auth.go` = verifikasi kredensial + proteksi brute-force, dipakai employee & citizen)
- adapter/ — http: DUA grup dengan pemasangan BERLAWANAN.
  * `handler.go` (auth: /auth/login, /auth/select-tenant, /auth/public/{login,otp/*}) dipasang
    lewat `cmd/server.mountAuthRoutes` di TOP MUX, di LUAR RequireAuth (login itu
    pra-otentikasi), kecuali select-tenant.
  * `admin_handler.go` (PR-W2: /admin/identity/{persons,employments,credentials,assignments,
    central-role-assignments}) dipasang lewat `cmd/server.mountAdminIdentityRoutes` ke ROUTER
    BISNIS, jadi ia mewarisi stack lengkap termasuk RequireAuth. Seluruhnya mutasi identitas.
- adapter/db — repo identity DB (+ dekorator audit; grup admin memakai yang ber-audit).
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

## Containment aktor→target (PR-W3a · ADR-019)

Punya permission ≠ boleh atas target ini. `identity/usecase/containment.go` menegakkan tiga
aturan pada mutasi identitas, mengikuti *privilege escalation prevention* Kubernetes RBAC:

1. **Tenant** — operasi yang menyebut tenant hanya boleh menyebut **tenant token aktor**
   (`AssignEmploymentToTenant`).
2. **Role** — role sentral hanya boleh diberikan bila aktor memegang SELURUH permission yang
   role itu berikan, scope-nya = tenant aktor, dan role **global** selalu di luar wewenang
   (`AssignCentralRole`).
3. **Target** — kredensial hanya untuk person yang wewenang SENTRALnya tak melampaui aktor
   (`CreateCredential`).

Pintu keluarnya satu, eksplisit, ter-audit: **`identity:authority:escalate`**.

**Pemegang PERTAMA-nya di-seed, bukan di-grant.** Memberikan role yang memuat `escalate`
menuntut aktor sudah memegangnya, jadi genesisnya selalu di luar aplikasi — jalur yang sama
dengan admin platform pertama (sentinel SYSTEM, lihat §Sentinel di bawah). Role bootstrap yang
diseed lewat repo/SQL **wajib** memuat `identity:authority:escalate`; tanpa itu instalasi baru
punya admin yang tak bisa menugaskan role global maupun menugaskan lintas tenant, tanpa jalan
keluar. Acuan bentuknya: `seedAdminBootstrap` di `cmd/server/admin_identity_e2e_integration_test.go`.

- **Wewenang teritorial aktor = `ctx.TenantID()`, bukan klaim `tenant_scope`.** Token selalu
  ter-scope satu tenant dan role sentral di dalamnya sudah disaring saat login, jadi
  `tenant_scope` sengaja diterbitkan kosong (`scopedTokenMinter.mint`). Jangan "melengkapi"
  klaim itu atau menambah `TenantScope()` ke `port.AuthContext` tanpa mengubah ADR-019 lebih
  dulu — hari ini ia kontrak yang dorman sejak lahir.
- **Konteks tanpa tenant fail-closed**, bukan wildcard — dan pemeriksaannya (`requireTenantBound`)
  MENDAHULUI pintu keluar escalate. `RequirePermission` permisif saat evaluator nil, jadi tanpa
  sinyal positif dari klaim tersigning, konteks tanpa evaluator akan terbaca "boleh eskalasi".
  Jangan memindahkannya ke belakang `mayEscalate`.
- **Wewenang TARGET disaring dengan `Expired`, bukan `ActiveAt`.** Kredensial berumur permanen;
  `valid_from` datang dari klien. Assignment yang belum mulai TETAP dihitung — kalau tidak, role
  global yang dijadwalkan pekan depan bisa dipanen lewat kredensial yang diterbitkan hari ini.
  Hanya `ValidUntil` yang sudah lewat yang aman diabaikan.
- **Kepemilikan permission aktor diperiksa lewat `ctx.RequirePermission`**, bukan jalur evaluasi
  sendiri. Jalur kedua akan menyimpang dari otorisasi sesungguhnya diam-diam.
- **Residu yang disengaja:** yang diperiksa wewenang SENTRAL target; role TENANT target hidup di
  DB tenant dan butuh port lintas-DB baru. Sisa risikonya lateral di dalam satu tenant.

## Pagar ukuran token (PR-W3c · ADR-020)

`JWTCodec.Issue` **menolak** token yang melewati ambang (`GOV_AUTH_TOKEN_MAX_BYTES`, default
`token.DefaultMaxBytes` = 6 KiB) — tidak diterbitkan, dan `core.ErrTokenTooLarge` (409
`TOKEN_TOO_LARGE`) menyebut jumlah role. `central_roles[]` + `tenant_roles[]` satu-satunya klaim
yang bertumbuh (≈ `panjang_nama × 1,37` byte per role); tanpa pagar, akun ber-role banyak
menerbitkan token yang ditolak PROXY, bukan aplikasi: login 200 lalu setiap request 400 tanpa
jejak di log Go.

- **Jangan memasukkan permission ke token** "supaya tak perlu katalog". Satu role dengan 40
  permission harus tetap SATU entri. Dijaga assertion biaya-per-role di
  `TestJWTCodec_UkuranTokenTumbuhSesuaiJumlahRole` — ia gagal bila muatan per role bertambah.
- **Jangan memangkas role agar token muat.** Login akan terasa berhasil lalu permission ditolak
  acak bergantung role mana yang terpotong: kegagalan otorisasi senyap, lebih buruk dari 409.
- **`MaxBytes` kosong bukan "tanpa pagar"** — ia berarti default. Tak ada nilai yang mematikan
  pagar; deployment tanpa proxy menuliskan angka besar secara eksplisit.
- **`Metrics` & `Logger` di `token.Options` wajib diisi di produksi.** Nil hanya untuk unit test:
  pagar tetap menegakkan, tapi penolakan yang tak ter-log/tak ter-metrik hanya memindahkan
  kegagalan senyap dari proxy ke aplikasi. Token sendiri TIDAK PERNAH ikut ter-log — ia
  kredensial, sekalipun tak terpakai.
- **Token di atas 80% ambang ter-log `Warn` walau LOLOS.** Itu satu-satunya peringatan dini yang
  hidup hari ini: histogram `auth_token_bytes` & counter `auth_token_oversize_total` belum bisa
  di-scrape sampai `GET /metrics` ter-mount (PR-W6). Jangan menghapus log itu "karena sudah ada
  metrik".
- **Saat mendiagnosis "akun terkunci", percayai LOG, bukan status yang dilihat user.** Kuota login
  dipakai juga oleh percobaan yang berhasil (tanpa `Reset`, sengaja), jadi akun yang terkunci pagar
  akan berpindah dari 409 `TOKEN_TOO_LARGE` ke 429 bila ia mencoba berulang — dan 429 menunjuk
  sebab yang salah.
- **Ambang token & `http.Server.MaxHeaderBytes` harus koheren.** Yang kedua DITURUNKAN dari yang
  pertama di `cmd/server.maxHeaderBytes`; menyetelnya sendiri membuka pagar kedua yang menolak
  token yang baru saja dinyatakan sah.

## Sentinel SYSTEM actor (PR-W2)

`domain.SystemActorID` = `00000000-0000-0000-0000-000000000001`, satu baris NYATA di `id.persons`
yang diseed migrasi `010_seed_system_actor`. Ia ada karena `assigned_by` pada
`id.tenant_assignments` & `id.central_role_assignments` NOT NULL ber-FK ke `id.persons` — aturan
yang benar, tapi yang membuat penugasan PERTAMA mustahil (admin pertama tak punya siapa pun yang
bisa menugaskannya). Melonggarkan FK menghapus ketelusuran SELURUH baris demi satu baris pertama.

- **`nik_enc`/`nik_bidx`-nya bytea ZERO-LENGTH, bukan NULL** (kolomnya NOT NULL; migrasi tak punya
  KeyProvider). Ketiga sifatnya diandalkan: `FieldSealer.Open` memetakan kolom kosong → string
  kosong sehingga `FindByID` tak error; `FindByNIK("")` **tidak** menemukannya karena bidx dari
  `""` adalah HMAC, bukan bytes kosong; `Person.Validate` menolak NIK kosong sehingga sentinel tak
  bisa dibuat ulang/ditimpa lewat `PersonRepo.Save` — penulisnya hanya migrasi.
- **Ia AKTOR, bukan kredensial.** Tak punya credential, tak bisa login, tak pernah jadi subjek
  permission. Jangan memberinya role atau memakainya sebagai `PersonID()` konteks.

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
  (request → nil senyap, verify → 401 seragam, **login password → 401 seragam**). Berlaku sama
  untuk `passwordAuthenticator` (PR-W1): hanya lapis MENTAH (pra-lookup) yang boleh 429.
- Menambahkan verifikasi kredensial sendiri di alur login baru alih-alih memakai
  `passwordAuthenticator`. Dua salinan aturan brute-force akan menyimpang saat salah satunya
  diperbaiki — alasan yang sama dengan `crypto.FieldSealer`.
- **Menambahkan `return errInvalidCredential()` lebih awal di `authenticate`** — mis. "hemat satu
  panggilan bcrypt kalau kredensialnya jelas-jelas tak ada". Itulah cacat yang ditutup di
  REVIEW_BACKLOG A11: 401 yang seragam tak menutup apa pun selama jalur tanpa bcrypt (~2-5 ms)
  bisa dibedakan dari jalur dengan bcrypt (~50-100 ms) — satu request per target sudah memastikan
  sebuah pengenal terdaftar. Sesudah lapis-1, SEMUA jalur harus jatuh ke titik `Verify` tunggal
  (`hash`+`eligible`), termasuk kredensial tak ada, `secret_hash` kosong, dan kuota lapis-2 habis.
  Rate limit tidak menggantikannya (penyerang cuma butuh 1-3 sampel per target).
- **Merakit `passwordAuthenticator` tanpa `VerifyGate` bersama**, atau membuat gerbang sendiri per
  use case. Penyeragaman biaya kerja membuat SETIAP percobaan anonim membayar bcrypt (~60-100 ms
  CPU, naik 20-50×), dan lapis-1 tak membendungnya karena ber-key nilai mentah — nilai acak berbeda
  tiap request tak menyentuh kuota mana pun. Tanpa batas concurrency, `/auth/*` menjadi jalur
  menjenuhkan CPU seluruh proses; dengan gerbang terpisah per use case, batasnya berlipat sebanyak
  permukaan login. Satu instance dirakit di `cmd/server.newAuthHandler` dan diteruskan ke keduanya.
- **Menulis hash tiruan sebagai konstanta bcrypt di kode** alih-alih memintanya ke
  `port.PasswordVerifier`. Cost-nya akan tertinggal saat bcrypt dinaikkan, jalur tiruan menjadi
  lebih murah dari jalur asli, dan celah timing terbuka lagi tanpa satu pun test atau linter
  mengeluh. Alasan yang sama membuat `newPasswordAuthenticator` panic bila `Hash` gagal:
  hash tiruan kosong mematikan kontrol ini tanpa jejak.
- **Mengirim OTP ke `in.CredValue`.** Sebelum blind index nilai itu selalu identik dengan yang
  terdaftar; sekarang tidak. `"victim@x.id\n"` me-resolve ke kredensial nyata lalu ditolak SMTP
  sebagai header injection (500), sedangkan alamat asing menjawab 200 — satu probe per target,
  dan probe-nya sekalian menimpa OTP korban yang sedang berjalan.
- **Menaruh rute `/admin/identity/*` di top mux meniru `/auth/*`.** Pemasangan kedua grup itu
  BERLAWANAN dan alasannya berlawanan: login pra-otentikasi, administrasi identitas justru
  permukaan mutasi paling sensitif. Gerbang `RequirePermission` di handler TIDAK menutup
  kekeliruan ini — pada request anonim `gateway.Context` tak punya evaluator, dan tanpa evaluator
  `RequirePermission` bersifat PERMISIF. Jadi rute yang lolos RequireAuth juga lolos permission,
  dan `POST /admin/identity/assignments` menjadi cara siapa pun menugaskan dirinya ke tenant mana
  pun. Dikunci `cmd/server/admin_identity_routes_test.go`.
- **Menambah permission `identity:*` ke role TENANT** (atau melonggarkan pagarnya di
  `tenantrole/domain.reservedPermissionPrefix`). `permission.Engine` menggabungkan grant lintas
  lapis secara UNION, jadi satu role tenant yang memberi `identity:credential:buat` cukup untuk
  membuat admin tenant menerbitkan kredensial bagi person mana pun yang id-nya ia ketahui —
  termasuk admin platform yang ter-clone ke tenantnya — lalu login sebagai orang itu. Permission
  identity HANYA lewat role sentral (REVIEW_BACKLOG B6).
- **Mengira `CreateCredential` aman karena ber-permission.** Ia menerima `person_id` MANA PUN, dan
  UNIQUE-nya `(cred_type, cred_value)` — bukan `person_id` — jadi kredensial TAMBAHAN untuk orang
  yang sudah punya kredensial tetap berhasil. Karena login me-resolve murni lewat
  `(cred_type, cred_value) → person_id`, menerbitkan kredensial SETARA dengan menjadi target.
  Sejak PR-W3a jalur ini dijaga containment (lihat §Containment), tapi gerbangnya PER USE CASE:
  pemanggil baru (CLI, importer, workflow action) TIDAK otomatis terlindungi — panggil
  `containment.go` juga, dan baca REVIEW_BACKLOG **B7** lebih dulu.
- **Merakit `CreateCredential` tanpa `VerifyGate` bersama.** Alasannya identik dengan jalur login:
  bcrypt terikat CPU dan gerbang per permukaan melipatgandakan batas yang ingin ditegakkan. Rute
  admin memang ber-token, tapi rate limit gateway per-principal ada di orde ratusan rps.
- **Menghitung hash password sendiri di jalur tulis** alih-alih memintanya ke
  `port.PasswordVerifier.Hash`. Cost bcrypt akan menyimpang dari sisi verifikasi begitu salah
  satunya dinaikkan, dan kredensial baru diam-diam menjadi lebih murah dipecahkan daripada yang
  lama. Alasan yang sama dengan larangan menulis hash tiruan sebagai konstanta (butir
  `newPasswordAuthenticator` di atas).
- **Memasukkan `secret_hash` ke diff audit.** Hash bcrypt bisa di-crack offline, jadi kompromi
  satu tabel audit menjadi kompromi seluruh password — sementara nilainya tak menjawab pertanyaan
  yang audit ada untuk menjawabnya ("siapa membuat kredensial ini, kapan, untuk siapa").
  Menyegelnya pun bukan jawaban: ia tak pernah perlu dibaca lagi. Lihat `credentialFields`.
- **Memantulkan pengenal kembali di badan respons endpoint admin.** Respons mengalir ke log
  akses, proxy, dan cache idempotency — jalur samping yang sama yang ditutup ADR-009 §6. Pemanggil
  sudah tahu nilai yang ia kirim; yang belum ia tahu hanyalah id.
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
- ADR-007 (token internal HS256) + **ADR-020 (pagar ukuran token)**, ADR-019 (containment wewenang)
