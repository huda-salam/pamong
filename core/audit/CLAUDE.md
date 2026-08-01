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
- reader.go — jalur BACA ber-gating: `QueryStore`, `Reader`, `VisibleEntry` (PR-3.8.4)
- permissions.go — `audit:sensitive:baca`
- query.go — query & replay audit trail
- middleware.go — auto-attach hook

## Konvensi khusus
- Audit log append-only. Tidak ada UPDATE/DELETE pada audit log.
- Hash chain: hash(entry) = H(prev_hash + entry_content). Entry pertama pakai seed.
- Diff hanya field yang berubah (before -> after), bukan seluruh record.
- Audit menyimpan actor (person_id), tenant, IP, dan workflow state transition bila ada.
- **Nilai ber-class `personal_id`/`specific` tersimpan TERENKRIPSI di diff** (ADR-002 tetap:
  raw = bukti; ADR-009 §6: bentuknya ciphertext). Yang mengenkripsi adalah lapis repository
  (`infra/db`), yang membuka adalah `Reader` di sini — pemisahan sengaja: enkripsi harus
  terjadi sedekat mungkin dengan penulisan, gating sedekat mungkin dengan pembacaan.
- `Reader` mengenali nilai sensitif dari BENTUKNYA (ciphertext framework, lewat `PurposeOf`),
  bukan dengan membaca ulang class field. Class sebuah field bisa berubah setelah entry lama
  tertulis; jejak audit harus diperlakukan sesuai apa yang benar-benar tersimpan.
- **Nilai diff terikat ke baris yang dimutasi** (ADR-016): `Reader` membangun koordinat baca
  dari `AuditEntry.EntityID`, tak pernah dari parameter pemanggil. Akibatnya nilai yang
  dipindah ke entry entity LAIN tampil sebagai `UndecryptableRaw`. Perpindahan antar entry
  pada entity yang SAMA tetap mungkin dari sisi kripto — itu wilayah hash chain (ADR-002),
  yang putus bila baris audit disentuh. Dua lapis untuk dua sisi ancaman berbeda.
- Tanpa permission, entry TETAP tampil — hanya nilai pengenalnya tertutup. Menyembunyikan
  entry utuh akan merusak fungsi audit itu sendiri.
- `tenant_id` untuk pembacaan selalu dari AuthContext, tak pernah parameter pemanggil.

## Pitfall umum
- Mencatat field sensitif mentah (mis. NIK lengkap) di audit -> masking bila perlu.
- Hash chain putus karena penulisan paralel tanpa urutan -> serialize per entity/tenant.
- Mengira audit sama dengan comment/disposisi (itu komentar manusia, terpisah).
- **Memverifikasi chain atas hasil `Reader`.** Nilainya sudah dibuka/ditutup sehingga hash
  tak lagi cocok → laporan tamper palsu. `VisibleEntry` sengaja tidak membawa Hash/PrevHash;
  verifikasi selalu memakai entry mentah dari `QueryStore`.

## Test
- Unit: diff calculator, hash chain integrity, tamper detection.
- Integration: mutasi entity Auditable -> audit log dengan diff benar; modifikasi log ->
  verify gagal.
- go test ./core/audit/... -race

## Rujukan
- PRD.md, core/domain/PRD.md (Auditable), CODING_PHILOSOPHY.md #4
- ADR-002 (audit diff & hash chain), ADR-009 §6, ADR-015/ADR-016 (pengikatan ciphertext)
