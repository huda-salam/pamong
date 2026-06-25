# ADR-003: Audit untuk mutasi identity (store sentral terpisah)

## Status
Accepted

## Konteks
PRD identity mewajibkan: *"Semua perubahan identitas/role/assignment ter-audit."* Namun
auto-attach audit (PR-1.3.3) hanya membungkus `SQLRepository` generik, sedangkan repo
identity ditulis tangan (entity `id.persons`/`id.employments`/`id.credentials` tidak
punya kolom `version`/`deleted_at`). Akibatnya CreatePerson/AttachEmployment tidak
menghasilkan jejak audit — divergensi nyata dari PRD.

Dua kendala spesifik identity:
1. Audit engine yang ada menulis ke `gov.audit_logs` di **tenant DB**. Operasi identity
   bersifat **sentral/lintas-tenant**; `tenant_id` sering kosong (admin platform membuat
   person sebelum ada tenant). Identity tidak bisa menumpang audit log tenant.
2. Repo identity bespoke (bukan `BaseRepository`), jadi dekorator auto-audit PR-1.3.3
   tidak langsung berlaku.

## Keputusan
Identity punya **audit store sentral terpisah di identity DB, schema `id`**
(`id.audit_logs`), bukan menumpang `gov.audit_logs`.

- **Reuse penuh engine audit.** `core/audit` sudah storage-agnostic lewat `Store` port;
  `ComputeHash`/`VerifyChain` dipakai apa adanya. `infra/db.AuditRepo` digeneralisasi
  agar table-nya bisa di schema mana pun (`NewSchemaAuditRepo(pool, "id")`) — perubahan
  non-perilaku untuk path gov.
- **Chain tunggal untuk identity.** Karena tak ada tenant, seluruh mutasi identity
  dirantai jadi satu chain. Kolom `tenant_id` dipakai sebagai partisi chain dengan nilai
  sentinel konstan `"central"`.
- **Tetap auto-attach (tanpa kode audit di use case).** Repo identity dibungkus dekorator
  audit di `identity/adapter/db` (`NewAuditedPersonRepo`, dst). Use case memanggil
  `repo.Save(ctx, ...)` seperti biasa; dekorator mencatat audit dari `AuthContext`. Use
  case tetap bersih dari kode audit — konsisten dengan filosofi PR-1.3.3.

## Konsekuensi
- Verifikasi integritas identity: telusuri chain partisi `"central"` di `id.audit_logs`
  dengan `VerifyChain` yang sama.
- `tenant_id` di `id.audit_logs` berfungsi ganda sebagai partisi chain (nilai `"central"`),
  bukan tenant sungguhan — didokumentasikan agar tidak membingungkan.
- Verifikasi via `pamongctl audit verify` saat ini menyasar DB tenant (`GOV_DB_*`);
  dukungan verifikasi identity DB (`GOV_IDENTITY_DB_*`) menyusul saat wiring identity DB
  pool dibangun (bersama provisioning/2.2.x). Tidak memblokir pencatatan audit itu sendiri.

## Alternatif yang dipertimbangkan
- **Menumpang `gov.audit_logs` (tenant DB).** Ditolak: operasi identity sentral, sering
  tanpa tenant; mencampur audit sentral ke salah satu tenant DB salah secara isolasi.
- **Mencatat audit di dalam use case identity (memanggil engine langsung).** Ditolak:
  melanggar prinsip "tidak ada kode audit di lapisan use case". Dekorator menjaga use
  case bersih.
- **Memberi `id.persons` dkk kolom version/deleted_at agar muat SQLRepository generik.**
  Ditolak: memaksakan model tenant ke entity identity yang bentuknya memang beda
  (anchor NIK, employment, credential) hanya demi reuse — distorsi model demi mekanisme.
