# ADR-012: Bridge workflow→notification — konsumsi role binding & port TransitionNotifier

## Status
Accepted

## Konteks
ADR-011 (PR-3.2.6) membangun seam SLA/eskalasi (`DeadlineScheduler`, `InstanceStateReader`,
`Escalator`) tapi secara eksplisit MENUNDA dua hal ke backlog "[PR-3.6.x] Konsumsi role
binding":
- `ApplyBindings` (PR-3.2.4) sudah bisa mengganti peran generik (`"validator_sla"`) menjadi
  role konkret tenant (`"kepala_dinas"`), tapi tak ada satu pun jalur nyata yang memanggilnya
  sebelum `Escalation`/`NotifySpec` dikonsumsi — `Engine.Start` mengambil definisi mentah lewat
  `DefinitionStore.Get`, bukan lewat `TemplateStore.GetForTenant` yang sudah ter-binding.
- `NotifySpec.ToRole` pada transisi (bukan cuma eskalasi SLA) belum pernah dipicu sama sekali —
  `Engine.ExecuteWithComment` tidak menyentuh `tr.Notify`.
- Backlog keamanan terkait: `RoleBindings` (map string bebas) tak divalidasi saat tulis. Begitu
  notifikasi hidup, nilai binding menentukan SIAPA menerima dokumen — binding ke role salah
  ketik / role tenant lain harus ditolak SAAT TULIS, bukan baru terlihat saat kirim gagal.

PR-N1 (`infra/notification.DBRecipientDirectory`) sudah membuat `RecipientDirectory` nyata.
PR-N2 menutup KEDUA item di atas: workflow yang sudah lengkap (state machine + SLA + Notify)
kini benar-benar memicu notifikasi lewat stack PR-N1, dengan role konkret tenant, bukan peran
generik.

## Keputusan

**1. Port baru `TransitionNotifier` di `core/workflow` (notify.go), sejajar `Escalator`.**
Sama seperti `Escalator`, ini driven port opsional (nil = no-op, pola sama `WithDeadlines`):
`WithNotifier(n TransitionNotifier) Option`. Dipanggil `ExecuteWithComment` SETELAH transisi
otoritatif & SLA state baru terjadwal, hanya bila `tr.Notify != nil`. Diimplementasikan di
`infra/workflow.NotifierTransition` — mirror `NotifierEscalator` (ADR-011), keduanya kini
berbagi helper `sendRoleNotify` untuk memetakan (tenantID, role) → `notification.RoleTarget`
lalu memanggil `RoleNotifier.NotifyRole`.

**2. RoleBindings di-resolve & DIBEKUKAN di Engine saat Start, BUKAN di adapter Escalator/
Notifier, dan BUKAN dibaca ulang tiap transisi.**
ADR-011 sempat berspekulasi titik penyisipan ada "di `NotifierEscalator`, via
`TemplateStore.GetForTenant`". PR-N2 memutuskan sebaliknya:
- `Engine.StartFromTemplate(ctx, slot, entityID)` (entry point baru, terpisah dari `Start`)
  memanggil `TemplateStore.GetTenantConfig(tenant, slot)` SEKALI, menerapkan `ApplyBindings`,
  lalu MEMBEKUKAN `cfg.RoleBindings` ke `WorkflowInstance.RoleBindings` (field baru).
- `ExecuteWithComment` menerapkan ULANG `ApplyBindings(def, instance.RoleBindings)` pada
  SETIAP transisi (bukan hanya initial_state) sebelum membaca state/transition — sehingga
  `Escalation.EscalateToRole` (via `scheduleSLA`) dan `NotifySpec.ToRole` konsisten role
  KONKRET sepanjang hidup instance.
- `Start` (defID langsung, tanpa TemplateStore) TIDAK BERUBAH — tetap unbound, untuk definisi
  yang sudah berisi role konkret di luar mekanisme slot template tenant. Didokumentasikan
  eksplisit di `Start` bahwa ia TIDAK BOLEH dipakai untuk definisi asal-template.

**3. RoleBindings pinned di Start, bukan live-read, demi konsistensi dengan DefinitionVersion.**
Alternatif "baca ulang `TemplateStore.GetForTenant` tiap transisi" ditolak — lihat Alternatif.

