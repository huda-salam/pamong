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

### A4. Auth middleware + populasi context — `DEFERRED(PR-2.4.2)`
- (belum ada) `gateway/middleware/auth.go`, `gateway/context.go`
- Review saat hadir: ekstraksi bearer token aman; **header `X-Tenant-ID` tak boleh meng-override
  tenant dari klaim token** (vektor eskalasi lintas-tenant — lihat C1); pembangunan
  Engine/ScopedEngine per-request benar (tak bocor antar request/tenant).

### A5. Alur login employee/citizen + cross-tenant — `DEFERRED(PR-2.4.3/2.4.4/2.4.5)`
- (belum ada) `identity/usecase/login_*`, OTP, pemilihan tenant
- Review saat hadir: ASN login publik **tanpa** role internal (kebocoran wewenang); OTP rate-limit
  & brute-force; cross-tenant assignment wajib `identity:assignment:cross_tenant`.

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

### C1. Tenant resolver (PR-2.2.2) — `OPEN` (prioritas tinggi)
- `gateway/middleware/tenant_resolver.go`
- Properti & cek: tenant dari klaim token diutamakan; **header `X-Tenant-ID` tak boleh
  dipakai untuk pindah ke tenant lain saat token sudah menentukan tenant** (vektor eskalasi).
  Tenant tak dikenal→404, nonaktif→403. Tiap request hanya pernah membawa tenant-nya sendiri.

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

### E2. PII di audit (NIK mentah) — `DEFERRED(masking)` 
- `identity/adapter/db/audit_store.go`, ADR-002
- Status: NIK tercatat **mentah** di audit (masking ADR-002 ditunda). Saat masking dibangun,
  review: field sensitif (NIK, no HP) ter-mask di trail; diff audit tak membocorkan PII penuh.

---

## F. Credential storage — Phase 2.1

### F1. Credential repo — `OPEN`
- `identity/adapter/db/credential_repository.go`, `identity/domain/entity.go` (`Credential`)
- Properti & cek: `secret_hash` = bcrypt (tak pernah plaintext); tak pernah di-SELECT/return ke
  jalur yang tak butuh (mis. resolver login hanya membandingkan hash, tak mengembalikannya);
  `UNIQUE(cred_type, cred_value)`; tak di-log.

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
