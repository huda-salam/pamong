# Security Review Backlog — Pamong Framework

Daftar **permukaan sensitif keamanan** yang ditandai untuk **review keamanan terfokus**
(deferred checking). Tujuannya: ketika menjalankan `/security-review` atau audit manual,
reviewer bisa langsung ke area berisiko tinggi tanpa menyapu seluruh repo.

Phase 2 (identity, tenancy & auth) = fondasi authn/authz → hampir semuanya security-relevant.
Dokumen ini menyaring ke **hotspot** yang benar-benar menentukan: eskalasi privilege,
kebocoran lintas-tenant, kripto token, dan integritas audit.

## Cara pakai
- Review per-area (A–F). Tiap entri: **file** · **properti yang dijaga** · **status**.
- **Status:** `HARDENED` (sudah ditinjau & diperketat saat dev — jangan re-litigasi tanpa
  temuan baru) · `OPEN` (belum pernah di-review keamanan khusus) · `DEFERRED` (belum dibangun,
  review saat PR-nya hadir).
- Karena workflow `kerja di main + push`, jalankan review **sebelum push** (lihat catatan
  workflow di bawah) agar tool punya diff terhadap `origin/HEAD`.

---

## A. Otentikasi & token — Phase 2.4

### A1. Codec JWT (PR-2.4.1) — `HARDENED`, perlu konfirmasi reviewer
- `identity/adapter/token/jwt.go`
- Properti: pin algoritma (`WithValidMethods` + keyfunc `*SigningMethodHMAC` → tolak
  `alg=none` & alg-confusion); `exp` wajib & diverifikasi; `iss`/`aud` internal dicek;
  cek revocation **setelah** tanda tangan sah; **fail-closed** saat store revocation error;
  secret tak pernah di-log; `jti`/`sub` di-parse sebagai uuid.
- Cek lanjutan: timing-safe compare (delegasi ke golang-jwt — konfirmasi versi), tak ada
  jalur yang mengembalikan Claims sebelum semua validasi lulus.

### A2. Konfigurasi secret token (PR-2.4.1) — `HARDENED`
- `core/config/schema.go` (`AuthConfig`, `Validate`), `config/default.yaml`
- Properti: secret wajib & ≥32 byte di production; `default.yaml` `token_secret: ""`
  (tak ada secret ter-commit). Cek: tak ada fallback diam-diam ke secret kosong di runtime.

### A3. Error 401 mapping (PR-2.4.1) — `HARDENED`
- `core/errors.go` (`ErrUnauthorized`), `gateway/response.go`
- Properti: kegagalan otentikasi → 401 (bukan 403/500); store-error → 500 (fail-closed),
  bukan 401-lalu-lolos.

### A4. Auth middleware + populasi context (PR-2.4.2) — `HARDENED`
- `gateway/middleware/auth.go`, `gateway/context.go`
- Properti: ekstraksi bearer aman (`CutPrefix`, hanya prefix `Bearer `); token invalid/revoked
  → 401; tanpa token → anonimus (eval nil = permisif; penolakan TIDAK dari RequirePermission —
  route publik vs internal dipisah saat registrasi router). Engine dibangun per-request via
  EvaluatorFactory dari Claims (tak bocor antar request/tenant).
- **`HasCentralRole` scope-blind (catatan):** Context hanya membawa NAMA role, bukan katalog
  global-vs-scoped → `HasCentralRole` true di tenant mana pun. Mitigasi: otorisasi WAJIB lewat
  `RequirePermission` (Engine menegakkan scope), bukan `HasCentralRole` (hint UI saja); invariant
  scope difilter saat login di A5 (CentralRoleResolver hanya memasukkan nama role yang berlaku).
- **Vektor `X-Tenant-ID` ditutup:** lihat C1 — header dihapus total, tenant hanya dari klaim
  tersigning.

### A5. Alur login employee/citizen (PR-2.4.3) — `HARDENED`, perlu konfirmasi reviewer
- `identity/usecase/login.go` (helper+invariant), `login_employee.go` (LoginEmployee+SelectTenant),
  `login_citizen.go`; `identity/adapter/auth/password.go` (bcrypt), `port/password.go`.
- Properti yang dijaga:
  - **Respons kegagalan SERAGAM** (`errInvalidCredential` → 401) untuk credential tak ada / hash
    kosong (SSO/OTP-only) / password salah / person non-aktif → tak membocorkan bagian yang gagal.
  - **Verifikasi password timing-safe** (bcrypt `CompareHashAndPassword`); password >72 byte ditolak
    (cegah pemotongan diam-diam); hash tak pernah di-return ke jalur lain.
  - **Persona ditentukan jalur masuk, bukan tipe orang:** employee hanya NIP/NIK; citizen hanya
    NIK/email/no_hp (silang ditolak). LoginCitizen **tidak pernah** memanggil resolver role →
    token citizen mustahil membawa role internal (ASN login publik = warga murni). **Diuji**:
    `TestLoginCitizen_Success_NoInternalRoles`.
  - **Reject tanpa employment aktif** (orang biasa tak bisa masuk internal) + tanpa penugasan
    tenant aktif; tenant non-aktif tak ditawarkan.
  - **INVARIANT scope difilter saat login**: hanya role yang berlaku untuk (person, tenant) yang
    dibakar (central via `CentralRoleResolver.EffectiveRoles(person, tenant)`; tenant via resolver
    terikat DB tenant). **Diuji**: `TestLoginEmployee_ScopeFiltered_NoCrossTenantRoleLeak`.
  - **Pemilihan tenant aman**: token sementara (multi-tenant) tanpa tenant & tanpa role (hanya bisa
    panggil SelectTenant); `SelectTenant` ambil person_id dari klaim tersigning (bukan input) &
    menolak tenant di luar penugasan aktif / persona non-employee.
- Cek lanjutan reviewer: tak ada jalur yang menerbitkan token sebelum verifikasi tuntas; token
  sementara benar-benar tak berdaya (RequirePermission menolak karena role kosong).
