# DOCUMENTATION_CONVENTION.md — Konvensi Dokumentasi Pamong

Cara menulis dan memelihara dokumentasi di Pamong: komentar kode, PRD, ADR, CLAUDE.md
lokal, dan dokumentasi kontrak. Prinsip dasar dari `CODING_PHILOSOPHY.md` #10:
dokumentasi hidup bersama kode, di-update di PR yang sama dengan perubahannya. Dokumentasi
basi lebih buruk daripada tidak ada.

---

## 1. Peta dokumentasi — apa ditulis di mana

```
CLAUDE.md (root)              → konvensi & aturan permanen seluruh proyek (jarang berubah)
ROADMAP.md                    → urutan pekerjaan: phase, sub-phase, jobs/PR
docs/CODING_PHILOSOPHY.md     → mengapa di balik keputusan teknis
docs/CODE_CONVENTION.md       → standar konkret penulisan kode
docs/DOCUMENTATION_CONVENTION.md → dokumen ini
docs/adr/NNN-*.md             → keputusan arsitektur + alasannya (append-only)
docs/contracts/               → kontrak yang di-generate: event topology, daftar permission, OpenAPI

{komponen}/CLAUDE.md          → konteks LOKAL untuk Claude Code & developer (ringkas)
{komponen}/PRD.md             → spesifikasi fungsional komponen (detail)

Komentar kode (godoc)         → mengapa kode ini begini, bukan apa yang dilakukannya
```

Aturan pemilihan: **keputusan dengan trade-off → ADR. Spesifikasi apa yang dibangun →
PRD. Cara kerja dengan satu komponen → CLAUDE.md lokal. Alasan satu potong kode → komentar.**

---

## 2. Komentar kode (godoc)

### Prinsip: komentar menjelaskan MENGAPA, kode menjelaskan APA
```go
// BURUK — mengulang apa yang sudah jelas dari kode
// increment counter
counter++

// BAIK — menjelaskan mengapa, konteks yang tak terlihat dari kode
// Pagu dicek ulang di sini meski sudah dicek di handler, karena antara
// request masuk dan transaksi commit, pagu bisa berubah oleh SPM lain
// yang diproses paralel (race). Ini guard terakhir sebelum mutasi.
cukup, err := uc.pagu.CekKetersediaan(ctx, in.PaguID, in.Nilai)
```

### Doc comment paket
Setiap paket punya doc comment di salah satu file (konvensi: file bernama sama dengan
paket, atau `doc.go`):
```go
// Package penatausahaan menangani siklus penatausahaan pengeluaran daerah:
// SPP → SPM → SP2D. Modul ini bergantung pada penganggaran (pagu) lewat port
// PaguChecker dan pada kepegawaian (data PPK/bendahara) lewat UserResolver.
package penatausahaan
```

### Doc comment simbol exported
- Semua tipe, fungsi, method, dan konstanta exported wajib punya doc comment.
- Diawali nama simbol: `// CreateSPM ...`, bukan `// This creates ...`.
- Untuk interface (port): jelaskan kontraknya, termasuk siapa yang biasa memanggil dan
  siapa yang mengimplementasi.

### Komentar khusus yang dikenali tooling
```go
// gov:raw-ok reason=... query=...        → izin raw SQL (lihat CODE_CONVENTION)
// TODO(nama, #issue): ...                → wajib referensi issue, tanpa itu CI menolak
// Deprecated: gunakan X sejak v1.2       → format standar godoc untuk deprecation
```

---

## 3. PRD per-komponen

PRD adalah kontrak fungsional: apa yang komponen ini harus lakukan, agar bisa dikerjakan
independen dengan konteks kecil. Struktur baku:

```markdown
# PRD: {Nama Komponen}

## Tujuan
2-4 kalimat: masalah apa yang diselesaikan, untuk siapa.

## Konteks & batasan
Posisi komponen dalam sistem. Apa yang JADI tanggung jawabnya, apa yang BUKAN
(eksplisit — mencegah scope creep).

## Model data / tipe kunci
Struct, interface, atau skema tabel inti. Secukupnya untuk memahami bentuk data.

## Kebutuhan fungsional
F1, F2, ... — tiap kebutuhan spesifik dan dapat diuji. Bukan "harus cepat" (kabur),
tapi "resolve modul < 100ms saat boot" (terukur). Sertakan edge case & failure mode.

## Kebutuhan non-fungsional
Performa (angka), keamanan, observability, batasan teknis.

## Dependency
Komponen/port lain yang dibutuhkan, dan APA yang dibutuhkan dari masing-masing.
Tandai bila bisa di-stub dulu.

## Anti-pattern / yang harus dihindari
Kesalahan umum spesifik komponen ini.

## Keputusan tertunda / open questions
Hal yang belum diputuskan, supaya tidak hilang.

## Acceptance criteria
Checklist [ ] yang, bila semua tercentang, berarti komponen selesai & benar.
```

