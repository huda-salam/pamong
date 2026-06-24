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
- engine.go — entry evaluasi permission request
- rbac.go — role-based evaluation
- abac.go — attribute-based evaluation
- hierarchy.go — OPD tree, pewarisan
- delegation.go — PLT/pelaksana, validasi waktu
- registry.go — registrasi permission dari manifest, export/import
- enforcer.go — helper untuk gateway middleware & AuthContext

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