### A6. Jalur OTP citizen + rate-limit (PR-2.4.4) — `HARDENED`, perlu konfirmasi reviewer
- `identity/usecase/request_otp.go`, `verify_otp.go`, `otp.go` (policy+helper seragam);
  `identity/adapter/auth/otp.go` (crypto/rand + bcrypt); `identity/adapter/db/otp_repository.go`;
  `identity/domain/otp.go`; `port/{otp,messaging,ratelimit}.go`; `infra/ratelimit/memory.go`;
  `core.ErrTooManyRequests` (429); migrasi `006_create_otps`. ADR-008.
- Properti yang dijaga:
  - **Kode OTP `crypto/rand`** (bukan math/rand; uniform tanpa bias modulo), disimpan sebagai
    **hash bcrypt** (bukan plaintext), tak pernah di-log/di-return; `Verify` timing-safe (bcrypt).
    Kripto hanya di adapter — domain & use case bebas dependency (cermin PasswordVerifier).
  - **Respons kegagalan SERAGAM** (`errInvalidOTP` → 401) untuk credential tak ada / OTP tak ada /
    kedaluwarsa / sudah dipakai / attempts habis / kode salah / person non-aktif — tak membocorkan
    tahap yang gagal.
  - **Sekali pakai + cap tebak**: OTP di-`Consume` SEBELUM token terbit (replay tertutup); cap
    `MaxOTPAttempts=5` per OTP menghanguskan saat habis; verify menilai OTP **terbaru** per credential.
  - **Token citizen tanpa role internal**: resolver role TAK PERNAH dipanggil di jalur OTP (sama
    seperti LoginCitizen password). **Diuji**: `TestVerifyOTP_Success_IssuesCitizenToken_NoInternalRoles`.
  - **Enumeration-resistant pada RequestOTP**: credential tak dikenal / person non-aktif → sukses
    senyap tanpa kirim. **CATATAN reviewer**: kegagalan *pengiriman* (provider down) → error 500 →
    sedikit sinyal "akun ada" saat outage. Trade-off sadar (UX retry) — ADR-008 §deferred (refinable:
    swallow + log / enumeration-resistance penuh).
  - **Rate-limit per-kredensial (Opsi B)** di use case via `port.RateLimiter` (bukan per-IP gateway):
    request 3/15mnt, verify 10/15mnt; limiter error → **fail-closed** (aksi tak lanjut). **Diuji**:
    `TestRequestOTP_RateLimited`, `TestRequestOTP_LimiterError_FailClosed`, `TestVerifyOTP_RateLimited`.
  - **Kuota BERLAPIS DUA sejak PR-3.8.6** (lihat A7) — lapis mentah pra-lookup + lapis kanonik
    per credential ID pasca-lookup.

### A7. Kuota OTP di-key pada nilai mentah (regresi PR-3.8.6) — `TERTUTUP` (PR-3.8.6)
- Ditemukan security review PR-3.8.6, diperbaiki di PR yang sama.
- **Cacat.** PR-3.8.6 memindahkan pencarian kredensial ke blind index, yang menormalkan nilai lebih
  dulu (trim untuk semua purpose, case-fold untuk `email`). `otpRequestKey`/`otpVerifyKey` tetap
  di-key pada string permintaan MENTAH, sehingga `budi@x.id`, `Budi@x.id`, dan `" budi@x.id"`
  me-resolve ke SATU kredensial tetapi menempati bucket limiter yang berbeda-beda. Kuota penerbitan
  (3/15mnt) dan kuota verifikasi (10/15mnt) karena itu bisa dilipatgandakan tanpa batas hanya dengan
  mengubah huruf besar/kecil atau menambah spasi. Yang tersisa hanya cap per-OTP
  (`MaxOTPAttempts=5`) — yang memang dirancang sebagai SETENGAH dari proteksi (`identity/domain/otp.go`):
  penyerang me-mint OTP tanpa batas (OTP bombing + membatalkan kode yang sedang dipakai korban),
  masing-masing memberi 5 tebakan baru atas ruang 6 digit.
- **Perbaikan.** Kuota dipecah dua lapis, keduanya diperlukan:
  - Lapis 1 (`otpRequestKey`/`otpVerifyKey`, nilai mentah, PRA-lookup) — tetap ada demi
    enumeration-resistance: nilai yang tak dikenal pun tertahan, sehingga keberadaan akun tak
    terbaca dari laju.
  - Lapis 2 (`otpCredRequestKey`/`otpCredVerifyKey`, **ID kredensial**, PASCA-lookup) — kuota yang
    sebenarnya, kanonik by construction.
  Habisnya lapis 2 dibuat TAK BISA DIBEDAKAN dari jalur normal: request → `nil` senyap (sama dengan
  credential tak dikenal), verify → 401 seragam. Kalau ia menjawab 429, lapis 2 justru menjadi
  orakel keberadaan akun yang tak dimiliki lapis 1.
- **Kenapa bukan "normalkan saja key-nya".** Itu menyalin tabel kebijakan `infra/crypto`
  (`caseFoldedPurposes` + trim) ke lapis use case — yang justru tak boleh menyentuh kripto
  (`infra/crypto/CLAUDE.md` §Tidak boleh) — dan menciptakan sumber kebenaran kedua yang bisa
  menyimpang diam-diam. ID kredensial sudah hasil resolusi, jadi tak punya masalah itu.
- **Diuji + diverifikasi lewat mutasi** (5 mutasi, tiap kali test yang dituju gagal):
  `TestRequestOTP_EjaanBerbedaBerbagiKuotaKredensial`,
  `TestRequestOTP_KuotaKredensialHabis_DiamSepertiTakDikenal`,
  `TestVerifyOTP_EjaanBerbedaBerbagiKuotaKredensial`,
  `TestVerifyOTP_KuotaKredensialHabis_SeragamUnauthorized`. Mutasi yang paling penting: mengganti
  key lapis 2 kembali ke nilai mentah (yaitu "perbaikan" yang salah) — keempat test gagal.
