# ADR-020: Pagar ukuran token, ditegakkan saat terbit

## Status
Accepted

**Amends ADR-007** (token internal JWT HS256) — menambahkan satu klausul pada Keputusan 2-nya:
pembagian pengisian klaim tetap seperti di sana, tapi `Issue` kini juga **menolak** token yang
melampaui ambang ukuran. Keputusan lain ADR-007 (HS256, seam port, revocation jti, 401) tidak
berubah.

## Konteks
`central_roles[]` + `tenant_roles[]` adalah satu-satunya klaim token yang **bertumbuh**, dan
sampai PR-W3c tak ada pagar di mana pun: tidak di `tenantRoleNameRe` (mengizinkan nama sepanjang
apa pun), tidak di resolver role, tidak di `JWTCodec.Issue`.

Diukur dengan codec produksi (`TestJWTCodec_UkuranTokenTumbuhSesuaiJumlahRole`):

| Bentuk akun | Ukuran token |
|---|---|
| tanpa role | 383 B |
| 50 role @25 karakter | ~2,3 KB (aman) |
| 100 role @100 karakter | **14,1 KB** |

Biaya per role ≈ `panjang_nama × 1,37` byte (base64 + JSON). Batas header proxy yang lazim:
nginx 8 KiB per header, ALB 16 KiB. Jadi akun yang mengakumulasi role lintas tahun — **justru
admin tenant, akun paling penting** — akhirnya menerbitkan token yang tak bisa dipakai.

**Bentuk kegagalannya itulah yang membuat ini mendesak.** Login BERHASIL (200, token terbit),
lalu SETIAP request berikutnya dijawab 400 oleh proxy — tanpa satu pun jejak di log aplikasi,
karena request tak pernah sampai ke Go (`MaxHeaderBytes` default 1 MiB, jauh di atas batas
proxy). User terkunci total dan tak ada sinyal yang menunjuk penyebabnya.

## Keputusan

**1. `JWTCodec.Issue` MENOLAK token di atas ambang; token tak diterbitkan.**
Diukur pada artefak yang benar-benar dikirim (sesudah ditandatangani — base64url + header +
tanda tangan membuat ukuran akhir tak bisa disimpulkan dari panjang klaim). Errornya
`core.ErrTokenTooLarge` (code `TOKEN_TOO_LARGE`, **HTTP 409**) dan pesannya **menyebut jumlah
role sentral + tenant serta ambangnya**, sebab penerimanya adalah orang yang baru gagal login
dan admin yang membaca log — keduanya perlu tahu bahwa yang harus dikurangi adalah ROLE, bukan
mencoba ulang. 409 dipilih karena retry tak menolong dan keadaan (himpunan role) yang harus
berubah; bukan 503 (mengundang retry sia-sia), bukan 500 generik (menyembunyikan sebab yang
actionable).

**2. Ambangnya CONFIG (`GOV_AUTH_TOKEN_MAX_BYTES`), bukan konstanta — dengan kurungan dua sisi.**
Batas yang sesungguhnya ada di proxy dan berbeda per deployment, jadi ia kebijakan ops yang
harus bisa dinaikkan tanpa rilis. Default `token.DefaultMaxBytes` = 6 KiB: di bawah nginx 8 KiB
dengan sisa ±2 KiB untuk `Authorization: Bearer ` dan header lain. **Tidak ada nilai yang
mematikan pagar** — 0/kosong berarti "pakai default", karena deployment yang lupa menyetelnya
justru yang paling butuh dilindungi. Nilai di bawah 1 KiB ditolak `config.Validate`: ia pasti
salah ketik (token tanpa role saja ~420 B) dan akibatnya memblokir SEMUA login. Nilai di atas
64 KiB (`config.MaxTokenMaxBytes`) juga ditolak: karena Keputusan 4 menurunkan `MaxHeaderBytes`
dari ambang ini, angka raksasa justru mengembalikan batas header ke kelonggaran default Go 1 MiB —
kebalikan dari tujuan pagar. Nilai efektif yang berlaku **ikut di-log saat boot** bersama
`MaxHeaderBytes`-nya, karena loader sengaja mengabaikan env var yang tak bisa di-parse: tanpa baris
itu `GOV_AUTH_TOKEN_MAX_BYTES=16k` diam-diam menyisakan default, justru di tengah insiden ketika
knob ini disetel.

