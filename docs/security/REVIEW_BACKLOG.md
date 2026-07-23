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
  `tenant_id="central"` tak bisa dipalsukan untuk menyisipkan entri.

### E2. PII di audit (NIK mentah) — `DEFERRED(kripto)` — mekanisme diputuskan (ADR-009)
- `identity/adapter/db/audit_store.go`, ADR-002 (diperbarui ADR-009)
- Status: NIK tercatat **mentah** di `diff` (JSONB). Mekanisme kini diputuskan: field class
  `personal_id`/`specific` → diff **ikut terenkripsi** via `port.CryptoPort` (raw tetap ada
  sebagai bukti BPK, tak terbaca tanpa kunci + `audit:sensitive:baca`); class `personal`
  disaring saat baca. Prasyarat: sub-phase kripto (lihat §H). Cek saat dibangun: diff
  `personal_id` tak terbaca plaintext dari dump; hash chain tetap verify.

---

## F. Credential storage — Phase 2.1

### F1. Credential repo — `OPEN`
- `identity/adapter/db/credential_repository.go`, `identity/domain/entity.go` (`Credential`)
- Properti & cek: `secret_hash` = bcrypt (tak pernah plaintext); tak pernah di-SELECT/return ke
  jalur yang tak butuh (mis. resolver login hanya membandingkan hash, tak mengembalikannya);
  `UNIQUE(cred_type, cred_value)`; tak di-log.

---

## H. Enkripsi field & key management — sub-phase kripto (ADR-009/010)

### H1. Cakupan enkripsi selektif — `DEFERRED(kripto)`
- `core/domain/field_types.go` (`FieldDef.Class`), `infra/db` (DDL + repo), `infra/crypto`
- Cek: hanya `personal_id`/`specific` terenkripsi; `nama_lengkap` TIDAK; field terenkripsi
  tak masuk sortable/filterable (kecuali equality); `Unique` di kolom `_bidx`, bukan `_enc`.

### H2. Blind index & dictionary attack — `DEFERRED(kripto)`
- `infra/crypto`, ADR-009
- Cek: kunci bidx TERPISAH & di KMS (bukan DB); HMAC atas nilai ternormalisasi; ruang NIK
  16 digit kecil → kunci bidx bocor = brute-force layak, jadi kunci wajib di luar dump DB.

### H3. Jalur kebocoran samping — `DEFERRED(kripto)` (ADR-009 §6)
- audit diff (E2), payload event (NATS stream), `gov.idempotency_keys`, staging table
  migrasi, log/trace (OTEL, query log), clone `gov.user_profiles`
- Cek: tiap jalur tak membocorkan pengenal mentah. Enkripsi kolom saja = teater keamanan.

### H4. Key custody & rotasi — `DEFERRED(kripto)` (ADR-010)
- `infra/crypto/envelope.go`, KeyProvider driver, KMS
- Cek: DEK per-tenant per-purpose; DEK ter-wrap tak di tenant DB; `KeyProvider` di balik
  registry (KEK tak pernah keluar KMS); custody per-tenant (`key_custody`) diresolusi benar
  (platform vs tenant); driver `local` ditolak di production; format ciphertext self-describing
  (rotasi jalan). Untuk tenant `key_custody=platform` di Tier 3: escrow/exit kunci tertulis (kontrak).

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