- **Cacat kedua dari akar yang sama** — ditemukan code review putaran kedua, diperbaiki di PR ini.
  `RequestOTP` mengirim kode ke `in.CredValue` (nilai permintaan), bukan ke alamat kredensial yang
  ter-resolve. Sebelum PR-3.8.6 keduanya selalu identik karena lookup-nya `WHERE cred_value = $1`
  eksak; blind index membuat mereka bisa berpisah. `normalize()` memakai `strings.TrimSpace`, yang
  **ikut membuang CR/LF**, jadi `"victim@x.id\n"` me-resolve ke kredensial `"victim@x.id"` — OTP
  dibuat & disimpan, lalu `SMTP.SendEmail` menolak alamat ber-CRLF sebagai header injection →
  `errOTPSendFailed` (500), sementara alamat tak terdaftar tetap menjawab `nil` (200). Orakel
  keberadaan akun dengan **satu probe per target**, dan lapis 1 di-key pada nilai mentah sehingga
  tiap target mendapat bucket segar. Probe itu juga menimpa OTP korban yang sedang berjalan
  (`FindLatestByCredential` mengambil yang terbaru). Tanpa serangan pun jalur ini patah untuk warga
  yang menempelkan alamat berspasi: lookup sukses, kirim gagal.
  **Perbaikan:** tujuan kirim diambil dari `cred.CredValue` (alamat kanonik sebagaimana didaftarkan,
  hasil dekripsi kolom `_enc`). Diuji `TestRequestOTP_KirimKeAlamatKanonikBukanNilaiPermintaan`
  (tiga ejaan: case, spasi, CRLF); mutasi mengembalikannya ke `in.CredValue` membuat ketiganya gagal.
- **Cacat ketiga dari akar yang sama, di jalur TULIS** — ditemukan code review putaran ketiga,
  diperbaiki di PR ini. Dua cacat di atas menutup jalur BACA dengan memakai nilai kanonik hasil
  resolusi; tapi tak satu pun yang mengkanonikalisasi nilai yang MASUK. `seal()` mengenkripsi
  verbatim sementara `index()` menormalkan, dan `Credential.Validate()` hanya menolak nilai
  kosong — jadi kredensial ber-CRLF/spasi-tepi bisa TERSIMPAN. Begitu tersimpan, `cred.CredValue`
  (perbaikan cacat kedua) sendiri yang menjadi alamat ber-CRLF: SMTP menolaknya → 500 untuk akun
  terdaftar vs 200 untuk yang tidak. Orakel yang sama persis, hanya pindah dari jalur baca ke
  jalur tulis, dan kali ini bertahan permanen di baris DB alih-alih per-permintaan.
  **Perbaikan di DOMAIN, bukan di `seal()`.** `identity/domain.bentukPengenalRusak` menolak
  control character (CR/LF/TAB/NUL/DEL/C1) dan spasi tepi (`unicode.IsSpace`, termasuk NBSP);
  dipakai `Credential.Validate` (`cred_value`) dan `Person.Validate` (`email`, `no_hp` — keduanya
  ikut ke clone `gov.user_profiles` lalu menjadi alamat kirim notifikasi, PR-N3b/ADR-013).
  Menormalkan di `seal()` ditolak sebagai perbaikan: ia membuat nilai terdekripsi ≠ nilai yang
  didaftarkan (dump berbeda dari yang diketik warga, tanpa jejak) dan menyalin tabel kebijakan
  `infra/crypto` ke lapis repo — kesalahan yang sama dengan "normalkan saja key-nya" di atas.
  Menolak di pintu masuk menjaga verbatim tetap verbatim: yang tersimpan hanya yang sudah kanonik.
  Diuji `TestCredential_Validate_BentukPengenal` (14 kasus) + `TestCredential_Validate_KanonikSetelahLolos`
  (mengikat aturan pada ALASANNYA: apa pun yang lolos harus sama dengan bentuk ter-`TrimSpace`-nya)
  + kasus `email`/`no_hp` di `TestPerson_Validate`. Diverifikasi lewat 3 mutasi (lumpuhkan cabang
  spasi-tepi; lumpuhkan `unicode.IsControl`; cabut aturannya dari `Credential.Validate`) — tiap
  kali test yang dituju gagal.
  **Susulan (code review putaran keempat, diperbaiki di PR ini): aturannya belum punya penegak.**
  `Credential.Validate` tak dipanggil satu pun kode produksi — belum ada use case penulis
  credential — dan `Person.Validate` hanya kebetulan dipanggil `CreatePerson`. Aturan di atas
  karena itu masih dokumentasi: penulis use case credential pertama tinggal lupa memanggilnya dan
  orakel jalur tulis kembali terbuka utuh. **Penegakan dipindah ke PINTU TULIS repo** —
  `PersonRepo`/`EmploymentRepo`/`CredentialRepo.Save` memanggil `Validate()` sebelum `seal()`,
  dengan alasan yang sama seperti enkripsi & audit dipasang di lapis repo: yang bergantung pada
  ingatan tiap penulis use case pasti terlewat. Yang pindah adalah PEMANGGILANNYA, bukan
  aturannya — tabel kebijakannya tetap di domain. Diuji
  `TestRepoIdentity_MenolakNilaiCacatDiPintuTulis` (DBConn yang menggagalkan test bila tersentuh,
  jadi yang dibuktikan "tak pernah sampai ke DB", bukan sekadar "error dikembalikan"); mutasi
  mencabut `Validate()` dari `CredentialRepo.Save` → test gagal lewat conn yang tersentuh.
