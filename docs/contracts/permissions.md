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
