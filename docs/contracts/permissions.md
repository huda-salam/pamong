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
| `identity:credential:buat` | CreateCredential | Buat kredensial login (jalur tulis password satu-satunya) — PR-W2 |
| `identity:tenant:daftar` | RegisterTenant | Daftarkan tenant ke registry |
| `identity:tenant:baca` | ListTenants | Lihat tenant |
| `identity:tenant:nonaktif` | DeactivateTenant | Nonaktifkan tenant |
| `identity:assignment:tugaskan` | AssignEmploymentToTenant | Tugaskan employment ke tenant (home) |
| `identity:assignment:cross_tenant` | AssignEmploymentToTenant | Tambahan wajib bila penugasan cross-tenant (PJ/PLT) |
| `identity:central_role:buat` | CreateCentralRole | Buat role sentral (global/scoped) + grant — admin platform (PR-2.3.2) |
| `identity:central_role:assign` | AssignCentralRole | Tugaskan role sentral ke person — admin platform (PR-2.3.2) |
| `identity:authority:escalate` | (pintu keluar containment) | Melampaui wewenang sendiri saat memutasi identitas — ADR-019, PR-W3a |

Permukaan HTTP-nya (PR-W2) adalah grup `/admin/identity/*`, seluruhnya POST dan seluruhnya di
balik `RequireAuth` (berbeda dari `/auth/*` yang sengaja pra-otentikasi):

| Rute | Permission |
|---|---|
| `POST /admin/identity/persons` | `identity:person:buat` |
| `POST /admin/identity/employments` | `identity:employment:lampir` |
| `POST /admin/identity/credentials` | `identity:credential:buat` |
| `POST /admin/identity/assignments` | `identity:assignment:tugaskan` (+ `identity:assignment:cross_tenant` bila `cross_tenant: true`) |
| `POST /admin/identity/central-role-assignments` | `identity:central_role:assign` |

`identity:credential:buat` sengaja DIPISAH dari `identity:person:buat`: mencatat bahwa seseorang
ada berbeda jenis wewenangnya dari memberi orang itu cara masuk. Operator entri data boleh yang
pertama tanpa otomatis boleh yang kedua.

**Namespace `identity:` DIRESERVASI untuk lapis sentral.** `tenantrole/domain.TenantRole.Validate`
menolak role tenant yang memuat permission ber-prefiks `identity:`, dan penolakan itu ditegakkan
di pintu tulis repo — bukan hanya di use case. Alasannya menutup jalur eskalasi: `permission.Engine`
menggabungkan grant lintas lapis secara UNION, jadi tanpa pagar itu admin tenant dapat memberi
dirinya `identity:credential:buat` dan mengambil alih akun mana pun yang id person-nya ia ketahui
(REVIEW_BACKLOG B6).

Permission cross-tenant ditegakkan di **use case**, bukan di handler — sifat cross-tenant baru
diketahui setelah body di-parse, sementara aturan #3 mewajibkan gerbang pertama berdiri sebelum
parse. Handler memeriksa permission dasarnya saja.

**Punya permission ≠ boleh atas target ini (ADR-019).** Sejak PR-W3a ketiga mutasi identitas juga
menegakkan CONTAINMENT: tenant tujuan harus tenant token aktor, role sentral hanya boleh diberikan
bila aktor memegang seluruh permission-nya (role global selalu di luar wewenang), dan kredensial
hanya boleh diterbitkan untuk target yang wewenang sentralnya tak melampaui aktor. Satu-satunya
jalan melampauinya adalah `identity:authority:escalate` — permission TERSENDIRI, bukan pengecualian
tersembunyi, supaya "siapa yang boleh melampaui wewenangnya" bisa dijawab satu kueri audit. Aturan
ini diambil dari verb `escalate` Kubernetes RBAC. Penolakannya 403 menyebut permission pintu keluar,
bukan permission operasinya.

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

## customization (lintas-modul)

Tata-kelola kustomisasi tenant (custom field, label override, capability flag) yang dikelola
admin tenant, disimpan di tenant DB (schema `gov`). Konstanta di
`core/customization/permissions.go`; ditegakkan di `Manager` (jalur tulis).

| Permission | Use case | Keterangan |
|---|---|---|
| `customization:custom_field:buat` | Manager.CreateCustomField | Tambah custom field ke entity modul (PR-3.4.1) |
| `customization:custom_field:hapus` | Manager.DeactivateCustomField | Nonaktifkan custom field (soft) (PR-3.4.1) |
| `customization:label:ubah` | Manager.SetLabel | Override label field per-tenant (PR-3.4.1) |
| `customization:capability:ubah` | Manager.SetCapability | Aktif/nonaktifkan capability flag per-tenant (PR-3.4.1) |

Catatan: `tenant_id` selalu diambil dari AuthContext (token tersigning), bukan parameter —
aktor tak bisa menulis kustomisasi tenant lain. Custom field ber-class data (default aman
`internal`); enkripsi field ber-pengenal DEFERRED(Phase-3.8/ADR-009). Custom field yang bentrok
nama dengan field inti atau menargetkan entity tak terdaftar ditolak fail-closed.

## workflow

Tata-kelola pilihan template workflow tenant, disimpan di tenant DB (schema `gov`).
Konstanta di `core/workflow/permissions.go`; ditegakkan di `TemplateChoiceManager.SetChoice`.

| Permission | Use case | Keterangan |
|---|---|---|
| `workflow:template:pilih` | TemplateChoiceManager.SetChoice | Pilih/ubah template workflow tenant untuk satu slot (PR-3.3.2b) |

Catatan: `tenant_id` selalu diambil dari AuthContext (token tersigning), bukan parameter —
aktor tak bisa menulis pilihan template tenant lain. `template_id` divalidasi terdaftar DAN
milik slot yang dituju — format `{slot}.{varian}` dengan varian satu segmen (bukan sekadar
prefix string, agar slot bertingkat seperti "keuangan.spm" vs "keuangan.spm.lanjutan" tidak
saling lolos). Penyimpanan append-only ber-versi; pilihan lama tetap terbaca untuk audit/rollback.

## audit (lintas-modul)

Kontrol akses pada pembacaan jejak audit. Konstanta di `core/audit/permissions.go`;
ditegakkan di `core/audit.Reader` (bukan di tiap pemanggil).

| Permission | Use case | Keterangan |
|---|---|---|
| `audit:sensitive:baca` | Reader.ByEntity / Reader.ByTenant | Buka nilai diff ber-class `personal_id`/`specific` yang tersimpan terenkripsi (PR-3.8.4, ADR-009/ADR-002) |

Catatan: ketiadaan permission ini TIDAK menyembunyikan entry — pemeriksa tetap melihat siapa
mengubah apa dan kapan; hanya nilai pengenalnya (NIK, no rekening) yang tertutup dengan penanda
eksplisit. `tenant_id` selalu dari AuthContext, bukan parameter, sehingga jejak tenant lain tak
terjangkau. Nilai yang gagal didekripsi tampil sebagai penanda, tidak pernah sebagai blob mentah.
