# core/strategy — Strategy Registry

Selectable strategy pattern. Untuk titik keputusan dengan beberapa varian algoritma/
kebijakan yang sah (FIFO/LIFO/average, pendekatan aset/beban). Developer menulis
implementasi, tenant memilih lewat key. Di DB HANYA tersimpan key, bukan logika.

## Bergantung pada
- port/strategy.go
- core/rules (untuk filter opsi berdasarkan tier), core/config (penyimpanan pilihan)

## Tidak boleh
- Menyimpan logika di DB — hanya identifier
- Mengizinkan if tenant.x untuk pilihan algoritma [linter: tenant-branch-must-be-strategy]
- Mengizinkan key tak terdaftar dipakai [linter: strategy-key-must-be-registered]

## Tanggung jawab
- Registry ber-key: Register(key, impl), Resolve(tenant, point)
- Validasi: key tak terdaftar -> tolak
- Filter opsi: opsi tersedia = developer-provided irisan rule-tier-allowed
- Versioning pilihan: ber-versi + effective date, non-retroaktif
- Hook validator koherensi kombinasi lintas-pilihan

## File kunci
- registry.go — Register/Resolve/AvailableOptions
- selection.go — penyimpanan & resolusi pilihan tenant (lewat core/config)
- versioning.go — pilihan ber-versi + effective date
- coherence.go — hook validator kombinasi

## Konvensi khusus
- Decision point = titik di mana varian dipilih, key format {modul}.{titik}.
- Strategy key format {modul}.{titik}.{varian} (mis. keuangan.persediaan.fifo).
- Pilihan disimpan di gov.tenant_configs (lewat core/config, ber-scope).
- Perubahan pilihan = aksi ber-permission + ter-audit, non-retroaktif untuk periode
  terkunci.

## Pitfall umum
- Mengira ini untuk susunan langkah (itu workflow). Strategy = isi satu langkah berganti;
  workflow = susunan langkah berganti.
- Lupa filter opsi dengan rule tier -> tenant bisa pilih yang dilarang regulasi.
- Pilihan retroaktif mengubah perhitungan periode lama.

## Test
- Unit: register & resolve, key invalid ditolak, filter opsi by rule tier, versioning
  non-retroaktif, validator koherensi.
- go test ./core/strategy/... -race

## Rujukan
- PRD.md, core/rules/PRD.md, CODING_PHILOSOPHY.md #5 & #6
