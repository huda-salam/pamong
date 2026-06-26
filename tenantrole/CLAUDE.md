# tenantrole/ — Role Tenant (Lapisan 2)

Role yang dikelola admin tenant, disimpan di **tenant DB** (schema `gov`), berlaku HANYA
di dalam tenant-nya (CLAUDE.md root "Lapisan 2"). Pelengkap role sentral (`identity/`,
lapis central di identity DB). Komponen ini = master data + resolusi role tenant; EVALUASI
permission ada di `core/permission.Engine`.

Bukan bagian `identity/` (sengaja): datanya di tenant DB & dikelola admin tenant, bukan
data identitas sentral. Pola kode meniru `identity/adapter/db` central role, beda: tenant DB
per-tenant, tanpa scope_type (role tenant selalu satu tenant).

## Bergantung pada
- core/permission (RoleCatalog, Layer), infra/db (Pool/Conn, audit), core/audit, port, core

## Struktur
- domain/ — TenantRole, TenantRoleAssignment (+ Validate, AppliesTo), ports, permissions, errors
- adapter/db/ — repo (Save role+grant atomik), TenantRoleCatalog (snapshot), TenantRoleResolver
  (EffectiveRoles per-user), audited_repos (ADR-003 → gov.audit_logs), schema.go (EnsureSchema)
- usecase/ — CreateTenantRole, AssignTenantRole (permission iam:tenant_role:buat/assign)

## Konvensi khusus
- Isolasi "hanya berlaku di tenant-nya" bersifat STRUKTURAL: resolver hanya melihat gov.*
  milik tenant DB yang dikoneksikan — tak ada parameter tenantID.
- Tabel gov.* via EnsureSchema-on-write (precedent gov.user_profiles); runner migrasi
  framework-gov formal + FK ke gov.user_profiles = DEFERRED (lihat ROADMAP backlog).
- scope unit_kerja: kolom disimpan & di-round-trip, BELUM ditegakkan (DEFERRED Phase-2.3.5).
- Resolusi konflik (global menang, strict=intersection) ada di Engine, bukan di sini.

## Test
- Unit: domain Validate/AppliesTo; use case permission-denied & dedup (fake lokal).
- Integration (tag integration, PAMONG_TEST_DB_DSN): end-to-end persist→catalog→resolver→
  Engine (global menang, strict intersection) + audit. JANGAN DROP SCHEMA gov CASCADE —
  drop per-tabel (gov.audit_logs dipakai bersama lintas-paket).

## Rujukan
- CLAUDE.md root ("Identity & manajemen user" → Lapisan 2), core/permission/CLAUDE.md
