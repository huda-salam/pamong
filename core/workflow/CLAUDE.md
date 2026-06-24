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
- Template selection: tenant memilih template ber-key + binding peran->jabatan
- Guard expression DSL: compile saat load, evaluate boolean, tanpa side-effect
- SLA & deadline: timer per-state, eskalasi otomatis (lewat scheduler)
- History: riwayat transisi immutable per-instance

## BUKAN tanggung jawab
- Apa yang terjadi di dalam satu langkah (itu use case modul)
- Pemetaan peran->orang konkret (itu core/permission + kepegawaian)
- Pengiriman notifikasi (itu core/notification; workflow hanya memicu)

## File kunci
- engine.go — state machine runner, transition executor
- definition.go — struct definisi, definition store (DB)
- loader.go — YAML seed loader + schema validation
- guard.go — DSL compiler & evaluator (boolean only)
- sla.go — deadline tracker, eskalasi
- history.go — transition history (immutable)
- template.go — template selection & parameter binding

## Konvensi khusus
- Guard di-compile saat definisi di-load. Syntax error -> tolak di pintu masuk.
- Action di YAML = nama use case. Engine memanggilnya lewat dispatcher, tidak inline.
- Instance menyimpan versi definisi saat instance dimulai; perubahan definisi tidak
  mengubah instance berjalan.
- Perubahan definisi = aksi ber-permission + ter-audit.

## Pitfall umum
- Menaruh logika (hitung, validasi data) di guard atau action — dilarang. Guard hanya
  boolean read-only; action hanya panggil use case.
- Membuat guard yang butuh fungsi custom kompleks — sinyal logika harus pindah ke use case.
- Lupa versioning instance -> perubahan workflow merusak proses berjalan.

## Test
- Unit: transisi valid/ilegal, guard evaluation (boolean), template selection.
- Integration: dua tenant template berbeda + use case identik; SLA lewat -> eskalasi.
- go test ./core/workflow/... -race

## Rujukan
- PRD.md, port/workflow.go, CODING_PHILOSOPHY.md #5 (fleksibel di tepi)
