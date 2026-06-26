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
  per-tenant); Lookup mencoba berurutan, central didahulukan (cegah tenant shadow role global).
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

Menyusul (belum ada — rencana per ROADMAP):
- registrasi permission dari manifest + export/import antar modul (2.3.4)
- abac (atribut unit/anggaran/periode), hierarchy (tree OPD + pewarisan),
  delegation (PLT/pelaksana, validasi waktu) — semuanya 2.3.5
- enforcer/helper middleware bila diperlukan saat wiring auth nyata

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

## Pitfall umum
- Mengasumsikan satu user = satu role. User bisa banyak role + central role bersamaan.
- Lupa cek scope: punya permission "baca SPM" tidak berarti bisa baca SPM semua unit.
- Delegasi tanpa batas waktu. Selalu ada masa berlaku.

## Test
- Unit: RBAC allow/deny, ABAC scope, hierarki pewarisan, delegasi aktif/kedaluwarsa,
  konflik union vs strict.
- go test ./core/permission/... -race

## Rujukan
- PRD.md, identity/PRD.md (model role), port/auth.go
