# Kontrak Permission

Daftar permission yang dipakai framework & modul. Format `{modul}:{entity}:{aksi}`
(CODE_CONVENTION #8) — selalu dirujuk lewat konstanta, tidak pernah string literal di logika.

> **Sumber kebenaran definisi permission = manifest modul (kode, di Git).** Dokumen ini
> adalah katalog turunan untuk review/audit. Registrasi ke engine terjadi otomatis saat
> bootstrap dari modul yang terkompilasi (export/import antar modul: PR-2.3.4) — bukan
> import manual ke DB. Grant role→permission disimpan di DB (`id.central_role_permissions`
> untuk role sentral; analog `gov.*` untuk role tenant), berisi string permission ini.

## identity

Modul sentral. Konstanta di `identity/domain/permissions.go`.

| Permission | Use case | Keterangan |
|---|---|---|
| `identity:person:buat` | CreatePerson | Buat master person (anchor NIK) |
| `identity:employment:lampir` | AttachEmployment | Lampirkan employment (NIP untuk ASN) |
| `identity:tenant:daftar` | RegisterTenant | Daftarkan tenant ke registry |
| `identity:tenant:baca` | ListTenants | Lihat tenant |
| `identity:tenant:nonaktif` | DeactivateTenant | Nonaktifkan tenant |
| `identity:assignment:tugaskan` | AssignEmploymentToTenant | Tugaskan employment ke tenant (home) |
| `identity:assignment:cross_tenant` | AssignEmploymentToTenant | Tambahan wajib bila penugasan cross-tenant (PJ/PLT) |
| `identity:central_role:buat` | CreateCentralRole | Buat role sentral (global/scoped) + grant — admin platform (PR-2.3.2) |
| `identity:central_role:assign` | AssignCentralRole | Tugaskan role sentral ke person — admin platform (PR-2.3.2) |

Catatan: mutasi identity selalu ter-audit (ADR-003). Membuat & menugaskan role sentral
adalah pemberian wewenang lintas tenant — sensitif, butuh review ekstra.

## iam (lintas-modul)

Kapabilitas IAM (identity & access management) framework yang dikelola admin tenant,
disimpan di tenant DB (schema `gov`). Namespace `iam` menandai kapabilitas lintas-modul,
bukan satu modul bisnis.

| Permission | Use case | Keterangan |
|---|---|---|
| `iam:tenant_role:buat` | CreateTenantRole | Buat definisi role tenant + grant (PR-2.3.3) |
| `iam:tenant_role:assign` | AssignTenantRole | Tugaskan role tenant ke user; opsi scope unit kerja (PR-2.3.3/2.3.5) |
| `iam:delegasi:buat` | CreateDelegation | Limpahkan subset permission ke user lain, berbatas waktu (PR-2.3.5) |

Catatan: assignment role tenant bisa dibatasi ke unit kerja (ABAC data-level, PR-2.3.5):
`unit_kerja_id` + `include_subtree` menentukan jangkauan data, ditegakkan di
`core/permission.ScopedEngine`. Delegasi selalu berbatas waktu (kedaluwarsa otomatis) dan
tak boleh memuat permission yang ditandai non-delegable. Mutasi role/delegasi tenant
ter-audit ke `gov.audit_logs` (ADR-003).
