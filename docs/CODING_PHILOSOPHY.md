# CODING_PHILOSOPHY.md — Filosofi Pengembangan Pamong

Dokumen ini menjelaskan *mengapa* di balik keputusan teknis Pamong. Setiap aturan
konkret di `CODE_CONVENTION.md` dan setiap larangan di linter berakar pada salah satu
prinsip di sini. Kalau suatu saat aturan terasa membatasi, baca kembali prinsipnya —
biasanya batasan itu sengaja, dan ada alasannya.

Konteks yang membentuk semua prinsip ini: **ini sistem keuangan & tata kelola
pemerintahan.** Salah hitung anggaran, data audit yang bisa dimanipulasi, atau
kebocoran data antar daerah bukan bug biasa — itu temuan BPK, potensi pidana, dan
kehilangan kepercayaan publik. Maka kami memilih **predictability di atas
cleverness**, **eksplisit di atas implisit**, dan **gagal cepat di atas gagal diam-diam.**

---

## 1. Convention over configuration, enforcement over documentation

Konvensi yang hanya ditulis di dokumen bukan konvensi — itu harapan. Setiap konvensi
penting di Pamong harus punya **satu lapis mekanis** yang menegakkannya:

```
Lapis 1 — Scaffolding   : pelanggaran tidak mungkin terjadi (struktur sudah benar)
Lapis 2 — Compiler      : pelanggaran gagal kompilasi (interface, tipe)
Lapis 3 — Linter        : pelanggaran merah di editor (custom analyzer)
Lapis 4 — CI gate       : pelanggaran ditolak sebelum merge
Lapis 5 — Runtime boot  : pelanggaran panic saat start, bukan saat melayani user
```

Prinsip turunan: **dorong setiap konvensi ke lapis sedini mungkin.** Kalau sebuah aturan
bisa jadi tipe (Lapis 2), jangan biarkan ia cuma jadi rule linter (Lapis 3). Kalau bisa
di-scaffold (Lapis 1), jangan biarkan ia jadi aturan yang harus diingat manusia.

Aturan di dokumen yang masih "wajib diingat" adalah **hutang** — idealnya tiap aturan
punya penegak mekanis. Saat menambah aturan baru, tanyakan: di lapis mana ini ditegakkan?

---

## 2. Hexagonal: domain tidak tahu dunia luar

Inti bisnis (domain, use case) tidak boleh tahu apakah datanya di Postgres, apakah
dipanggil lewat HTTP atau CLI, apakah event-nya lewat NATS atau memory. Domain bicara
lewat **port (interface)**; infrastruktur mengimplementasi.

Mengapa, secara konkret untuk proyek ini:
- **Testability** — logika anggaran bisa diuji tanpa DB, ribuan kali per detik.
- **Portabilitas** — tenant pindah dari shared DB ke server sendiri tanpa ubah satu
  baris domain (lihat tenant tier).
- **Kesiapan microservice** — modul di-extract jadi service dengan ganti adapter, bukan
  rewrite domain.
- **Auditabilitas** — logika bisnis terkumpul di satu tempat, bukan tersebar di handler,
  query, dan trigger DB.

Konsekuensi yang harus diterima: ada boilerplate (port + adapter). Kami terima itu —
ditebus dengan scaffolding (Lapis 1) dan entity tier (yang sederhana tidak perlu
hexagonal penuh).

---

## 3. Eksplisit mengalahkan implisit

Zero value yang diam-diam berbahaya adalah musuh. Contoh kanonik: `Auditable bool`
default `false` — developer lupa mengisi, entity tidak teraudit, dan tidak ada yang
protes sampai BPK bertanya. Maka:

- Keputusan penting tidak punya default diam-diam. Entity wajib mendeklarasikan
  `Audit` dan `Lockable` secara eksplisit (pakai tipe tanpa zero value valid bila perlu).
- Permission dan role adalah konstanta bernama, bukan string literal yang tersebar.
- Event adalah konstanta dengan schema terdaftar, bukan string bebas.
- Dependency antar modul dideklarasikan di manifest, bukan muncul diam-diam dari import.

