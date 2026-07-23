# PRD: Identity Module

## Tujuan
Mengelola identitas tunggal lintas seluruh platform: satu manusia = satu person (anchor
NIK), dengan kepegawaian (employment) opsional dan banyak credential. Mendukung persona
ganda (seorang ASN tetap warga: bisa akses portal publik) dan penugasan lintas instansi
(PLT/PJ) dengan otorisasi. Menjadi sumber kebenaran identitas yang di-clone ke tenant.

## Konteks & batasan
### Jadi tanggung jawab
- Master person/employment/credential di identity DB (terpisah)
- Central role (global + scoped) & assignment
- Tenant assignment (termasuk cross-tenant ber-otorisasi)
- Auth flow & JWT (issue/verify/revoke)
- Sync clone ke tenant via event
### BUKAN tanggung jawab
- Evaluasi permission (core/permission)
- Tenant role master (tenant DB) — identity hanya central role
- Data operasional modul

## Model data / tipe kunci
```
id.persons              (id, nik UNIQUE, nama, tgl_lahir, no_hp, email, is_active)
id.employments          (id, person_id, status[asn|non_asn], nip UNIQUE-nullable,
                         instansi_asal, is_active, valid_from/until)
id.credentials          (id, person_id, cred_type[nip|nik|email|no_hp|oauth],
                         cred_value, secret_hash, is_primary)
id.central_roles        (id, name, scope_type[global|scoped])
id.central_role_assignments (id, person_id, role_id, tenant_scope[], valid_from/until)
id.tenant_assignments   (id, employment_id, tenant_id, is_home_tenant, assigned_by,
                         valid_from/until)
id.tenant_registry      (tenant_id, tier, db_host, db_name, db_schema, migration_version,
                         key_custody[platform|tenant])
id.data_keys            (tenant_id, purpose, key_version, wrapped_dek, created_at)
                         -- DEK ter-wrap (envelope, ADR-009/010); sentral, BUKAN tenant DB

Tenant clone: gov.user_profiles (read-only, di-sync via event)
```

## Kebutuhan fungsional

### F1 — Model person/employment/credential
- Person anchor NIK (unik global). Employment opsional; ASN wajib NIP (unik), non-ASN
  tanpa NIP. Constraint: status=asn ⇒ nip not null; status=non_asn ⇒ nip null.
- Banyak credential per person; semua resolve ke person yang sama.
- Resolve by NIK / NIP / email / no_hp.
- **Enkripsi pengenal (ADR-009, diimplementasi pasca-Phase 3).** `nik`, `no_hp`, `email`
  (persons), `nip` (employments), `cred_value` (credentials) berkelas `personal_id` →
  disimpan terenkripsi + blind index. `UNIQUE` pindah ke kolom `_bidx`; semua lookup di F1
  adalah **equality** (`WHERE nik=$1` dst) sehingga tertangani blind index tanpa kehilangan
  fungsi. `nama_lengkap` **tidak** dienkripsi (class `personal`, harus dapat dicari).
  Clone `gov.user_profiles` (nik/nip) ikut terenkripsi. Enkripsi transparan di lapis
  repository (infra/db) — use case identity tak menyentuh kripto.

### F2 — Persona (konteks login)
- citizen: tersedia untuk SEMUA person (setiap orang adalah masyarakat). Tidak butuh
  employment / tenant assignment.
- employee: tersedia bila person punya employment aktif + tenant assignment.
- Persona ditentukan portal/jalur login, BUKAN tipe orang.
- ASN login portal publik → token persona=citizen, TANPA role internal.

### F3 — Central role
- Global (super_admin, platform_helpdesk): berlaku semua tenant.
- Scoped (regional_helpdesk): berlaku di tenant dalam tenant_scope[].
- Assignment ber-audit.

### F4 — Tenant assignment & cross-tenant
- Employment ditugaskan ke tenant (is_home_tenant=true untuk instansi induk).
- Cross-tenant (is_home_tenant=false, mis. PJ Bupati dari Pemprov): butuh permission
  identity:assignment:cross_tenant (admin sentral).
- Citizen tidak perlu tenant assignment.

### F5 — Auth flow (tiga jalur)
- Employee sentral: login (NIP) → token sementara (central_roles, scope) → pilih tenant
  (dari scope) → pilih app → scoped token.
- Employee daerah: login (NIP/NIK) → cek employment aktif (tolak bila tidak ada) → cek
  tenant assignment → 1 tenant langsung / >1 pilih → scoped token → app list.
- Citizen: portal publik, login NIK/email/no_hp → resolve person → verifikasi OTP/password
  (TANPA cek employment) → token persona=citizen (scope publik).

### F6 — JWT
- Claim: sub(person_id), persona, employment_status, tenant_id, central_roles,
  tenant_roles, tenant_scope, is_cross_tenant, iat, exp, jti.
- Tidak memuat password / NIK lengkap / data sensitif.
- Revocation via jti (mis. saat central role dicabut).

### F7 — Sync engine
- Subscribe event identity, clone person+employment ke gov.user_profiles tenant tujuan.
- Event: identity.person.dibuat/diperbarui, identity.employment.ditugaskan/dicabut,
  identity.central_role.diassign/dicabut.
- Clone read-only di tenant; modul bisnis tidak mengubah data inti user.

## Kebutuhan non-fungsional
- Identity DB selalu sentral. Tenant Tier 3 (dedicated server) tetap connect ke identity
  DB untuk auth (trade-off sadar; alternatif replikasi lebih kompleks).
- Verifikasi token: < 5ms. Login: < 200ms.
- Semua perubahan identitas/role/assignment ter-audit.

## Dependency
- core/domain (entity definition untuk person dkk)
- core/permission (central role master dikonsumsi saat evaluasi)
- core/eventbus (sync clone)
- port/user.go, port/auth.go

## Anti-pattern / yang harus dihindari
- user_type sebagai properti person. Yang benar: employment (opsional) + persona.
- citizen butuh tenant assignment (tidak).
- ASN login publik membawa role internal (harus tanpa).
- Modul bisnis query identity DB / gov.user_profiles langsung (lewat UserResolver port).
- Cross-tenant assignment tanpa permission khusus.

## Keputusan tertunda
- Replikasi identity DB ke server tenant Tier 3 (untuk ketahanan saat koneksi putus) —
  ditunda; sementara identity sentral, koneksi wajib.
- Integrasi SSO/SPBE & SIASN sebagai sumber credential/employment — adapter terpisah,
  Phase lanjut.
- **Enkripsi pengenal (ADR-009) + migrasi UNIQUE→blind index** — diimplementasi pasca-Phase 3,
  **sebelum tenant produksi pertama**. Gratis selama belum ada data; setelahnya butuh
  dual-write + backfill. Custody kunci Tier 3 menunggu ADR-010.

## Acceptance criteria
- [ ] Person dibuat + employment ASN + credential → resolve by NIK & NIP benar.
- [ ] Constraint status=asn⇒NIP wajib, non_asn⇒NIP null ditegakkan.
- [ ] ASN login portal publik → token persona=citizen tanpa role internal.
- [ ] Employee daerah tanpa employment aktif → ditolak masuk internal.
- [ ] >1 tenant assignment → user memilih tenant; 1 tenant → langsung.
- [ ] Central role global → akses semua tenant; scoped → hanya scope.
- [ ] Cross-tenant assignment butuh permission khusus.
- [ ] Token revocation via jti bekerja.
- [ ] Event identity.employment.ditugaskan → clone gov.user_profiles di tenant tujuan.
- [ ] Modul bisnis tidak bisa import identity/ (linter).
