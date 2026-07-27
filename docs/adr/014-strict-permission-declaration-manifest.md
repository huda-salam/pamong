# ADR-014: Manifest sebagai titik deklarasi permission strict (segregation of duties)

## Status
Accepted

## Konteks
`permission.Engine` sudah menegakkan resolusi konflik PENUH sejak PR-2.3.3 (F7 PRD):
untuk permission yang ditandai **strict**, izin antar role non-global (scoped + tenant)
memakai **INTERSECTION** — izin hanya bila SEMUA role non-global yang dipegang actor
memberi perm, dan minimal satu memberi. Ini alat *segregation of duties* (SoD): memegang
satu role yang tidak memberi perm strict akan **memblokir**-nya, sehingga strict dipakai
hemat. Role **global** tetap menang tanpa syarat (termasuk atas strict).

Konstruktor `NewEngine(catalog, strict ...Permission)` sudah menerima daftar permission
strict. Tetapi sampai PR-5.1.2b **tidak ada titik deklarasi**: `evaluatorFactory` (composition
root gateway) selalu membangun Engine dengan set strict KOSONG, jadi semua permission
di-resolve sebagai union murni. `core/domain.PermissionDef` (kontrak manifest) hanya punya
`Name` + `Label` — modul tak punya cara menyatakan bahwa sebuah permission bersifat strict.

Pertanyaan yang diputuskan ADR ini: **di mana otoritas untuk menandai sebuah permission
sebagai strict berada, dan bagaimana ia mengalir ke Engine.**

## Keputusan

**1. Manifest modul adalah satu-satunya titik deklarasi strict.**
`domain.PermissionDef` bertambah field `Strict bool` (default `false` = permission biasa /
union). Modul yang memiliki permission-lah yang menyatakan sifat SoD-nya, berdampingan dengan
definisi permission itu sendiri (`{modul}:{entity}:{aksi}` di `Groups`). Ini sejalan dengan
prinsip framework: permission didefinisikan di manifest (kode, ter-review, ter-compile), bukan
data DB. Perubahan sifat strict = perubahan kode + review, bukan konfigurasi runtime — tepat
untuk keputusan sekelas segregation of duties.

**2. Strict dikumpulkan saat boot dan disuntik ke Engine lewat composition root.**
`Registry.StrictPermissions()` menelusuri seluruh manifest modul terdaftar dan mengembalikan
daftar permission strict (unik, terurut) — pola yang sama dengan `ExportedPermissions()` yang
juga menurunkan katalog lintas-modul dari manifest tanpa menyentuh paket modul. `cmd/server`
memanggilnya SETELAH `Registry.Validate()` (hanya modul valid berkontribusi) dan menyuntik hasil
ke `evaluatorFactory`, yang meneruskannya ke SETIAP `NewEngine` (jalur citizen central-only
maupun employee composite). Engine tetap tenant-agnostic; strict adalah properti global proses
(satu permission bersifat strict di mana pun ia dievaluasi), bukan per-tenant.

**3. Field aditif & backward-compatible — bukan perubahan breaking.**
`Strict` zero-value `false` menjaga perilaku manifest lama (semua union) tanpa perubahan.
Tidak ada modul eksisting yang menandai strict saat ADR ini ditulis, jadi efek fungsional
langsung = nol; yang dibangun adalah *jalur*-nya, sehingga saat modul keuangan/aset pertama
mendeklarasikan perm strict, penegakannya sudah aktif tanpa perubahan gateway.

## Konsekuensi
- Kontrak publik core (`PermissionDef`) bertambah satu field. Aditif; manifest & test lama
  kompilasi tanpa perubahan.
- Penegakan strict kini AKTIF di gateway (bukan lagi dorman): begitu sebuah manifest menandai
  `Strict: true`, Engine memberlakukan intersection untuk perm itu. Konsekuensi sengaja —
  memegang role tambahan yang tak memberi perm strict akan memblokirnya. Dokumentasikan jelas
  di sisi modul saat pertama dipakai agar tak mengejutkan.
- Strict bersifat proses-global: bila dua modul kebetulan memakai nama permission sama (tak
  seharusnya — prefix modul mencegahnya) dan salah satu menandainya strict, ia strict di mana
  pun. Prefix `{modul}:` membuat tabrakan nama praktis mustahil, jadi ini bukan risiko nyata.
- `IsStrict`/intersection sudah ter-test di `core/permission`; wiring manifest→factory→Engine
  ditutup test di `core/domain` (kumpul strict) & `cmd/server` (propagasi ke keputusan Engine).

## Alternatif yang dipertimbangkan
- **Deklarasi strict sebagai data (tenant config / DB).** Ditolak: sifat SoD sebuah permission
  adalah keputusan desain modul, bukan preferensi tenant. Menaruhnya di DB membuka "ubah aturan
  otorisasi tanpa review" — bertentangan dengan philosophy "tak ada logika ter-eksekusi di DB"
  dan dengan konteks pemerintahan (auditabilitas > fleksibilitas untuk keputusan otorisasi inti).
- **Set strict di-hardcode di `cmd/server`.** Ditolak: memusatkan pengetahuan tentang permission
  milik modul di composition root melanggar module ownership; modul lain menambah perm strict
  akan menuntut edit `cmd/server`. Manifest = tempat yang benar, dikumpulkan lewat registry.
- **Tanpa field, biarkan set strict tetap kosong (status quo 5.1.2b).** Ditolak: menyisakan
  kemampuan Engine (intersection SoD) yang tak pernah bisa diaktifkan modul — fitur setengah jadi.

## Rujukan
- Semantik resolusi: `core/permission/engine.go` (`Allows`/`IsStrict`), PR-2.3.3, F7 PRD
  (`core/permission/PRD.md`), CLAUDE.md "Lapisan role — Opsi A".
- Kode: `core/domain/manifest.go` (`PermissionDef.Strict`), `core/domain/registry.go`
  (`StrictPermissions`), `cmd/server/evaluator_factory.go`, `cmd/server/main.go`.
- Terkait: ADR-007 (JWT internal, sumber role di token), refresh catalog tenant TTL-based
  (PR-5.1.2c; invalidasi event-driven DEFERRED).