Aturannya: **kalau sebuah keputusan berdampak pada uang, audit, atau keamanan, ia harus
ditulis, bukan diasumsikan.**

---

## 4. Gagal cepat, gagal keras, di tempat yang murah

Urutan biaya menemukan kesalahan: saat ngetik < saat compile < saat test < saat boot <
saat melayani user < saat audit setahun kemudian. Kami selalu menggeser deteksi ke kiri.

- Konfigurasi salah → panic saat boot, bukan error misterius saat request ke-1000.
- Manifest tidak valid → aplikasi menolak start.
- Workflow definition dengan syntax guard salah → ditolak saat load, bukan saat transisi.
- Migrasi tanpa down → ditolak CI, bukan ditemukan saat perlu rollback darurat.

Yang TIDAK kami lakukan: menelan error diam-diam, melanjutkan dengan nilai default
yang "kira-kira benar", atau menulis data yang mungkin korup. Untuk uang negara,
berhenti dan berteriak selalu lebih baik daripada melanjutkan dengan ragu.

---

## 5. Fleksibel di tepi, kaku di inti

Pemerintahan butuh fleksibilitas (tiap pemda beda alur, beda kebijakan) tapi juga
kepatuhan ketat (regulasi nasional tak bisa dilanggar). Kami pisahkan keduanya:

- **Yang berubah sering** (alur approval, pilihan metode, SLA, notifikasi) → configurable
  sebagai **data**, bukan kode. Bisa diubah per-tenant tanpa redeploy.
- **Yang harus dijaga** (logika perhitungan, integritas transaksi, constraint regulasi)
  → **kode** yang ter-compile, ter-test, ter-review. Tidak bisa diubah lewat config.

Prinsip kunci yang tak bisa ditawar: **tidak ada satu baris logika eksekusi yang
tersimpan sebagai kode di database.** Yang di DB hanya pilihan (identifier), susunan
(struktur workflow), atau kondisi (boolean expression sempit). Ini menutup vektor
"kode arbitrary di DB" — sangat penting untuk sistem pemerintahan.

---

## 6. Open for extension, closed for modification

Penambahan kemampuan ke depan tidak boleh menyentuh kode yang sudah jalan dan teruji.
Mekanismenya seragam: **menambah = mendaftarkan sesuatu yang baru, bukan mengubah yang
lama.** Registry pattern (strategy, workflow template, channel, evaluator, driver) dipakai
konsisten supaya varian baru cukup ditulis + didaftarkan satu baris.

Aturan untuk developer & Claude Code: bila kebutuhan baru menggoda untuk mengubah kode
lama, periksa dulu apakah ia bisa diekspresikan sebagai *registrasi* pada titik ekstensi
yang ada. Mengubah kode lama hanya dibenarkan bila tidak ada titik ekstensi yang cocok —
dan itu sinyal perlu ADR untuk menambah titik ekstensi, bukan menambal ad-hoc.

---

## 7. Modular monolith: boundary service tanpa ongkos distribusi

Satu binary, tapi batas antar modul sekuat batas antar service. Komunikasi antar modul
hanya lewat event bus dan port — tidak ada import lintas-modul, tidak ada JOIN lintas-schema.

Kami memilih ini di atas microservice karena: deployment di pemda harus sederhana, tim
masih kecil, dan kompleksitas distributed system (network failure, distributed transaction,
eventual consistency di mana-mana) tidak sepadan dengan keuntungannya di tahap ini.

Tapi kami menjaga pintu microservice terbuka: disiplin hexagonal + event + no-cross-import
memberi ~80% kesiapan. Saat satu modul benar-benar perlu di-extract (biasanya karena
kebutuhan scaling yang berbeda), ganti adapter port dari in-process ke gRPC — domain tak
berubah. Saga orchestrator dan data replication dibangun saat itu, bukan sekarang.

---

## 8. Konsistensi mengalahkan preferensi pribadi

