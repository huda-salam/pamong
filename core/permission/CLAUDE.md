# core/permission — Permission System

RBAC + ABAC hybrid dengan hierarki OPD dan delegasi/PLT. Dua lapis role: sentral
(global + scoped, di identity DB) dan tenant (di tenant DB). Lebih kompleks dari RBAC
biasa karena struktur jabatan pemerintahan + delegasi wewenang.

## Bergantung pada
- port/auth.go, port/user.go
- core/domain (untuk permission registration dari manifest)

## Tidak boleh
- Hardcode string role/permission di logika [linter: no hardcode]
- Mengizinkan modul cek permission modul lain tanpa import di manifest
  [linter: permission-must-be-registered]

## Tanggung jawab
- RBAC: role -> permission mapping, evaluasi
- ABAC: atribut (unit kerja, anggaran, periode) -> keputusan akses
- Hierarki OPD: tree jabatan struktural, pewarisan akses
- Delegasi/PLT: pelimpahan wewenang berwaktu, kedaluwarsa otomatis
- Permission export/import antar modul (manifest-based)
- Data-level permission: bukan hanya akses menu, tapi data mana (per unit/tahun)
- Prioritas konflik: global > scoped ~ tenant; union kecuali strict:true

## BUKAN tanggung jawab
- Autentikasi / issue token (itu identity)
- Penyimpanan data role (sentral di identity DB, tenant di tenant DB; komponen ini
  mengevaluasi, bukan menyimpan master)

## File kunci

Sudah ada (PR-2.3.1 — RBAC dasar; resolusi disempurnakan di 2.3.3):
- permission.go — tipe Permission, Layer (tenant/scoped/global), Role
- catalog.go — port RoleCatalog + MemoryCatalog (seam; impl DB di 2.3.2/2.3.3)
- engine.go — Engine.Allows + IsStrict
- (konsumen) port/permission.go PermissionEvaluator → dipakai gateway.Context.RequirePermission

Sudah ada (PR-2.3.3 — resolusi konflik PENUH + lapis tenant):
- engine.go — Engine.Allows kini menegakkan resolusi penuh (Opsi A, CLAUDE.md "Lapisan role"):
  GLOBAL menang tanpa syarat (termasuk atas strict); antar role non-global (scoped+tenant)
  perm biasa=union, perm strict=intersection (semua role non-global harus memberi). Layer
  dibaca dari catalog → kontrak Engine & port tetap utuh.
- composite.go — CompositeCatalog: gabung lapis central (snapshot proses) + tenant (snapshot
  per-tenant). **Sejak PR-W3a/ADR-019 resolusi dikurung PER LAPIS ASAL** (LookupRef atas
  port.RoleRef), bukan lagi "coba berurutan atas nama telanjang" — lihat di bawah.
- catalog DB tenant + resolver hidup di `tenantrole/adapter/db` (paket top-level baru, BUKAN
  identity — data di tenant DB schema gov, dikelola admin tenant): TenantRoleCatalog (snapshot,
  Lookup tanpa I/O) + TenantRoleResolver (EffectiveRoles per-user; isolasi per-tenant struktural
  karena terhubung ke tenant DB-nya sendiri). Tabel gov.* via EnsureSchema-on-write.

Sudah ada (PR-2.3.2 — central roles persist di identity DB):
- catalog DB central + resolver scope hidup di `identity/adapter/db` (impor core/permission,
  bukan sebaliknya): CentralRoleCatalog (snapshot definisi role → Lookup tanpa I/O, interface
  RoleCatalog tak berubah) + CentralRoleResolver (EffectiveRoles: global selalu, scoped via
  tenant_scope[], di luar masa berlaku diabaikan — lihat domain.CentralRoleAssignment.AppliesTo).
  Engine TETAP tenant-agnostic: scope di-resolve di luar Engine. Resolusi konflik penuh
  (global-precedence + strict-intersection) tetap menyusul 2.3.3 (saat lapis tenant juga di-DB-kan).

Sudah ada (PR-W3a — provenance role, ADR-019, menutup REVIEW_BACKLOG B8):
- port/permission.go — `RoleOrigin` + `RoleRef{Origin,Name}`. Masukan `PermissionEvaluator.Allows`
  kini ref, BUKAN nama telanjang: nama dari klaim `tenant_roles` dicari HANYA di katalog tenant,
  dari `central_roles` HANYA di katalog central. Nama yang sama di dua lapis = DUA role berbeda.
