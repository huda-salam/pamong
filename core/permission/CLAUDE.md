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

Sudah ada (PR-2.3.1 — RBAC dasar, evaluasi union in-memory):
- permission.go — tipe Permission, Layer (tenant/scoped/global), Role
- catalog.go — port RoleCatalog + MemoryCatalog (seam; impl DB menyusul 2.3.2/2.3.3)
- engine.go — Engine.Allows (union antar role), IsStrict (flag; intersection menyusul)
- (konsumen) port/permission.go PermissionEvaluator → dipakai gateway.Context.RequirePermission

Menyusul (belum ada — rencana per ROADMAP):
- catalog DB central (2.3.2: id.central_roles, scoped tenant_scope[]) & tenant (2.3.3: gov.tenant_roles)
- registrasi permission dari manifest + export/import antar modul (2.3.4)
- abac (atribut unit/anggaran/periode), hierarchy (tree OPD + pewarisan),
  delegation (PLT/pelaksana, validasi waktu) — semuanya 2.3.5
- enforcer/helper middleware bila diperlukan saat wiring auth nyata

Catatan resolusi: prioritas "global menang" & strict-intersection (F7 PRD) baru
berdampak saat lapis central + tenant hidup berdampingan (2.3.2/2.3.3); di 2.3.1
evaluasi murni union.

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
