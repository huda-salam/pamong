# delegation/ — Delegasi / PLT (pelimpahan wewenang berwaktu)

Pelimpahan SUBSET permission dari satu user ke user lain dalam SATU tenant untuk rentang
waktu terbatas (PRD core/permission F5). Disimpan di **tenant DB** (schema `gov`). Lapis
ketiga kewenangan setelah role sentral (`identity/`) & role tenant (`tenantrole/`). Komponen
ini = master data + resolusi delegasi; EVALUASI permission ada di `core/permission.ScopedEngine`
(delegasi aktif → `Authority.DelegatedGrants`, jalur MANDIRI).

Paket top-level sendiri (bukan tenantrole): delegasi adalah orang→orang, bukan role. Pola kode
meniru `tenantrole/` (domain+usecase+adapter+audit).

Beda dari penugasan cross-tenant (PJ/PLT antar tenant = `id.tenant_assignments`, ranah
`identity/`/2.4.5): delegasi di sini INTRA-tenant.

## Bergantung pada
- core/permission (Grant), infra/db (Conn, audit), core/audit, port, core

## Struktur
- domain/ — Delegation (+ Validate, AppliesTo), NonDelegableSet (policy), ports, permissions, errors
- adapter/db/ — DelegationRepo (gov.delegations, ensure-on-write), DelegationScopedGrantResolver
  (delegasi aktif → permission.Grant), audited_repos (ADR-003 → gov.audit_logs), schema.go
- usecase/ — CreateDelegation (permission iam:delegasi:buat; tolak non-delegable; dedup)

## Konvensi khusus
- SELALU berbatas waktu: ValidUntil wajib, di masa depan dari ValidFrom (Validate menolak
  delegasi permanen). Kedaluwarsa = LAZY saat evaluasi (`ListActiveByDelegatee` filter di SQL:
  now ∈ [valid_from, valid_until)) — korektness tak bergantung job/cron (DoD PR-2.3.5b).
- NonDelegable: permission yang tak boleh didelegasikan (mis. TTD KPA) ditolak saat buat.
  Sumbernya himpunan yang di-inject (MVP manual); flag manifest per-permission = DEFERRED(Phase-2.4).
- Jangkauan data delegasi: UnitKerjaID nil → TenantWide; include_subtree → keturunan unit
  (hierarki OPD), sama model dengan assignment role tenant.
- Delegasi tak tunduk strict-intersection role: delegatee menerima wewenang di luar role-nya
  (justru itu inti PLT). Itu sebabnya DelegatedGrants jadi jalur terpisah di ScopedEngine.

## DEFERRED
- Phase-2.4: use case RevokeDelegation + publish event delegasi (refresh/revoke klaim token).
- Phase-2.4: sumber NonDelegable dari flag manifest per-permission.
- Phase-3.6+: job purge/notifikasi delegasi kedaluwarsa (hiasan; expiry sudah benar lazy).

## Test
- Unit: domain Validate (permanen/terbalik/diri-sendiri ditolak) & AppliesTo (kedaluwarsa);
  use case permission-denied, non-delegable ditolak, dedup.
- Integration (tag integration, PAMONG_TEST_DB_DSN): delegasi aktif → scoped-grant menembus
  ScopedEngine (delegatee tanpa role); delegasi kedaluwarsa → tak ter-resolve; audit. JANGAN
  DROP SCHEMA gov CASCADE — drop per-tabel (gov.audit_logs dipakai bersama lintas-paket).

## Rujukan
- core/permission/PRD.md (F5), core/permission/CLAUDE.md, tenantrole/CLAUDE.md

## Permukaan HTTP & containment (PR-W3b · ADR-021)

`/admin/iam/delegations` (`delegation/adapter/http`) dipasang pada ROUTER BISNIS lewat
`cmd/server.mountAdminIAMRoutes` — di balik RequireAuth, alasan sama dengan tenantrole.

- **`CreateDelegation` memakai `permission.RequireAuthorityOver`.** Delegasi adalah jalur MANDIRI
  di evaluator (tak tunduk strict-intersection role), jadi ia permukaan eskalasi paling langsung:
  pembuat delegasi se-tenant bisa melimpahkan wewenang ke akun mana pun tanpa menyentuh satu pun
  role. `unit_kerja_id` KOSONG = seluruh tenant, dan karenanya ikut diperiksa.
- **`Validate` menolak `unit_kerja_id` ber-UUID nol** — alasan identik dengan tenantrole.
- **RESIDU yang disengaja:** yang diperiksa jangkauan UNIT, bukan apakah pembuat MEMEGANG tiap
  permission yang ia limpahkan. `NonDelegableSet` menutup yang paling berbahaya; pemeriksaan
  per-permission menuntut evaluasi wewenang DELEGATOR (bukan pembuat) — lihat "Keputusan tertunda"
  ADR-021. Jangan menganggapnya sudah tertutup.

## Larangan minimum delegasi (PR-W3b · ADR-021 Keputusan 4b)

`domain.DefaultNonDelegable()` — dipakai composition root — melarang `identity:*` dan `iam:*` di
semua tenant. **Jangan menggantinya dengan `NewNonDelegableSet()` kosong** "karena tenant belum
punya kebijakan": himpunan kosong membuat delegasi menjadi jalur pemberian wewenang tanpa pagar apa
pun, sementara pembuatnya belum diwajibkan memegang sendiri permission yang ia limpahkan.

- `identity:*` sudah dipagari agar tak bisa masuk role tenant (`reservedPermissionPrefix`); tanpa
  pagar yang sama di sini, jalan pintasnya tinggal "delegasikan" alih-alih "berikan lewat role".
- `iam:*` = kemampuan MEMBERI wewenang. Melimpahkannya berarti melimpahkan kemampuan melimpahkan;
  sekali lolos, containment unit bisa dilebarkan sendiri oleh penerimanya secara berantai.
- Entri berbentuk **namespace** (`ns:*`), bukan daftar permission utuh, supaya permission baru di
  keluarga yang dilarang tak diam-diam menjadi boleh didelegasikan.
- Larangan spesifik-tenant ditumpuk: `DefaultNonDelegable("keuangan:spm:ttd_kpa")`.