- **Pelajaran yang berlaku umum:** setiap kali pencarian berpindah ke blind index, SEMUA yang
  diturunkan dari nilai pencarian ikut berubah artinya — bukan hanya yang di-*key* padanya.
  Rate limiter, cache, dan idempotency key harus di-key ulang pada hasil resolusi; dan nilai
  permintaan berhenti layak dipakai sebagai **alamat tujuan, parameter kirim, atau apa pun yang
  mengalir ke sistem luar**. Sesudah lookup, yang kanonik hanyalah baris yang ditemukan.
  Dan sisi lainnya, yang baru terlihat di putaran ketiga: kalau nilai yang TERSIMPAN sendiri tak
  kanonik, memakai "baris yang ditemukan" tidak menyelamatkan apa pun — pintu masuknya harus ikut
  ditutup, di domain, sebagai invariant.
- Cek lanjutan reviewer: TOCTOU `FindLatest`→`IsUsable`→`Consume` di bawah verify konkuren kode
  benar → paling jauh token ganda utk person yang SAMA (bukan eskalasi); dinilai non-konkret. Opsi C
  (lapis gateway per-IP + Redis multi-instance) ditunda di balik `port.RateLimiter` (additive).
- **DEFERRED(Phase-2.4/PR-2.4.x):** ~~jalur OTP + rate-limit~~ SELESAI di PR-2.4.4. Live wiring
  HTTP/messaging/ratelimit konkret menyusul Phase 5.1.1 (router). Konfigurasi `OTPPolicy` dari
  `core/config` saat ada kebutuhan tenant.
- **PR-2.4.5 `HARDENED`:** `identity/usecase/assign_employment_tenant.go` — `validateAssignment`
  kini menegakkan 3 invariant bisnis: employment aktif (`IsActiveAt`), tenant ada & aktif di
  registry, anti-duplikat penugasan aktif ke tenant yang sama. Tidak ada permukaan kripto/token
  baru; otorisasi (`PermAssignmentCrossTenant`) sudah ada sejak PR-2.2.4 dan tetap di baris pertama.
  Security review inline sebelum commit: tidak ada temuan ≥ MEDIUM.

### A8. Kanal OTP `no_hp` tanpa driver SMS — `OPEN` (pra-existing, terlihat saat PR-3.8.6)
- `infra/messaging.SMTP.SendSMS` selalu mengembalikan `MsgErrPermanent` (driver email-only).
  Akibatnya permintaan OTP `no_hp` untuk nomor **terdaftar** berakhir `errOTPSendFailed` (500),
  sementara nomor tak dikenal menjawab `nil` (200) — bentuk orakel keberadaan akun yang sama
  dengan A7, tetapi lewat ketiadaan driver, bukan lewat kode.
- **Bukan regresi PR-3.8.6** dan sengaja TIDAK ditambal di sana: perbaikannya sebuah keputusan,
  bukan sebaris kode. Pilihannya (a) menelan kegagalan kirim menjadi `nil` — menutup orakel tapi
  menghapus diagnostik kegagalan kirim yang nyata, atau (b) menolak kanal `no_hp` di konfigurasi
  sampai ada driver SMS betulan. Rekomendasi: (b) — gerbang konfigurasi saat wiring messaging,
  karena (a) menyembunyikan kerusakan operasional dari operator.
- Selama belum diputuskan: jangan aktifkan `no_hp` sebagai kanal OTP di environment mana pun
  yang memakai driver `smtp`.

---

## B. Otorisasi (RBAC + ABAC) — Phase 2.3

### B1. Resolusi konflik permission (PR-2.3.3) — `HARDENED`
- `core/permission/engine.go`, `core/permission/composite.go`
- Properti: global menang tanpa syarat (termasuk atas strict); antar role non-global perm
  biasa=union, strict=intersection. Cek: tak ada kombinasi yang menghasilkan eskalasi
  (mis. global non-grant tak boleh "menetralkan" deny secara salah); composite mendahulukan
  central agar tenant tak men-shadow global.

### B2. Scope ABAC + delegasi bypass-strict (PR-2.3.5) — `HARDENED`
- `core/permission/scoped_engine.go`, `core/permission/scope.go`
- Properti: `AllowsInUnit` 2-tahap = (RBAC `Engine.Allows` UTUH **AND** RoleGrants cover unit)
  **OR** DelegatedGrants cover unit. Delegasi **sengaja** bypass strict-intersection (inti PLT) —
  reviewer wajib mengonfirmasi ini memang diinginkan & tak bisa disalahgunakan untuk eskalasi
  di luar subset yang didelegasikan. Cek covers: TenantWide/unit==res/(Subtree&&IsWithin).

### B3. Fail-closed scope central role (PR-2.3.2) — `HARDENED`
- `identity/adapter/db/central_role_resolver.go`, `identity/domain/central_role.go` (`AppliesTo`)
- Properti: otoritas global vs scoped = `scope_type` (BUKAN kekosongan `tenant_scope`). Scoped
  dengan scope kosong → **tak berlaku di mana pun** (cegah eskalasi ke semua tenant). Cek tetap
  fail-closed setelah perubahan apa pun.

### B4. Hierarki OPD (recursive CTE) (PR-2.3.5) — `OPEN`
- `tenantrole/adapter/db/hierarchy.go`, `tenantrole/adapter/db/scoped_resolver.go`
- Properti & cek: recursive CTE subtree **aman dari siklus** (parent_id melingkar → batas
  kedalaman / cycle guard?); isolasi per-tenant struktural (resolver konek ke tenant DB-nya,
  tanpa parameter tenantID yang bisa salah-arah).

### B5. Delegasi/PLT (PR-2.3.5) — `OPEN`
- `delegation/domain/policy.go`, `delegation/usecase/create_delegation.go`,
  `delegation/adapter/db/scoped_resolver.go`
- Properti & cek: `NonDelegable` ditegakkan saat buat (permission sensitif tak bisa
  didelegasikan); expiry **lazy** benar (`ListActiveByDelegatee` filter `valid_until` di SQL →
  delegasi kedaluwarsa tak ter-resolve); delegatee tak bisa men-sub-delegasikan di luar subset.

---

## C. Isolasi tenant — Phase 2.2

