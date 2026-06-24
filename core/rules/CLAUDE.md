# core/rules — Rule Engine

Regulasi sebagai data, bukan kode. Constraint bertingkat (nasional > provinsi >
kab/kota) yang bisa diubah tanpa redeploy. Membedakan logika bisnis tetap (Go) dari
aturan regulasi yang berubah tiap tahun (Permendagri, Permenkeu, PP).

## Bergantung pada
- port/ + stdlib
- core/strategy (untuk memfilter opsi strategy berdasarkan rule tier)

## Tidak boleh
- Mengeksekusi kode arbitrary dari DB — hanya expression DSL boolean/nilai
- Mengizinkan tier bawah melonggarkan constraint tier atas

## Tanggung jawab
- Rule store: simpan rule di DB, ber-versi, effective date
- Expression evaluator: evaluasi rule terhadap konteks data (boolean/nilai)
- Tiered constraint: hierarki nasional > provinsi > kab/kota
- Versioning: rule berlaku per rentang tanggal, backtest, riwayat
- Conflict detector: deteksi dua rule bertentangan sebelum aktivasi
- Custom evaluator: registrasi Go evaluator untuk logika di luar DSL

## File kunci
- engine.go — evaluator utama
- store.go — rule storage (DB), CRUD
- expression.go — DSL evaluator (boolean/nilai, tanpa side-effect)
- tiers.go — hierarki tier & resolusi constraint
- versioning.go — effective date, backtest
- conflict.go — deteksi konflik antar rule
- evaluator.go — registry custom Go evaluator

## Konvensi khusus
- Rule = data (expression string + metadata), bukan kode di DB.
- Tier: Nasional (tak bisa di-override), Provinsi (perketat, tak longgarkan nasional),
  KabKota (perketat dalam batas provinsi).
- Expression sama dengan guard DSL workflow (boolean/nilai), tanpa side-effect.
- Aktivasi rule = aksi ber-permission + ter-audit; melewati conflict check dulu.

## Pitfall umum
- Membuat rule yang butuh algoritma (bukan kondisi) — itu strategy/custom evaluator,
  bukan expression rule.
- Lupa effective date -> rule retroaktif mengubah validitas data periode lama.
- Tier bawah melonggarkan tier atas -> harus ditolak.

## Test
- Unit: evaluasi expression, tiered constraint (kab tak bisa longgar dari provinsi),
  versioning (rule lama vs baru per tanggal), conflict detection.
- go test ./core/rules/... -race

## Rujukan
- PRD.md, core/strategy/PRD.md, CODING_PHILOSOPHY.md #5