Aturan: kebutuhan fungsional harus **dapat diuji**. Kalau sebuah kebutuhan tidak bisa
diterjemahkan jadi test, ia terlalu kabur — pertajam.

---

## 4. CLAUDE.md lokal per-komponen

Berbeda dari PRD. CLAUDE.md lokal adalah **panduan kerja ringkas** yang dibaca Claude Code
(dan developer) saat membuka komponen — tujuannya konteks kecil tapi cukup. Struktur:

```markdown
# {path} — {Nama Singkat}

Satu paragraf: apa peran komponen ini.

## Bergantung pada
Port/komponen yang boleh diimport.

## Tidak boleh
Larangan import & aturan spesifik (rujuk linter rule bila ada).

## Tanggung jawab
Bullet ringkas hal-hal yang ditangani komponen ini. Sebutkan yang BUKAN tanggung
jawabnya bila rawan disalahpahami.

## File kunci
Daftar file utama + peran satu baris.

## Konvensi khusus
Hal spesifik komponen yang tak tercakup konvensi umum.

## Pitfall umum
Kesalahan yang sering terjadi di komponen ini.

## Test
Apa yang wajib di-test, cara menjalankannya.

## Rujukan
Link ke PRD.md, port terkait, ADR relevan.
```

Target panjang: cukup untuk dipahami dalam < 2 menit. Detail mendalam ada di PRD, bukan
di sini. CLAUDE.md lokal menjawab "bagaimana saya bekerja di sini"; PRD menjawab "apa yang
harus dibangun".

---

## 5. ADR (Architecture Decision Record)

ADR mencatat keputusan arsitektur yang punya trade-off, agar alasannya tidak hilang dan
tidak diperdebatkan ulang. Wajib dibuat untuk:
- Perubahan interface publik di `core/` atau `port/`
- Penambahan port baru
- Perubahan skema event yang breaking
- Keputusan infrastruktur (pilihan DB, message broker, dsb)
- Keputusan yang pernah jadi perdebatan dan punya alternatif serius

Format: `docs/adr/NNN-judul-kebab-case.md`, nomor urut, mengikuti template `000-template.md`.

Aturan:
- ADR yang sudah `Accepted` **tidak diubah**. Keputusan berubah → ADR baru yang
  `Supersedes` ADR lama; ADR lama ditandai `Superseded by ADR-XXX`.
- Tulis juga **alternatif yang ditolak** dan alasannya — ini bagian paling berharga,
  mencegah orang mengusulkan ulang opsi yang sudah dipertimbangkan.
- Singkat lebih baik. ADR bukan esai; ia keputusan + konteks + konsekuensi.

---

## 6. Dokumentasi kontrak (di-generate)

`docs/contracts/` berisi dokumentasi yang **di-generate dari kode**, bukan ditulis tangan:
- `events.md` — topology event: siapa produce, siapa consume (dari manifest semua modul)
- `permissions.md` — daftar semua permission + group + export/import (dari manifest)
- `openapi.json` — spec API (dari rute + entity def)

Karena di-generate, jangan edit manual — ubah sumbernya (manifest, entity def), lalu
regenerate via `pamongctl`. Hasil generate ikut di-commit agar bisa di-review perubahannya.

---

## 7. Bahasa & gaya

- Dokumentasi & komentar: **bahasa Indonesia**.
- Istilah domain pemerintahan tetap asli (SPM, pagu, DPA) — tidak diterjemahkan.
- Istilah teknis yang lazim Inggris boleh dipertahankan (port, adapter, event, commit).
- Gaya: lugas, padat, tanpa basa-basi. Hindari kalimat panjang berbelit. Satu ide per
  paragraf.
- Tabel & diagram ASCII didorong bila memperjelas struktur.

---

## 8. Aturan pemeliharaan

- Setiap PR yang mengubah perilaku komponen **wajib** update PRD/CLAUDE.md lokal bila
  spesifikasi berubah. Reviewer menolak PR yang mengubah perilaku tanpa update dokumen.
- Setiap PR yang menyentuh interface publik core/port **wajib** menyertakan ADR baru atau
  update yang relevan.
- Komentar yang menjadi tidak akurat karena perubahan kode harus diperbaiki di PR yang
  sama — komentar menyesatkan lebih berbahaya daripada tidak ada komentar.
- Dependency baru, event baru, permission baru → otomatis tercermin di docs/contracts
  lewat regenerate; jalankan sebelum commit.