**3. Ukuran token diobservasi, bukan hanya dibatasi — dan diperingatkan sebelum ditolak.**
`port.MetricsPort` bertambah `RecordSize(name, bytes, tags)` — histogram bersatuan **byte**
dengan bucket eksponensial 256 B…64 KiB, terpisah dari `RecordDuration` yang bersatuan detik.
Satu `Record` generik ditolak: bucket bergantung satuan, dan `prometheus.DefBuckets` (0,005–10)
membuat observasi byte menumpuk di `+Inf` — metrik yang ada tapi tak menjawab satu-satunya
pertanyaan yang berguna, "seberapa dekat ke batas". Token yang LOLOS masuk histogram
`auth_token_bytes`; penolakan menaikkan counter `auth_token_oversize_total` (layak dialertkan)
dan ter-log `Error` dengan person_id/tenant_id/ukuran/jumlah role. Token sendiri tak pernah
ter-log: ia kredensial, sekalipun tak terpakai.

Selain itu, token yang **masih lolos** tapi sudah melewati 80% ambang ter-log `Warn`. Ini yang
menjawab keberatan terkuat atas Keputusan 1: pagar yang hanya menolak akan mengunci akun yang tadi
masih bekerja, tepat pada saat rilis, tanpa siapa pun pernah tahu ia mendekati batas. Peringatan
lewat LOG dipilih karena ia bekerja hari ini di semua deployment — sementara kedua metrik di atas
belum bisa di-scrape: `GET /metrics` (`PrometheusMetrics.Handler()`) **belum ter-mount**, itu
lingkup PR-W6. Sampai W6 mendarat, log adalah satu-satunya sinyal yang hidup; sesudahnya histogram
menjadi cara melihat TREN dan log tetap cara melihat AKUN. Volume `Warn` terjaga oleh konstruksi:
hanya akun yang benar-benar mendekati batas yang menghasilkannya.

**4. `http.Server.MaxHeaderBytes` dinyatakan eksplisit dan DITURUNKAN dari ambang token.**
Default Go 1 MiB adalah nilai yang tak pernah dipilih siapa pun: ia di atas batas proxy mana pun
(sehingga aplikasi tak pernah jadi yang menolak) dan mengizinkan satu klien menahan 1 MiB buffer
per koneksi. `maxHeaderBytes(tokenMax)` = `max(16 KiB, tokenMax + 8 KiB slack)`, dengan ambang lebih dulu
dinormalkan `effectiveTokenMaxBytes` (0 → default, dikurung pada plafon) sehingga codec dan batas
header selalu memakai ANGKA YANG SAMA dan penjumlahannya tak bisa meluap. Diturunkan,
bukan disetel sendiri, agar dua batas ini tak bisa menyimpang: ops yang menaikkan ambang token ke
16 KiB untuk ALB tanpa batas header ikut naik akan mendapat pagar KEDUA yang menolak token yang
baru saja dinyatakan sah — kegagalan yang sama membingungkannya, hanya berpindah tempat.

## Konsekuensi
- Akun yang melewati ambang **tidak bisa login** sampai role-nya dikurangi (atau ambang
  dinaikkan). Ini pertukaran yang disengaja: penguncian yang ter-log dan menyebut sebabnya
  menggantikan penguncian senyap yang tak terdiagnosis.
- **Interaksi dengan rate limit login yang perlu diketahui operator:** `port.RateLimiter` sengaja
  tak punya `Reset`, jadi percobaan login yang BERHASIL pun memakai kuota (10/15 menit; lihat
  `loginRawKey` di `identity/usecase/password_auth.go`). Akun yang terkunci pagar ini akan
  menerima 409 `TOKEN_TOO_LARGE` pada percobaan-percobaan awal, lalu 429 bila ia terus mencoba —
  dan 429 itu menunjuk sebab yang salah. Catatan definitifnya tetap log `Error` di sisi server.
  Menambah `Reset` demi kenyamanan ditolak di tempat asalnya (ia memberi jalur "nolkan penghitung"
  yang menarik disalahgunakan) dan tidak dibuka ulang di sini.
