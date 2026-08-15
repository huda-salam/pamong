# core/workflow — Workflow Engine

Mengorkestrasi use case lintas waktu dan aktor. Definisi workflow disimpan sebagai DATA
di DB (bukan kode), bisa berbeda per-tenant, ber-versi. State machine + guard DSL + SLA.
Inti dari kemampuan "changeable workflow".

## Bergantung pada
- port/workflow.go, port/eventbus.go, port/auth.go
- core/scheduler (untuk SLA timer) — via port

## Tidak boleh
- Menjalankan business logic di dalam engine [linter: workflow-action-no-logic]
- Action mengakses DB langsung — action HANYA memanggil use case
- Menyimpan referensi ke orang konkret — workflow bicara PERAN

## Tanggung jawab
- State machine: state, transition, guard evaluation, action dispatch
- Definition store: simpan/baca definisi workflow di DB, versioned + audited
- YAML seed loader: load baseline dari file modul, simpan ke DB saat bootstrap
- Template selection: tenant memilih template ber-key + binding peran->jabatan (RoleBindings);
  nilai binding divalidasi ∈ gov.tenant_roles saat tulis lewat seam RoleChecker (PR-N2)
- Guard expression DSL: compile saat load, evaluate boolean, tanpa side-effect
- SLA & deadline: timer per-state, eskalasi otomatis (lewat scheduler); Escalation yang
  dipicu sudah membawa role KONKRET tenant bila instance dimulai via StartFromTemplate
- Notifikasi transisi: memicu seam TransitionNotifier (opsional, WithNotifier) setelah
  transisi sukses & SLA state baru terjadwal (PR-N2)
- History: riwayat transisi immutable per-instance

## BUKAN tanggung jawab
- Apa yang terjadi di dalam satu langkah (itu use case modul)
- Pemetaan peran->orang konkret (itu core/notification.RoleNotifier, fallback PLT)
- Pengiriman notifikasi (itu core/notification; workflow hanya memicu lewat TransitionNotifier/Escalator)

## File kunci
- engine.go — state machine runner, transition executor, Start & StartFromTemplate,
  ExecuteRequest (TransitionRequest: Entity untuk guard, Params untuk action — ADR-022)
- dispatch.go — ActionRegistry: sisi DAFTAR (modul saat Bootstrap) + sisi PANGGIL (engine),
  memenuhi domain.WorkflowRegistry & ActionDispatcher sekaligus (PR-W4a)
- definition.go — struct definisi, definition store (DB)
- loader.go — YAML seed loader + schema validation
- guard.go — DSL compiler & evaluator (boolean only)
- sla.go — deadline tracker, eskalasi (port Escalator, DeadlineScheduler, InstanceStateReader)
- notify.go — port TransitionNotifier (notifikasi transisi, PR-N2)
- history.go — transition history (immutable)
- instance.go — WorkflowInstance + port InstanceStore (persistensi; adapter di infra/workflow)
- template.go — template selection & parameter binding (RoleBindings, ApplyBindings)
- template_choice.go — jalur TULIS ber-tata-kelola pilihan template (permission, validasi
  template_id + RoleBindings lewat seam RoleChecker)

## Konvensi khusus
- **Action = port bertipe, bukan `any` (ADR-022).** Modul mendaftarkan port.WorkflowAction lewat
  app.Workflow().RegisterAction(nama, action); adapter tipis di modul memetakan Params →
  input use case bertipe. Nama ganda / action nil ditolak SAAT BOOT.
- **Params ≠ Entity.** Params adalah niat aktor pada request ini (hanya untuk action); Entity
  adalah snapshot keadaan tersimpan (hanya untuk guard). Menyatukannya = guard mengevaluasi nilai
  yang ditulis aktor sendiri.
- **Engine dirakit PER-TENANT** (ADR-022 Keputusan 3): DefinitionStore/TemplateStore tak membawa
  ctx/tenant, jadi satu store proses-lebar mustahil memilih DB tenant yang benar. Lihat
  cmd/server/workflow.go (workflowFactory) — isolasi struktural, bukan filter kolom. Tumpukannya
  DIRAKIT ULANG tiap permintaan (pool di-resolve ulang dari registry; tenant yang naik tier tak
  boleh terus ditulisi ke DB lama); yang di-cache hanya penyiapan DB (ensure schema + seed).
- Guard di-compile saat definisi di-load. Syntax error -> tolak di pintu masuk.
- **Guard ber-`entity.x` FAIL-CLOSED tanpa snapshot** (ADR-022 Keputusan 7): pembacaannya error,
  bukan bernilai nil. Penolakan ada di titik baca (entityField.eval), BUKAN di depan Program.Eval —
  evaluator ber-short-circuit, jadi menolak di depan akan ikut mematikan cabang yang tak pernah
  dibaca (`actor.has_role('x') || entity.status == 'aktif'` masih sah bagi pemegang role).
- Action di YAML = nama use case. Engine memanggilnya lewat dispatcher, tidak inline.
- Instance menyimpan versi definisi saat instance dimulai; perubahan definisi tidak
  mengubah instance berjalan.
- Perubahan definisi = aksi ber-permission + ter-audit.
- Instance dari template (StartFromTemplate) MEMBEKUKAN RoleBindings tenant saat Start ke
  `WorkflowInstance.RoleBindings`; ExecuteWithComment menerapkannya ulang (ApplyBindings) di
  SETIAP transisi — bukan dibaca ulang dari TemplateStore tiap kali (PR-N2, ADR-012).

## Pitfall umum
- Menaruh logika (hitung, validasi data) di guard atau action — dilarang. Guard hanya
  boolean read-only; action hanya panggil use case.
- Membuat guard yang butuh fungsi custom kompleks — sinyal logika harus pindah ke use case.
- Lupa versioning instance -> perubahan workflow merusak proses berjalan.
- **Pakai `Start` (bukan `StartFromTemplate`) untuk definisi yang dipilih lewat TemplateStore.**
  `Start` tidak pernah menerapkan RoleBindings — EscalateToRole/NotifySpec.ToRole tetap peran
  generik selamanya, notifikasi diam-diam tak sampai ke siapapun. Gunakan `StartFromTemplate`
  untuk instance yang lahir dari pilihan template tenant (lihat ADR-012).

## Test
- Unit: transisi valid/ilegal, guard evaluation (boolean), template selection, RoleBindings
  konsisten di Notify & SLA sepanjang instance (notify_test.go).
- Integration: dua tenant template berbeda + use case identik; SLA lewat -> eskalasi; instance
  ter-binding -> Notify transisi & eskalasi SLA sampai ke role konkret (infra/workflow).
- go test ./core/workflow/... -race

## Status wiring (PR-W4a)
TERPASANG di server hidup: registry action (nama di YAML → use case), engine per-tenant, seed
definisi baseline dari FS ter-embed modul ke tenant DB, persistensi instance, permukaan HTTP
`/workflow/instances*` (gateway/workflow).

BELUM (PR-W4b): `WithDeadlines` & `WithNotifier` tidak dipasang di engine produksi — state
ber-`sla_hours` belum menjadwalkan eskalasi dan `notify:` pada transisi belum mengirim apa pun.
Keduanya no-op yang memang sudah jadi kontrak engine (scheduler/notifier nil), bukan cacat baru.

## Rujukan
- PRD.md, port/workflow.go, CODING_PHILOSOPHY.md #5 (fleksibel di tepi)
- ADR-011 (seam SLA/eskalasi), ADR-012 (bridge notifikasi + konsumsi RoleBindings, PR-N2),
  ADR-022 (kontrak dispatch action + perakitan engine per-tenant, PR-W4a)
