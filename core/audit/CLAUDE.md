# core/audit — Audit Engine

Audit trail yang tidak bisa dimanipulasi. Setiap mutasi entity Auditable dicatat dengan
diff field-level + hash chain untuk deteksi tamper. Auto-attach — modul TIDAK menulis
kode audit. Kebutuhan paling ketat untuk sistem pemerintahan (temuan BPK).

## Bergantung pada
- port/ + stdlib
- core/domain (hook attachment ke entity Auditable)

## Tidak boleh
- Mengizinkan modifikasi audit log (immutable)
- Membiarkan entity Auditable lolos tanpa audit

## Tanggung jawab
- Audit writer: catat before/after diff, actor, timestamp, IP, workflow state
- Hash chain: tiap entry menyimpan hash entry sebelumnya (tamper detection)
- Auto-attach: hook ke semua entity Auditable, tanpa kode modul
- Query & replay: telusur audit trail per entity/per actor
- Verifikasi: pamongctl audit verify mendeteksi manipulasi

## File kunci
- engine.go — audit writer, koordinasi
- diff.go — field-level diff calculator
- chain.go — hash chain, verifikasi integritas
- query.go — query & replay audit trail
- middleware.go — auto-attach hook

## Konvensi khusus
- Audit log append-only. Tidak ada UPDATE/DELETE pada audit log.
- Hash chain: hash(entry) = H(prev_hash + entry_content). Entry pertama pakai seed.
- Diff hanya field yang berubah (before -> after), bukan seluruh record.
- Audit menyimpan actor (person_id), tenant, IP, dan workflow state transition bila ada.

## Pitfall umum
- Mencatat field sensitif mentah (mis. NIK lengkap) di audit -> masking bila perlu.
- Hash chain putus karena penulisan paralel tanpa urutan -> serialize per entity/tenant.
- Mengira audit sama dengan comment/disposisi (itu komentar manusia, terpisah).

## Test
- Unit: diff calculator, hash chain integrity, tamper detection.
- Integration: mutasi entity Auditable -> audit log dengan diff benar; modifikasi log ->
  verify gagal.
- go test ./core/audit/... -race

## Rujukan
- PRD.md, core/domain/PRD.md (Auditable), CODING_PHILOSOPHY.md #4