**4. `RoleChecker` — validasi RoleBindings saat TULIS (menutup prasyarat keamanan ADR-011/backlog).**
Port baru `RoleChecker.RoleExists(ctx, tenantID, roleName) (bool, error)` disuntik ke
`TemplateChoiceManager` (parameter baru di `NewTemplateChoiceManager`, WAJIB non-nil bila
`RoleBindings` dipakai — `SetChoice` menolak eksplisit, bukan panic, bila `RoleChecker` tak
terpasang tapi `RoleBindings` non-kosong). `SetChoice` menolak nilai binding yang bukan role
terdaftar di `gov.tenant_roles` milik tenant, SEBELUM menyimpan versi baru. Diimplementasikan
`infra/workflow.TenantRoleChecker` atas `tenantrole/adapter/db.TenantRoleRepo`.

**5. Kegagalan `TransitionNotifier` best-effort, tapi TETAP dipropagasi sebagai error.**
Transisi domain sudah otoritatif di titik pemanggilan (state sudah berubah, history sudah
tercatat) — kegagalan notifikasi TIDAK membatalkannya. Tapi errornya tidak ditelan: dikembalikan
ke caller agar jejak kegagalan terlihat dan caller asinkron (outbox/relay, masih backlog
terpisah) bisa memutuskan retry — konsisten pola `Hub.Send` (error transport tanpa kehilangan
jejak).

## Konsekuensi
- `WorkflowInstance` bertambah field `RoleBindings map[string]string` (nil untuk instance dari
  `Start` biasa).
- Dua entry point Start (`Start` unbound vs `StartFromTemplate` bound) hidup berdampingan.
  Ini RISIKO YANG DIKETAHUI: tak ada penegakan tipe yang mencegah caller salah pilih `Start`
  untuk definisi asal-template. Dimitigasi lewat dokumentasi eksplisit di `Start` (bukan
  penegakan struktural) karena belum ada pemanggil produksi Engine sama sekali saat ADR ini
  ditulis — revisit bila risiko nyata muncul saat wiring modul bisnis pertama.
- `ErrEngineTemplatesNotWired` (error baru) untuk `StartFromTemplate` dipanggil tanpa
  `WithTemplates` — beda kelas dari `ErrTemplateNotConfigured` (tenant belum memilih template
  untuk slot itu).
- `NewTemplateChoiceManager` menambah parameter wajib `RoleChecker` (breaking, tapi belum ada
  pemanggil produksi).

## Alternatif yang dipertimbangkan
- **Terapkan binding di adapter (`NotifierEscalator`/`NotifierTransition`), seperti diduga
  ADR-011.** Ditolak: `Escalation` di-serialize ke payload job scheduler (JSON) — tak ada
  tempat alami menyisipkan `slot` untuk memanggil `TemplateStore.GetForTenant` di titik itu
  tanpa memperbesar payload & meng-couple adapter ke pemilihan template. Menerapkan di Engine
  (yang sudah punya akses `TemplateStore` + instance) jauh lebih sederhana.
- **Baca ulang `TemplateStore.GetForTenant` tiap Execute (live), bukan dibekukan.** Ditolak:
  tenant merekonfigurasi `RoleBindings` di tengah instance berjalan akan mengubah rute
  notifikasi instance yang SEDANG jalan secara retroaktif — tidak konsisten dengan
  `DefinitionVersion` yang sengaja dikunci saat Start (PRD F1/F7). Freeze-at-Start menyamakan
  kedua invariant.
- **Satu `Start` untuk semua kasus (deteksi otomatis butuh-binding-atau-tidak).** Dipertimbangkan
  tapi ditunda — butuh abstraksi `DefinitionSource` baru yang menyatukan `DefinitionStore` &
  `TemplateStore`; tak ada pemanggil produksi saat ini yang memaksa keputusan ini, revisit saat
  Engine benar-benar di-wire ke modul bisnis pertama.
- **`RoleChecker` opsional (nil = lewati validasi).** Ditolak: mengulang celah keamanan yang
  PR-N2 justru dimaksudkan menutup (RoleBindings tak tervalidasi). `RoleChecker` boleh nil HANYA
  bila `RoleBindings` kosong; sebaliknya `SetChoice` menolak eksplisit.
