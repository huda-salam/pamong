# core/fiscal — Fiscal Period & Locking

Mengelola periode fiskal dan penguncian. Status periode (open -> soft_closed ->
hard_closed) dan enforcement OTOMATIS pada setiap mutasi entity Lockable. Juga annual
cutoff (schema per tahun) dan carry-forward. Modul TIDAK mengecek fiscal sendiri.

## Bergantung pada
- port/fiscal.go
- core/domain (entity Lockable + FiscalDateField)

## Tidak boleh
- Membiarkan mutasi di periode hard_closed
- Mengizinkan reopen hard_closed tanpa approval pimpinan + berita acara

## Tanggung jawab
- Definisi periode: tahun, bulan (triwulan/semester = agregasi)
- Status periode & transisi satu arah (open -> soft_closed -> hard_closed)
- Enforcement otomatis: cek status sebelum mutasi entity Lockable
- Annual cutoff: buat schema tahun baru, carry-forward, schema lama read-only
- Aggregation ke warehouse saat cutoff

## File kunci
- period.go — definisi & status periode
- checker.go — implementasi FiscalChecker (dipanggil framework otomatis)
- close.go — proses tutup buku (soft/hard), validasi transisi
- cutoff.go — annual cutoff: schema baru, carry-forward, aggregation
- reopen.go — reopen hard_closed (approval berlapis)

## Konvensi khusus
- Status transisi satu arah. Reopen = pengecualian dengan workflow approval tersendiri.
- Soft_closed: hanya jurnal koreksi (permission khusus + audit ekstra).
- Hard_closed: tidak ada mutasi apapun.
- Enforcement otomatis untuk entity Lockable + FiscalDateField — modul tidak cek manual.
- Carry-forward & aggregation didefinisikan modul di manifest, dijalankan framework.

## Pitfall umum
- Modul menulis cek periode sendiri -> dilarang, framework yang handle.
- Lupa carry-forward -> tahun baru mulai dari nol (saldo hilang).
- Reopen hard_closed terlalu mudah -> harus sulit, berlapis.

## Test
- Unit: transisi status, enforcement (mutasi di hard_closed ditolak), jurnal koreksi
  di soft_closed butuh permission, carry-forward.
- go test ./core/fiscal/... -race

## Rujukan
- PRD.md, core/domain/PRD.md (Lockable), CODING_PHILOSOPHY.md #4