### C1. Tenant resolver (PR-2.2.2, diperketat PR-2.4.2) — `RESOLVED`
- `gateway/middleware/tenant_resolver.go`
- **Keputusan (PR-2.4.2):** header `X-Tenant-ID` **dihapus total**. `extractTenantID` lama
  (klaim → fallback header) diganti: tenant_id **hanya** dari klaim JWT tersigning (HS256).
  Menutup vektor eskalasi lintas-tenant: klien (citizen/anonimus) tak bisa lagi memalsukan/
  menarget tenant lewat header tak-tersigning. Regression test `TestTenantResolver_HeaderDiabaikan`
  mengunci properti ini (header-only → tanpa tenant; klaim menang atas header).
- Validasi registry tetap: tenant tak dikenal→404, nonaktif→403 (defense-in-depth bila token
  membawa tenant yang sejak itu dinonaktifkan). Tiap request hanya membawa tenant-nya sendiri.
- **Flow tanpa token yang perlu menarget tenant** (service/CLI/cross-tenant admin) ditunda;
  bila dibutuhkan → mekanisme ber-permission & ter-audit (service token ber-claim / endpoint
  tenant-switch yang menerbitkan token scoped baru), lewat ADR — **bukan** header mentah.

### C2. Routing pool DB per-tenant (PR-2.2.3) — `OPEN` (prioritas tinggi)
- `infra/db/conn_manager.go`
- Properti & cek: pool di-key per `(host, dbname)` → request tenant A **tak pernah** menyentuh
  DB tenant B; routing central vs tenant via residency benar; kegagalan open tak di-cache
  (retry) tak menimbulkan pemakaian pool yang salah.

### C3. Sync clone ke tenant (PR-2.2.4) — `OPEN`
- `identity/sync/writer_tenantdb.go`, `identity/sync/clone.go`
- Properti & cek: clone ke `gov.user_profiles` **tak memuat credential/password** (CLAUDE.md:
  "JANGAN tambah kolom credential atau password di sini"); hanya person dengan tenant
  assignment yang di-clone ke tenant tujuan (tak bocor identitas lintas tenant tak berhak).

---

## D. Provisioning & privilege boundary — Phase 2.2.3 (ADR-006)

### D1. Provisioner (CREATE DATABASE) — `HARDENED`
- `infra/db/provision.go`
- Properti: identifier (dbname/owner) dari registry divalidasi `identRe` + `quoteIdent`
  (cegah SQL injection lewat identifier); kredensial admin CREATEDB **terpisah** dari runtime
  (least privilege); OWNER=app user, migrasi sebagai app user. Cek: tak ada interpolasi
  identifier tanpa quote; maintenance DB tak menerima input bebas.

---

## E. Audit & PII — Phase 2.1.3 (ADR-002/003)

### E1. Hash chain audit identity — `OPEN`
- `identity/adapter/db/audit_store.go`, `core/audit/*`
- Properti & cek: chain immutable; tamper terdeteksi (sudah ada test); satu chain sentinel
  `tenant_id="central"` tak bisa dipalsukan untuk menyisipkan entri. **Sentinel dipindah ke
  `crypto.RealmCentral` (`_central`) di PR-3.8.6** — nilai lama LOLOS `tenantIDRe`, jadi tenant
  bernama `central` bisa melebur ke chain sentral. Tertutup secara struktural.
- **Gap baru (PR-3.8.6, belum ditutup): entri audit identity tak punya jalur baca lewat framework.**
  `core/audit.Reader` menggerbangi pembukaan diff dengan `audit:sensitive:baca` dan hanya
  menampilkan entri yang `TenantID`-nya cocok dengan tenant aktor. Realm `_central` **by
  construction** tak akan pernah menjadi klaim tenant siapa pun, jadi diff identity yang kini
  tersegel tidak bisa dibuka oleh siapa pun lewat `Reader` — test memulihkannya dengan memanggil
  `CryptoPort.Decrypt` langsung, yang MELANGKAHI gerbang permission.
- Ini **laten**: belum ada pemanggil produksi yang merakit `audit.NewReader` (Phase 5.x). Tapi ia
  bukan tambalan satu baris — pertanyaannya "siapa yang berwenang membaca audit sentral, dan atas
  dasar apa", dan jawabannya kemungkinan besar central role platform (`platform_auditor`) alih-alih
  pencocokan tenant. Diputuskan saat jalur baca audit dirakit; jangan diam-diam melonggarkan
  pencocokan tenant di `Reader` untuk "membuat `_central` lewat".

### E2. PII di audit (NIK mentah) — `TERTUTUP` (PR-3.8.3/3.8.4 + PR-3.8.6)
- **Tertutup: jalur repository entity tenant** (`gov.audit_logs`). `infra/db.auditedRepo`
  mengenkripsi nilai class `personal_id`/`specific` di diff sebelum persist, dan
  `core/audit.Reader` menggerbangi pembukaannya dengan `audit:sensitive:baca`. Terbukti
  di `infra/db/field_crypto_integration_test.go`: dump `diff` bersih dari NIK, nilainya
  tetap dapat dipulihkan dengan kunci, hash chain tetap verify.
- **Tertutup juga: jalur audit identity** (`id.audit_logs`) di PR-3.8.6. `personFields()` &
  `employmentFields()` tetap menyusun snapshot plaintext (perbandingan before/after harus
  mendahului enkripsi), tapi `auditedPersonRepo`/`auditedEmploymentRepo` kini menyegelnya lewat
  `infra/db.SealAuditDiff` — **mesin yang sama** dengan jalur tenant, bukan salinan, sehingga
  aturan "bandingkan plaintext dulu, segel sesudahnya, penanda gagal per sisi berbeda" tak
  bisa menyimpang di antara keduanya. Kedua konstruktor menolak `CryptoPort` nil, karena
  `SealAuditDiff` yang menerima nil akan meneruskan snapshot APA ADANYA.