- `port.MetricsPort` bertambah satu method → dua implementasi (`infra/observability`,
  `testkit.MockMetrics`) ikut berubah. Aditif untuk pemanggil.
- `core.ErrTokenTooLarge` + mapping 409 di gateway (aditif). `AuthConfig.TokenMaxBytes` +
  validasi lantai 1 KiB.
- `token.NewJWTCodec` beralih ke `token.Options` (struct) — dependensinya jadi enam dan dua di
  antaranya interface observasi, urutan posisional tak lagi aman. Ia juga **panic bila `Revoked`
  nil**: kesalahan perakitan yang hanya bisa terjadi di composition root, dan meloloskannya
  berarti nil-deref di `Verify` pada request pertama.
- Pagar ini tidak menyelesaikan *sebab*-nya (role bisa tumbuh tanpa batas, nama role tanpa batas
  panjang). Ia mengubah kegagalannya jadi terlihat. Membatasi jumlah/panjang role adalah
  keputusan terpisah — lihat Keputusan tertunda.

## Alternatif yang dipertimbangkan
- **Terbitkan saja, biarkan proxy yang menolak.** Nol kode. Ditolak: itu status quo, dan justru
  bentuk kegagalan yang paling mahal didiagnosis (tak ada jejak di log aplikasi).
- **Pangkas role sampai token muat.** Login tetap berhasil, jadi terasa "lebih ramah". Ditolak
  tegas: pemangkasan diam-diam **menghilangkan wewenang** — user tampak login normal lalu
  ditolak permission secara acak bergantung role mana yang kebetulan terpotong. Kegagalan
  otorisasi yang senyap lebih buruk daripada penolakan yang jujur.
- **Pindahkan role keluar dari token (token opaque + lookup sesi per request).** Menghapus
  masalah ukuran pada akarnya dan memungkinkan pencabutan seketika. Ditolak untuk sekarang:
  ia membalik keputusan dasar ADR-007 (token self-contained, verifikasi tanpa I/O) dan menambah
  lookup di jalur setiap request. Bila pertumbuhan role ternyata struktural, inilah ADR
  berikutnya — bukan menaikkan ambang tanpa batas.
- **Memasukkan permission (bukan hanya nama role) ke token** "supaya tak perlu katalog". Ini
  penyebab paling umum JWT membengkak: satu role dengan 40 permission menjadi 40 entri.
  Desain sekarang sengaja hanya membawa NAMA role. Dijaga oleh assertion biaya-per-role di
  `TestJWTCodec_UkuranTokenTumbuhSesuaiJumlahRole`.
- **`SetGauge` ukuran token terakhir**, agar `MetricsPort` tak berubah. Ditolak: dengan banyak
  user, "ukuran token terakhir" adalah derau — ia tak menunjukkan distribusi maupun tren, satu-
  satunya hal yang membuat metrik ini mencegah insiden.

## Keputusan tertunda
- **Batas jumlah & panjang nama role tenant.** `tenantRoleNameRe` tak membatasi panjang, dan tak
  ada batas jumlah assignment per user. Pagar token menangkap akibatnya, bukan sebabnya. Batas
  di pintu tulis `tenantrole` adalah perubahan perilaku bagi tenant yang mungkin sudah punya
  nama panjang — perlu keputusan sendiri (dan mungkin migrasi).
- **Exposition metrik.** `auth_token_bytes` & `auth_token_oversize_total` belum bisa di-scrape
  sampai `GET /metrics` ter-mount (**PR-W6**, sudah terjadwal). Sampai itu, sinyal yang hidup
  adalah log `Warn`/`Error` di Keputusan 3 — bukan alert dari histogram.
- **Batas laju peringatan dini.** `Warn` terbit sekali per login untuk akun yang mendekati ambang.
  Bila kelak ada akun yang login sangat sering, ini bisa jadi berisik; peredamnya (mis. sekali per
  person per jam) butuh state dan belum dibayar biayanya sekarang.