- catalog.go — `RefCatalog` (LookupRef) di samping `RoleCatalog` (Lookup satu-lapis, tetap
  kontrak tiap adapter katalog). `MemoryCatalog.LookupRef` mencocokkan origin terhadap Layer.
- composite.go — `NewCompositeCatalog(central, tenant)`; Layer hasil DIJEPIT ke lapis asal.
  Ia SENGAJA tak lagi mengimplementasi `RoleCatalog` dan tak lagi variadic — kompilasi yang
  mencegah jalur lookup nama telanjang dipanggil ulang tanpa terlihat.
- Kenapa: role TENANT bernama persis seperti role SENTRAL dulu me-resolve ke definisi sentral,
  mewarisi permission-nya SEKALIGUS LayerGlobal — sehingga B6 (reservasi namespace `identity:`)
  bisa dilewati tanpa menyebut `identity:` sama sekali.

Sudah ada (PR-2.3.5 — ABAC data-level + hierarki OPD + delegasi/PLT):
- scope.go — ResourceScope (unit kerja; tahun/periode = DEFERRED Phase-3.x), Grant (perm +
  jangkauan: TenantWide / unit / subtree), Authority (Roles []RoleRef untuk Tahap 1 RBAC +
  RoleGrants + DelegatedGrants untuk Tahap 2 scope), port Hierarchy (IsWithin subtree OPD).
- scoped_engine.go — ScopedEngine.AllowsInUnit = (Engine.Allows RBAC, UTUH, AND jangkauan
  RoleGrants menutupi unit) OR (jangkauan DelegatedGrants menutupi unit). Delegasi = jalur
  MANDIRI (tak tunduk strict-intersection: delegatee terima wewenang di luar role-nya).
  Bind(Authority) → port.ScopedEvaluator (actor-bound, disuntik gateway.Context di 2.4).
- Adapter tenant DB di `tenantrole/adapter/db`: TenantScopedGrantResolver (assignment+perm →
  Grant, hormati AppliesTo), OrgUnitHierarchy (gov.org_units adjacency + recursive CTE).
- Delegasi/PLT hidup di paket top-level `delegation/` (domain+usecase+adapter, mirror tenantrole):
  orang→orang subset permission, gov.delegations, expiry LAZY saat evaluasi (DoD), NonDelegable
  dicek saat buat. DelegationScopedGrantResolver → Authority.DelegatedGrants.
- Seam: port.ScopedEvaluator + AuthContext.RequirePermissionInUnit (default permisif sampai
  Authority di-wire live di 2.4).

Menyusul (belum ada — rencana per ROADMAP):
- registrasi permission dari manifest + export/import antar modul — SUDAH di 2.3.4 (lihat
  core/domain/registry.go + linter permission-must-be-registered)
- wiring Authority live + emitter central-role→Grant (TenantWide) di middleware auth (PR-W3b)
- ABAC atribut tahun anggaran/periode (Phase-3.x)

Catatan resolusi: prioritas "global menang" & strict-intersection (F7 PRD) AKTIF sejak
PR-2.3.3 (lihat engine.go). Sebelumnya (2.3.1/2.3.2) evaluasi murni union karena lapis
masih tunggal; kini Engine partisi role per Layer dan menerapkan global-precedence +
strict-intersection.

## Konvensi khusus
- Permission string format {modul}:{entity}:{aksi}. Selalu konstanta.
- Central role: scope_type global (semua tenant) atau scoped (tenant_scope[]).
- Konflik antar tenant role: union (lebih permisif menang) kecuali permission
  di-mark strict:true -> intersection.
- Delegasi punya valid_from/valid_until; kedaluwarsa = otomatis tidak berlaku.

## Containment jangkauan (PR-W3b · ADR-021)

`RequireAuthorityOver(ctx, perm, unit *uuid.UUID, includeSubtree bool)` adalah SATU-SATUNYA
implementasi aturan
"pemberi wewenang tak boleh memberi lebih luas dari yang ia pegang". Dipakai
`tenantrole.AssignTenantRole` & `delegation.CreateDelegation`.