- **Keputusan tenant kunci untuk chain sentral → ADR-017.** Sumbu partisi `id.data_keys`
  dibaca ulang sebagai **key realm**; realm sentral memakai token `_central` yang tak bisa
  dipalsukan (gagal `^[a-z]` di `tenantIDRe`) dan custody-nya `platform` sebagai invarian kode,
  bukan baris registry. Sentinel chain audit dipindahkan ke token yang sama — ia HARUS sama,
  karena `audit.Reader` membangun `RowRef.TenantID` dari `entry.TenantID` untuk membuka diff.
  Itu sekaligus menutup cacat laten sentinel lama `"central"`, yang LOLOS `tenantIDRe`.
- Terbukti di `identity/adapter/db/field_crypto_integration_test.go`
  (`TestIdentityCrypto_AuditDiffBersihDariPengenal`) + `audit_integration_test.go`: dump
  `id.audit_logs.diff` tak memuat NIK/NIP/no_hp/email plaintext, nilai non-sensitif
  (`nama_lengkap`) tetap ada sebagai bukti, nilainya dapat dipulihkan dengan kunci realm
  sentral, dan hash chain tetap verify. Dua mutasi (matikan penyegelan diff; terima
  `CryptoPort` nil) diverifikasi MEMBUAT test gagal.
- **Jalur pesan error ikut ditutup di PR yang sama.** `ErrConflict`/`ErrNotFound` repo identity
  tak lagi mengutip NIK/NIP/nilai kredensial: pesan `FrameworkError` mengalir ke log terpusat DAN
  ke body HTTP, jadi mengenkripsi kolom sambil menyalin NIK ke teks error hanya memindahkan
  kebocoran ke tempat yang lebih mudah dibaca daripada dump DB (ADR-009 §6 jalur log/trace).
  Referensi errornya kini menyebut JENIS pencarian (`"nik"`, `"nip"`, `"email"`), bukan nilainya —
  pemanggil sudah tahu nilai yang ia kirim, jadi tak ada informasi yang hilang. Terbukti di
  `TestIdentityCrypto_PesanErrorTanpaPengenal`, diverifikasi lewat mutasi.
- **Sisa yang bukan E2:** clone `gov.user_profiles` masih menerima pengenal plaintext lewat fat
  event — **`nik` dan `nip`, bukan hanya kontak (email/no_hp, ADR-013)**. Perlu disebut eksplisit
  supaya PR ini tidak terbaca sebagai "NIK sudah aman di mana-mana": ia kini terenkripsi di identity
  DB dan di `id.audit_logs`, tetapi tersalin apa adanya ke SETIAP tenant DB yang punya penugasan
  person tersebut — permukaan yang justru lebih lebar dari sumbernya. Jalur samping, cakupan
  PR-3.8.5 / H3.

---

## F. Credential storage — Phase 2.1

### F1. Credential repo — `OPEN` (permukaan berubah di PR-3.8.6 — tinjau ulang)
- `identity/adapter/db/credential_repository.go`, `identity/adapter/db/field_crypto.go`,
  `identity/domain/entity.go` (`Credential`)
- Properti & cek: `secret_hash` = bcrypt (tak pernah plaintext); tak pernah di-SELECT/return ke
  jalur yang tak butuh (mis. resolver login hanya membandingkan hash, tak mengembalikannya);
  tak di-log.
- **Berubah di PR-3.8.6:** `cred_value` kini `cred_value_enc` + `cred_value_bidx`; keunikan
  ditegakkan `uq_credentials_type_value_bidx (cred_type, cred_value_bidx)`. Purpose kunci
  diturunkan dari `cred_type` (ADR-017 §4) → **login lewat email menjadi case-insensitive**;
  reviewer wajib mengonfirmasi ini memang diinginkan dan tak membuka penggabungan akun yang
  tak diharapkan. Pesan error `ErrConflict`/`ErrNotFound` tak lagi memuat nilai kredensial
  (dulu ikut, dan error mengalir ke log — ADR-009 §6).

---

## G. Gateway middleware — Phase 5.1

Temuan review PR-5.1.2b/5.1.2c yang sengaja DITUNDA sampai fitur/caller yang menentukan
kebutuhannya dibangun (keputusan user 2026-07-27) — bukan bug aktif, tapi jangan hilang.

### G1. Buffering body request di middleware Idempotency — `DEFERRED` (edge body-limit)
- `gateway/middleware/idempotency.go` (`io.ReadAll(r.Body)`), calon rumah: server/edge
  (`http.MaxBytesReader` di `cmd/server` atau middleware terluar)
- Properti yang ingin dijaga: batas ukuran body request agar mutasi ber-Idempotency-Key tak
  mem-buffer body tak-terbatas ke memori sebelum handler jalan. **Auth-gated** (hanya principal
  terverifikasi), jadi bukan permukaan anonim; kelas DoS/memory (di luar cakupan security-review).
- Kenapa ditunda: batas ukuran body adalah kebijakan **edge gateway** yang berlaku untuk SEMUA
  handler, bukan tambalan di satu middleware (altitude). Tetapkan saat handler unggah besar
  (mis. lampiran surat) hadir sehingga ambang nyata jelas — reject vs skip-idempotency vs
  stream-to-disk diputuskan bersama kebutuhan itu.

### G2. Fidelitas header saat replay Idempotency — `DEFERRED` (butuh handler non-JSON)
- `gateway/middleware/idempotency.go` (`replayResponse`)
- Properti: replay hanya menyimpan status+body dan memaksa `Content-Type: application/json`;
  header lain (mis. `Location` pada 201) hilang. Aman di bawah kontrak "semua respons framework
  = JSON" (didokumentasikan di komentar), jadi belum ada dampak.
- Kenapa ditunda: revisit saat ADA handler yang menyetel header bermakna (Location/Content-Type
  non-JSON) — saat itu simpan & replay header terpilih. Sampai itu, menyimpan header sewenang-
  wenang = spekulatif.

---

## H. Enkripsi field & key management — sub-phase kripto (ADR-009/010)

