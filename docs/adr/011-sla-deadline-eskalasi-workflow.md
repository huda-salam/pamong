# ADR-011: SLA deadline & eskalasi workflow via port (guard race backstop)

## Status
Accepted

## Konteks
PR-3.2.6 menambahkan SLA & eskalasi ke workflow engine (PRD workflow F6): state dengan
`sla_hours > 0` mendaftarkan deadline saat instance MASUK, membatalkannya saat KELUAR, dan
saat deadline lewat memicu notifikasi ke `escalate_to_role`.

Eskalasi menyilangkan tiga komponen core yang tak boleh saling di-couple konkret:
- **core/workflow** — engine state machine (menjadwalkan/membatalkan saat transisi).
- **core/scheduler** — timer deadline (job one-shot, PRD scheduler F2).
- **core/notification** — pengiriman eskalasi (routing peran→orang + fallback PLT, PR-3.6.2).

Batasan arsitektur (CLAUDE.md §Arsitektur, §Aturan pengembangan #7, #9):
- Domain core (`core/workflow`) tak boleh mengimport core lain secara konkret — hanya lewat port.
- Engine tetap **tenant-agnostik**: bicara PERAN, bukan orang; resolusi peran→orang di luar.
- Eskalasi adalah **notifikasi**, bukan business logic — tak ada mutasi data / pemanggilan use case.
- Timer deadline dan jam dinding tak deterministik → ada **balapan** transisi-vs-deadline yang
  harus ditangani, karena membatalkan timer eksternal tak pernah bisa 100% atomik dengan transisi.

## Keputusan

**1. Tiga port baru di `core/workflow` (bukan import konkret).**
- `DeadlineScheduler` (driven) — `ScheduleDeadline`/`CancelDeadline`. Engine memanggilnya saat
  masuk/keluar state ber-SLA. Diimplementasi di luar (`infra/workflow.SchedulerDeadlines` atas
  `core/scheduler`, memakai job one-shot). `CancelDeadline` WAJIB idempoten.
- `InstanceStateReader` (driven) — `CurrentState(instanceID)`. Dipakai HANYA untuk guard race
  di fire-time (baca state terkini). Diimplementasi atas penyimpanan instance.
- `Escalator` (driven) — `Escalate(Escalation)`. Diimplementasi di luar
  (`infra/workflow.NotifierEscalator` atas `core/notification.RoleNotifier`); di sanalah
  resolusi peran→orang + PLT + render + kirim terjadi.

**2. `EscalationCoordinator` di core = kebijakan fire-time (guard race DULU, baru eskalasi).**
Saat deadline lewat: baca state terkini instance; bila sudah PINDAH dari state ber-deadline →
**no-op** (deadline basi). Bila masih → picu Escalator. Kebijakan ini hidup di core (teruji,
deterministik); sumber datanya pluggable. Coordinator dibungkus `scheduler.JobFunc`
(`EscalationJob`) yang didaftarkan ke `scheduler.Registry` saat bootstrap.

**3. Guard race adalah backstop kebenaran, bukan sekadar optimasi cancel.**
Engine membatalkan timer saat keluar state (best-effort). Bila pembatalan luput (mis. scheduler
sempat error, atau deadline sudah masuk antrian eksekusi), guard race di coordinator tetap
meng-no-op-kan eskalasi karena instance sudah pindah. Karena itu penjadwalan/pembatalan SLA
dilakukan SETELAH transisi tercatat: transisi domain sudah otoritatif; kegagalan scheduler
dipropagasi sebagai error (agar wiring rusak terlihat) tanpa membatalkan transisi.

**4. Engine tetap tenant-agnostik; deadline membawa PERAN generik dari definisi.**
`Deadline`/`Escalation` membawa `EscalateToRole` apa adanya dari `State.EscalateToRole` +
`TenantID` (dari AuthContext saat penjadwalan). Engine tak meresolusi peran→role konkret
maupun peran→orang.

**5. Opsi fungsional, bukan perubahan signature.**
Engine memperoleh SLA lewat `workflow.New(..., WithDeadlines(sched))`. Tanpa opsi ini engine
berperilaku persis seperti sebelum PR-3.2.6 (SLA nonaktif) — backward-compatible.

## Konsekuensi
- Eskalasi bekerja end-to-end via port; adapter konkret bisa di-swap (scheduler/notification lain)
  tanpa menyentuh engine.
- Tak ada jaminan atomik cancel timer↔transisi; guard race menutup celah → deadline basi aman.
- `WorkflowInstance` kini membawa `TenantID` (diisi saat Start) untuk routing eskalasi.
- Kegagalan scheduler saat transisi terlihat sebagai error meski transisi sudah tercatat —
  caller yang persist harus sadar transisi tetap sah (didokumentasikan di `ExecuteWithComment`).

## Alternatif yang dipertimbangkan
- **Engine memanggil scheduler/notification langsung.** Ditolak: melanggar hexagonal
  (domain-no-infra-import) dan meng-couple engine ke tenant/notifikasi.
- **Cek race di adapter scheduler, bukan di core.** Ditolak: kebijakan "kapan eskalasi sah"
  adalah aturan workflow — harus di core, teruji, tak tersebar di adapter.
- **Terapkan binding tenant (RoleBindings) pada peran eskalasi sekarang.** DITUNDA ke
  "[PR-3.6.x] Konsumsi role binding": peran eskalasi tetap generik; titik penyisipan binding
  (via `TemplateStore.GetForTenant`, BUKAN `DefinitionStore.Get` mentah) sudah disiapkan di
  `NotifierEscalator`. Selama notifikasi belum di-wire produksi, binding tak berdampak.