Dalam tim, kode yang seragam lebih mudah dibaca, di-review, dan di-maintain daripada kode
yang "lebih pintar" tapi idiosinkratik. Kalau ada dua cara yang sama baik, pilih yang
sudah jadi konvensi. Gaya pribadi mengalah pada `gofmt`, pada konvensi penamaan, pada pola
yang sudah ada di modul referensi.

Ini berlaku ganda untuk Claude Code: selalu pelajari `modules/surat_masuk/` sebagai
referensi sebelum membuat modul baru, dan ikuti polanya — bukan pola "terbaik menurut
training data."

---

## 9. Test sebagai spesifikasi yang dieksekusi

Test bukan formalitas pasca-coding. Untuk Pamong, test adalah cara kami membuktikan
bahwa: permission benar-benar ditegakkan, event benar-benar terbit, validasi benar-benar
menolak input buruk, dan periode terkunci benar-benar tidak bisa dimutasi.

Tiap use case wajib punya minimal: satu happy path, satu jalur permission-denied, satu
jalur validasi gagal. Bukan demi angka coverage — demi bukti bahwa pengaman bekerja.
Test permission-denied yang hilang berarti tidak ada bukti bahwa endpoint terlindungi.

---

## 10. Dokumentasi yang hidup bersama kode

PRD, ADR, dan komentar bukan artefak sekali tulis. PRD per-komponen adalah kontrak
fungsional; ADR adalah jejak keputusan yang tak boleh hilang; komentar menjelaskan
*mengapa*, bukan *apa* (kode sudah menjelaskan apa). Dokumentasi yang basi lebih buruk
daripada tidak ada — maka ia di-update bersama perubahan kode, di PR yang sama.

---

## Hierarki saat prinsip berkonflik

Kadang prinsip bertabrakan (mis. eksplisit vs sedikit boilerplate). Urutan prioritas:

```
1. Keamanan & integritas data   (tidak pernah dikompromikan)
2. Kebenaran (correctness)
3. Auditabilitas & kejelasan
4. Predictability & konsistensi
5. Kemudahan pengembangan (DX)
6. Performa
7. Keringkasan kode
```

Performa dan keringkasan ada di bawah — bukan karena tidak penting, tapi karena untuk
sistem pemerintahan, salah yang cepat dan ringkas jauh lebih mahal daripada benar yang
sedikit lebih lambat dan verbose. Optimasi dilakukan saat ada bukti bottleneck, bukan
atas dasar dugaan.

---

## 11. Testing utility: generik di testkit, spesifik di modul

`testkit/` hanya berisi mock dan helper yang **generik dan bisa dipakai semua modul**:
`MockRepo[T]`, `MockPublisher`, `MockSequence`, `MockMetrics`, `MockUserResolver`,
`TestContext`.

Mock yang **spesifik pada satu modul** — misalnya stub `DisposisiRepository` yang
return type-nya mengandung `*domain.Disposisi` dari modul `surat_masuk` — **tidak
boleh ada di testkit**. Alasannya dua:

1. **Import coupling**: testkit yang mengimport `modules/X/domain` menjadi bergantung
   pada modul X. Bila modul X berubah, testkit harus ikut berubah — padahal testkit
   adalah fondasi bersama.

2. **Redundansi**: Go sudah menyediakan cara mudah untuk stub lokal. Satu struct
   anonim dengan dua method di file `_test.go` tidak lebih panjang dari import testkit.

Pola yang benar:

```go
// modules/surat_masuk/usecase/disposisi_test.go

// Stub lokal — tidak perlu diekspor atau dipindah ke testkit.
type stubDisposisiRepo struct{}

func (r *stubDisposisiRepo) Save(_ context.Context, _ *domain.Disposisi) error { return nil }
func (r *stubDisposisiRepo) ListBySurat(_ context.Context, _ uuid.UUID) ([]*domain.Disposisi, error) {
    return nil, nil
}
```

Prinsip turunan: **testkit tahu tentang port, tidak tahu tentang domain modul bisnis.**
Batas ini harus dipertahankan agar testkit bisa dipakai bebas oleh semua modul tanpa
saling tergantung.