### H1. Cakupan enkripsi selektif — `SEBAGIAN DITUTUP (PR-3.8.3/3.8.6)`
- `core/domain/field_types.go` (`FieldDef.Class`), `infra/db` (DDL + repo), `infra/crypto`,
  `identity/adapter/db/field_crypto.go`
- Cek: hanya `personal_id`/`specific` terenkripsi; `nama_lengkap` TIDAK; field terenkripsi
  tak masuk sortable/filterable (kecuali equality); `Unique` di kolom `_bidx`, bukan `_enc`.
- Ditutup untuk entity tenant di PR-3.8.3 dan untuk **identity DB** di PR-3.8.6: `nik`/`nip`/
  `cred_value`/`no_hp`/`email` → `_enc`+`_bidx` dengan kolom plaintext DI-DROP; UNIQUE pindah
  ke `_bidx` (`uq_persons_nik_bidx`, `uq_employments_nip_bidx`,
  `uq_credentials_type_value_bidx`); `nama_lengkap` sengaja TETAP plaintext. Terbukti di
  `identity/adapter/db/field_crypto_integration_test.go` (katalog kolom + UNIQUE + dump).

### H5. Realm kunci sentral — `OPEN` (baru, PR-3.8.6 / ADR-017)
- `infra/crypto/realm.go` (`RealmCentral`, `WithCentralRealm`), `infra/crypto/crypto.go`
  (`NewFromConfig`), `identity/adapter/db/field_crypto.go`, `identity/adapter/db/audit_store.go`
- Properti yang dijaga:
  - Realm sentral memakai **satu** kunci per purpose (bukan per-tenant) — dituntut
    `UNIQUE(nik_bidx)` yang berlaku global se-identity-DB.
  - Token `_central` **tak bisa dipalsukan**: gagal `identity/domain.tenantIDRe` (`^[a-z]…`),
    jadi tak ada tenant yang bisa masuk ke ruang kunci sentral. Dikunci
    `TestRealmSentralTakBisaJadiTenantID`, yang juga menegaskan premisnya (sentinel polos
    `"central"` JUSTRU sah sebagai tenant_id — sebab itu ia ditolak).
  - Custody realm sentral = `platform` sebagai **invarian kode**, tanpa menyentuh registry.
    Dikunci `TestWithCentralRealm_*` (resolver di baliknya tak boleh pernah ditanya) +
    `TestIdentityCrypto_KunciRealmSentralTanpaBarisRegistry` (tak ada baris di
    `id.tenant_registry`, semua DEK ber-realm `_central` & custody `platform`).
  - Dekorator hanya MENAMBAH satu jalur: error & jawaban resolver tenant diteruskan apa adanya
    (fail-closed tetap fail-closed) — `TestWithCentralRealm_ErrorTenantDiteruskan`.
- **Blast radius yang diterima sadar & wajib dikonfirmasi reviewer:** bocornya DEK
  `(_central, nik, bidx)` membuka pencarian NIK seluruh person di semua pemda sekaligus. Itu
  konsekuensi tak terhindarkan dari `UNIQUE(nik)` yang memang global (ADR-017 §Konsekuensi);
  catatan H2 (ruang NIK 16 digit kecil → kunci bidx bernilai tinggi) berlaku penuh di sini,
  kini atas data lintas-pemda. Menaikkan taruhan pada perlindungan master key (ops).
- Cek lanjutan: tak ada jalur yang menyelipkan `RecordID` ke `BlindIndex` realm sentral (akan
  mematikan UNIQUE senyap); tak ada tempat lain yang menuliskan literal `"central"` atau
  `"_central"` selain konstanta bersama.

### H2. Blind index & dictionary attack — `SEBAGIAN DITUTUP (PR-3.8.2)`
- `infra/crypto`, ADR-009
- Cek: kunci bidx TERPISAH & di KMS (bukan DB); HMAC atas nilai ternormalisasi; ruang NIK
  16 digit kecil → kunci bidx bocor = brute-force layak, jadi kunci wajib di luar dump DB.
- Ditutup di PR-3.8.2: kunci bidx adalah DEK `kind='bidx'` TERPISAH dari `kind='enc'`
  (bukan turunan satu DEK), tersimpan hanya dalam bentuk ter-wrap di identity DB — dump
  tenant DB tak memuat kunci apa pun. HMAC-SHA256 atas nilai ternormalisasi (trim; case-fold
  untuk purpose email). Test: `TestBlindIndex_*`, `TestBlindIndex_KunciTerpisahDariKunciEnkripsi`.
- TERSISA: nilai bidx belum dipakai kolom nyata (itu PR-3.8.3/3.8.6) — verifikasi "UNIQUE di
  `_bidx`" ikut H1.

### H3. Jalur kebocoran samping — `SEBAGIAN DITUTUP (PR-3.8.5a/3.8.5b)` (ADR-009 §6, ADR-018)
- audit diff (E2), payload event (NATS stream), `gov.idempotency_keys`, staging table
  migrasi, log/trace (OTEL, query log), clone `gov.user_profiles`
- Cek: tiap jalur tak membocorkan pengenal mentah. Enkripsi kolom saja = teater keamanan.
- TERTUTUP: audit diff, log/trace, clone (3.8.5a), payload event + cache idempotency (3.8.5b).
- SISA: **staging table migrasi** — pipeline legacy-import belum ada; aturannya harus mendarat
  bersama pipeline-nya.
- SISA: **residu plaintext yang terlanjur mengendap** (stream NATS retensi, baris
  `gov.outbox_events` lama). Runbook, bukan kode. Nol dampak selama outbox belum berpemanggil.

### H7. Aturan "payload tanpa `personal_id`" tanpa penegak — `OPEN` (dicatat PR-3.8.5b)
- Aturan ADR-018 Keputusan #2 hidup di ADR + tiga CLAUDE.md + satu test yang hanya menutup
  payload yang sudah ada. Modul baru yang menambah `NIK string` ke payload-nya sendiri lolos
  semua gate. Kelas yang sama dengan "naikkan versi schema" — checklist tanpa mesin.