- **`unit == nil` (= seluruh tenant) ditanyakan sebagai `AllowsInUnit(perm, uuid.Nil)`.** Itu bukan
  trik: se-tenant adalah jangkauan terluas, jadi ia menuntut wewenang terluas. Sahihnya bergantung
  pada invariant domain — `TenantRoleAssignment.Validate` & `Delegation.Validate` MENOLAK unit
  ber-UUID nol — sehingga hanya grant `TenantWide` yang bisa menutupinya. **Jangan melonggarkan
  invariant itu**; satu baris ber-unit nol menjawab "ya" pada pertanyaan se-tenant tanpa pernah
  memberi wewenang se-tenant kepada siapa pun.
- **`include_subtree` adalah PERTANYAAN LAIN, bukan varian.** Memberi subtree atas sebuah unit =
  memberi jangkauan atas seluruh keturunannya, jadi ia lewat `RequirePermissionInSubtree` →
  `AllowsSubtree`, yang hanya dijawab ya oleh `TenantWide` atau grant ber-`Subtree` atas unit
  itu/leluhurnya. Grant yang terikat persis pada satu unit menutupi unit itu tapi BUKAN
  keturunannya — memakai `AllowsInUnit` untuk pertanyaan ini adalah eskalasi lewat boolean.
- **Jangan menyalin aturan ini ke paket use case.** Dua salinan akan menyimpang saat salah satunya
  diperbaiki (alasan yang sama dengan `crypto.FieldSealer`).
- **`RequirePermission` tidak dipanggil terpisah sebelumnya** — Tahap 1 `AllowsInUnit` sudah RBAC
  utuh (strict-intersection + global-precedence). Menambahkannya hanya menduplikasi keputusan.
- **Aturan ini PER USE CASE.** Pemanggil baru (CLI, importer, workflow action) tidak otomatis
  terlindungi, dan konteks tanpa `ScopedEvaluator` membuatnya diam-diam permisif.
- **Ia menjawab DI MANA, bukan APA.** Containment jangkauan tak membatasi permission apa yang boleh
  dimasukkan ke dalam sebuah role — itu pagar terpisah di `tenantrole.CreateTenantRole`
  (`grantingPermissionPrefix`, ADR-021 Keputusan 6). Keduanya dibutuhkan: tanpa yang kedua, pemberi
  wewenang cukup MENCETAK permission baru lalu menugaskannya di dalam jangkauannya sendiri.

## Perakitan Authority (PR-W3b)

`BuildAuthority` = `Roles` (apa adanya dari klaim; strict-intersection butuh SEMUA ref) +
`RoleGrants` (grant sentral `TenantWide` dari `CentralGrants` ∪ grant assignment tenant) +
`DelegatedGrants` (terpisah — jalur mandiri).

- **Role sentral selalu `TenantWide`.** Scope teritorialnya sudah disaring saat login (ADR-019
  Keputusan 3); menerjemahkannya jadi unit-scoped mematikan wewenang lintas-unit.
- **Kegagalan resolver = error, bukan Authority bolong.** Authority bolong terasa seperti "tidak
  berwenang" bagi orang yang sebenarnya berwenang, dan menyembunyikan DB tenant yang tak terjangkau.
- **Konteks tanpa tenant → Authority KOSONG, bukan evaluator nil.** nil = permisif di
  `gateway.Context`.

## Pitfall umum
- **Menambah RoleCatalog baru tanpa jalur LookupRef yang menghormati origin.** Katalog yang
  mengabaikannya membuka ulang B8 di satu lapis saja, tanpa satu pun gejala — lihat
  `MemoryCatalog.LookupRef` sebagai acuan.
- **Meratakan role jadi `[]string` di jalur baru** (mis. "supaya gampang di-log/di-cache").
  Di situlah lapis asal hilang, dan sesudahnya katalog hanya bisa menebak (ADR-019).
- Mengasumsikan satu user = satu role. User bisa banyak role + central role bersamaan.
- Lupa cek scope: punya permission "baca SPM" tidak berarti bisa baca SPM semua unit.
- Delegasi tanpa batas waktu. Selalu ada masa berlaku.

## Test
- Unit: RBAC allow/deny, ABAC scope, hierarki pewarisan, delegasi aktif/kedaluwarsa,
  konflik union vs strict.
- go test ./core/permission/... -race

## Rujukan
- PRD.md, identity/PRD.md (model role), port/auth.go
