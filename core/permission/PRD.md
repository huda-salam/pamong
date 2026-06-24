# PRD: Permission System

## Tujuan
Menegakkan otorisasi berlapis khas pemerintahan: kombinasi role berbasis jabatan
(RBAC), atribut unit kerja & anggaran (ABAC), hierarki OPD, dan delegasi wewenang (PLT).
Permission tidak hanya "boleh akses menu" tapi "boleh akses data mana" (data-level).

## Konteks & batasan

### Jadi tanggung jawab
- Evaluasi keputusan akses: given (actor, permission, resource) -> allow/deny
- Model & evaluasi RBAC, ABAC, hierarki OPD, delegasi
- Registrasi permission dari manifest + mekanisme export/import antar modul
- Resolusi prioritas saat konflik role

### BUKAN tanggung jawab
- Autentikasi & penerbitan token (identity)
- Master data role/assignment (identity DB untuk sentral, tenant DB untuk tenant;
  komponen ini membaca & mengevaluasi)
- Resolusi peran workflow -> orang (berkolaborasi dengan kepegawaian)

## Model data / tipe kunci

```go
// Permission string: {modul}:{entity}:{aksi}, mis. "penatausahaan:spm:sahkan"

type PermissionManifest struct {
    Groups  []PermissionGroup       // pengelompokan untuk assignment ke role
    Exports []string                // permission yang boleh dirujuk modul lain
    Imports []PermissionImport      // permission modul lain yang dipakai
}

type CentralRole struct {
    Name      string                // "super_admin", "regional_helpdesk"
    ScopeType string                // "global" | "scoped"
}
type CentralRoleAssignment struct {
    PersonID    uuid.UUID
    RoleID      uuid.UUID
    TenantScope []string            // untuk scoped; nil = global
    ValidFrom, ValidUntil time.Time
}

type TenantRole struct {
    Name string                     // "bendahara_pengeluaran", "ppk_opd"
}
type TenantRoleAssignment struct {
    PersonID    uuid.UUID
    RoleID      uuid.UUID
    UnitKerjaID *uuid.UUID          // scope ke unit; nil = seluruh tenant
    ValidFrom, ValidUntil time.Time
}

type Delegation struct {
    FromPersonID uuid.UUID          // pejabat asli
    ToPersonID   uuid.UUID          // PLT/pelaksana
    Permissions  []string           // subset yang didelegasikan
    ValidFrom, ValidUntil time.Time
    NonDelegable []string           // permission yang tak bisa didelegasikan (mis. TTD)
}
```

## Kebutuhan fungsional

### F1 — RBAC
- Evaluasi: actor punya permission P jika salah satu role-nya (tenant atau central)
  memuat P.
- Permission dikelompokkan dalam Group untuk kemudahan assignment.

### F2 — ABAC (data-level)
- Selain "punya permission", cek scope: unit kerja, tahun anggaran, periode.
- Contoh: "baca SPM" + unit_kerja_id = BPKAD → hanya boleh baca SPM milik BPKAD.
- Atribut resource dibandingkan dengan atribut actor (unit kerja, tahun aktif).

### F3 — Hierarki OPD
- Tree jabatan: Sekda > Kepala Dinas > Kabid > Kasubag.
- Pewarisan: atasan dapat melihat data unit di bawahnya (configurable per permission).
- Edge: matriks/lintas-unit (mis. inspektorat akses semua) ditangani via central scoped
  role atau permission khusus, bukan pewarisan hierarki.

### F4 — Central roles global & scoped
- Global (super_admin, platform_helpdesk): berlaku di semua tenant, menang atas semua.
- Scoped (regional_helpdesk): berlaku hanya di tenant dalam tenant_scope[].
- Scoped role setara tenant role di tenant yang di-scope.

### F5 — Delegasi / PLT
- Pejabat melimpahkan subset permission ke pelaksana untuk rentang waktu.
- Kedaluwarsa otomatis: setelah valid_until, delegasi tidak berlaku tanpa aksi manual.
- NonDelegable: beberapa permission tidak bisa didelegasikan (mis. TTD KPA tertentu).
- Delegasi tercatat di audit.

### F6 — Export/import permission antar modul
- Manifest `Exports`: permission yang boleh dirujuk modul lain (mis. dalam guard workflow).
- Manifest `Imports`: permission modul lain yang dipakai modul ini.
- Modul memakai permission modul lain tanpa mendaftar di Imports → linter tolak.
- Registrasi saat bootstrap: permission imported terdaftar ke engine.

### F7 — Resolusi konflik
- Prioritas: central global > (central scoped ~ tenant role).
- Antar tenant role: union (lebih permisif) — KECUALI permission di-mark strict:true,
  yang menggunakan intersection (semua role harus mengizinkan).

## Kebutuhan non-fungsional
- Evaluasi permission: < 5ms (dengan caching role/scope per-request).
- Cache permission di-invalidate saat role/delegasi berubah (via event).
- Audit setiap perubahan assignment & delegasi.

## Dependency
- port/auth.go — AuthContext yang memanggil RequirePermission
- port/user.go — resolusi data user/jabatan
- core/domain — registrasi permission dari manifest
- Event bus — invalidasi cache saat role berubah

## Anti-pattern / yang harus dihindari
- Hardcode string role/permission dalam logika.
- Mengasumsikan 1 user = 1 role. User punya banyak role + central bersamaan.
- Permission tanpa scope check (data-level diabaikan) → kebocoran data antar unit.
- Delegasi permanen tanpa batas waktu.
- Pewarisan hierarki untuk kasus yang seharusnya central scoped role.

## Keputusan tertunda
- Granularitas pewarisan hierarki per-permission (configurable) vs global — sementara
  configurable per permission via flag inherit.
- Caching strategy lintas-request (per-request aman; lintas-request butuh invalidasi
  hati-hati) — mulai per-request, optimasi nanti.

## Acceptance criteria
- [ ] RBAC: actor dengan role yang memuat P → allow; tanpa → deny.
- [ ] ABAC: "baca SPM" + scope unit → hanya data unit itu yang boleh.
- [ ] Hierarki: atasan melihat data unit bawahan (untuk permission inherit).
- [ ] Central global role → akses semua tenant.
- [ ] Central scoped role → hanya tenant dalam scope.
- [ ] Delegasi aktif → delegatee punya permission; setelah valid_until → tidak.
- [ ] NonDelegable permission tidak ikut terdelegasi.
- [ ] Import permission modul lain tanpa daftar Imports → linter tolak.
- [ ] Konflik tenant role: union; strict:true → intersection.
- [ ] Perubahan assignment → cache ter-invalidate via event.