- Bentuk yang dipertimbangkan: struct tag pada field payload (mis. `pamong:"class=personal_id"`)
  + analyzer yang menolak class terenkripsi di tipe yang terdaftar ke `SchemaRegistry`. Ini
  mengikuti cara industri menegakkannya — anotasi pada schema lalu tooling yang bertindak atas
  anotasi itu (tag PII di Confluent data contracts; `debug_redact`/custom field option + buf
  check plugin di protobuf) — bukan pencocokan nama field yang rapuh.
- Prasyarat: kosakata `DataClass` sudah ada di `FieldDef`; yang belum ada jembatannya ke tipe
  payload event.

### H4. Key custody & rotasi — `SEBAGIAN DITUTUP (PR-3.8.2)` (ADR-010)
- `infra/crypto/envelope.go`, KeyProvider driver, KMS
- Cek: DEK per-tenant per-purpose; DEK ter-wrap tak di tenant DB; `KeyProvider` di balik
  registry (KEK tak pernah keluar KMS); custody per-tenant (`key_custody`) diresolusi benar
  (platform vs tenant); driver `local` ditolak di production; format ciphertext self-describing
  (rotasi jalan). Untuk tenant `key_custody=platform` di Tier 3: escrow/exit kunci tertulis (kontrak).
- Ditutup di PR-3.8.2: DEK per (tenant, purpose, kind) di `id.data_keys` (sentral, hanya blob
  ter-wrap); `KeyProvider` ber-registry (`RegisterProvider`) dengan driver `static`/`local`;
  blob DEK terikat KeyRef lewat AAD (baris kunci tak bisa dipindah antar tenant/purpose/kind);
  ciphertext self-describing + rotasi KEK V1→V2 & rotasi DEK lazy teruji; `local` ditolak di
  luar development oleh DUA gerbang (`config.Validate` + `NewFromConfig`); custody dibaca
  per-tenant dari registry, `key_custody='tenant'` ditolak lantang (`ErrCustodyUnsupported`).
- TERSISA: driver KMS produksi + mode custody `tenant` (PR-3.8.8); escrow/exit kunci Tier 3
  (kontrak, bukan kode); rotasi belum punya jalur operasi (CLI re-wrap) — masih manual SQL.
- **DITUTUP di PR-3.8.9 (ADR-016):** AAD kini mengikat `(tenant, purpose, key_version,
  record_id)`, sehingga ciphertext yang dipindah antar BARIS gagal dibuka. `PurposeOf`
  (ADR-015) tetap dibutuhkan untuk perpindahan antar KOLOM pada baris yang sama. Format
  ciphertext naik ke `0x02`; blob `0x01` ditolak `Decrypt` dengan pesan yang menyebut
  re-enkripsi. Sisa risiko yang diterima sadar: pertukaran `_bidx` SAJA antar baris
  (integritas indeks, bukan kebocoran) — lihat ADR-016 §Konsekuensi.
- **BATAS yang harus diperiksa saat PR-3.8.3 (jangan dianggap sudah ditutup):** AAD ciphertext
  field hanya mengikat TENANT. purpose & key_version dibaca dari blob itu sendiri (konsekuensi
  sadar format self-describing), sehingga ciphertext bisa dipindah antar KOLOM dalam satu
  tenant dan tetap terbuka. Penegakan kolom = tugas repository: bandingkan `crypto.PurposeOf(ct)`
  dengan purpose kolom sebelum `Decrypt`. Blob DEK (`id.data_keys`) TIDAK punya batas ini —
  KeyRef-nya diberikan dari luar, jadi baris kunci memang tak bisa dipindah antar
  tenant/purpose/kind.
- Catatan cache: entri DEK ter-decrypt yang kedaluwarsa belum dihapus dari map (`envelope.go`)
  — materi kunci bertahan di memori proses lebih lama dari TTL yang didokumentasikan. Bukan
  jalur eksploitasi, tapi memperlebar jendela paparan core dump; rapikan saat 3.8.3/3.8.8.

### H6. `CryptoPort` typed-nil lolos gerbang konstruktor — `OPEN` (dicatat PR-3.8.6)
- `newIdentityCrypto`/`requireCrypto` menolak interface nil, tapi `port.CryptoPort` yang memuat
  `(*crypto.Service)(nil)` **bukan** interface nil — ia lolos, lalu panic saat pemakaian pertama.
- Dinilai TIDAK mendesak: gagalnya berisik (panic saat request pertama), bukan diam-diam menulis
  pengenal plaintext — jaring pengaman yang sebenarnya diinginkan masih memegang. Yang hilang
  hanya lokasi kegagalan (runtime, bukan konstruksi).
- Tinjau saat identity diwiring ke composition root (`cmd/server`): di situlah typed-nil paling
  mungkin lahir dari helper yang mengembalikan `(*crypto.Service, error)` tanpa dicek.

---

## Penanda di kode (opsional)
Dokumen ini = **indeks tunggal** (hindari menaburkan marker yang menambah noise & butuh
perubahan `CODE_CONVENTION §9`). Bila kelak ingin grep-ability langsung di kode, tambahkan
penanda ringan `// SECREVIEW(<area>): <properti>` di titik presisi (mis. keyfunc JWT, quoteIdent
provision, fail-closed resolver) lalu daftarkan penanda `SECREVIEW` di CODE_CONVENTION. Sampai
itu diputuskan, rujuk dokumen ini.

## Catatan workflow review
Tool review (`/security-review`, `/code-review`) mem-banding **terhadap `origin/HEAD`**. Karena
konvensi repo `kerja di main + push`, jalankan review **sebelum push** (saat commit masih lokal)
ATAU di branch fitur untuk perubahan paling sensitif. Lihat keputusan workflow di catatan sesi /
`docs/adr/` bila kelak diformalkan.
