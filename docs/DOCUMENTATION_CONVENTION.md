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
docs/DB_SCHEMA.md             → struktur database yang BERLAKU SEKARANG (skema + penjelasan), satu file
docs/DB_CHANGELOG.md          → riwayat setiap perubahan struktur database, satu entri per PR

{komponen}/CLAUDE.md          → konteks LOKAL untuk Claude Code & developer (ringkas)
{komponen}/PRD.md             → spesifikasi fungsional komponen (detail)

Komentar kode (godoc)         → mengapa kode ini begini, bukan apa yang dilakukannya
```

Aturan pemilihan: **keputusan dengan trade-off → ADR. Spesifikasi apa yang dibangun →
PRD. Cara kerja dengan satu komponen → CLAUDE.md lokal. Alasan satu potong kode → komentar.
Perubahan struktur DB → DB_CHANGELOG (riwayat) + DB_SCHEMA (keadaan).**

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
- **Supersede bukan satu-satunya relasi.** Bila yang berubah hanya SEBAGIAN — satu klausul,
  satu mekanisme — pakai `Amends` / `Amended by ADR-XXX`. ADR lama **tetap `Accepted`**; yang
  ditambahkan ke Status-nya hanya pointer, keputusannya tidak ditulis ulang. Syaratnya dua:
  (a) ADR baru menyebut **klausul persisnya** yang ia ganti, bukan "memperbarui ADR-XXX" saja;
  (b) tautannya **dua arah** — ADR lama menunjuk yang baru, yang baru menunjuk yang lama.
  Men-supersede satu ADR utuh demi mengubah satu kalimat justru merusak: seluruh keputusan lain
  di dalamnya ikut berhenti berlaku padahal masih dipakai. Ini juga praktik yang dipakai di luar
  (adr-tools menyediakan tautan dua arah untuk relasi selain supersede; kumpulan konvensi ADR
  yang beredar merumuskannya sebagai "ADR bersifat append-only: amandemen atau supersede,
  jangan sunting"). Contoh berjalan: ADR-018 amends ADR-009 §6 butir 2 & ADR-013 Keputusan #1.
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

## 7. Dokumentasi struktur database

Struktur DB didokumentasikan di **dua file, dengan peran yang tegas berbeda** — jangan
mencampurnya:

| File | Isi | Sifat |
|---|---|---|
| `docs/DB_SCHEMA.md` | struktur yang **berlaku sekarang**: topologi, setiap schema/tabel/kolom/index/constraint + penjelasan mengapa bentuknya begitu | ditulis-ulang mengikuti keadaan; tidak memuat riwayat |
| `docs/DB_CHANGELOG.md` | **riwayat** perubahan, satu entri per PR: apa berubah, kapan, kenapa, reversibel atau tidak | append (entri baru di atas); entri lama tidak diedit |

**Aturan wajib:** setiap PR yang mengubah struktur DB memperbarui **keduanya**, di PR yang sama.
PR yang mengubah DDL tanpa menyentuh dua file ini ditolak reviewer — sama seperti migration tanpa
down migration.

Yang dihitung sebagai "perubahan struktur DB" — bukan hanya file `.sql`:
- migrasi ber-file (`core/*/migrations/`, `modules/*/migrations/`, `identity/migrations/`)
- DDL *ensure-schema-on-write* di kode Go (mis. `tenantrole/adapter/db/schema.go`), termasuk
  penambahan kolom lewat `ALTER TABLE ... ADD COLUMN IF NOT EXISTS`
- perubahan generator DDL (`infra/db/ddl.go`) yang mengubah bentuk tabel yang dihasilkan
- perubahan index, constraint, CHECK, default — bukan hanya tabel & kolom
- perubahan **cara** skema diterapkan (registrasi migrasi, runner), meski DDL-nya tetap

Kedua file ditulis **tangan**, bukan di-generate: nilainya justru pada penjelasan *mengapa* sebuah
kolom/constraint ada — informasi yang tidak bisa dibaca dari `pg_dump`. Untuk isi teknisnya,
salin dari DDL sumber apa adanya (jangan mengarang tipe/nilai default).

Format entri changelog & penanda jalur (A/B/C) didefinisikan di kepala `DB_CHANGELOG.md`.

Tabel yang direncanakan tapi belum ada dicatat eksplisit di `DB_SCHEMA.md` §7 — supaya tidak ada
yang mencari tabel yang cuma hidup di rancangan.

---

## 8. Bahasa & gaya

- Dokumentasi & komentar: **bahasa Indonesia**.
- Istilah domain pemerintahan tetap asli (SPM, pagu, DPA) — tidak diterjemahkan.
- Istilah teknis yang lazim Inggris boleh dipertahankan (port, adapter, event, commit).
- Gaya: lugas, padat, tanpa basa-basi. Hindari kalimat panjang berbelit. Satu ide per
  paragraf.
- Tabel & diagram ASCII didorong bila memperjelas struktur.

---

## 9. Aturan pemeliharaan

- Setiap PR yang mengubah perilaku komponen **wajib** update PRD/CLAUDE.md lokal bila
  spesifikasi berubah. Reviewer menolak PR yang mengubah perilaku tanpa update dokumen.
- Setiap PR yang menyentuh interface publik core/port **wajib** menyertakan ADR baru atau
  update yang relevan.
- Komentar yang menjadi tidak akurat karena perubahan kode harus diperbaiki di PR yang
  sama — komentar menyesatkan lebih berbahaya daripada tidak ada komentar.
- Dependency baru, event baru, permission baru → otomatis tercermin di docs/contracts
  lewat regenerate; jalankan sebelum commit.
- Setiap PR yang menyentuh struktur DB (migrasi, DDL ensure-on-write, generator DDL) **wajib**
  menambah entri di `docs/DB_CHANGELOG.md` **dan** menyelaraskan `docs/DB_SCHEMA.md` — lihat §7.
