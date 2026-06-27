# ADR-008: OTP login citizen, seam messaging & rate-limit (brute-force protection)

## Status
Accepted

## Konteks
PR-2.4.4 melengkapi alur login persona `citizen` (portal publik) dengan **jalur OTP**
(no_hp/email tanpa password). PR-2.4.3 sudah membangun `LoginCitizen` berbasis password
(bcrypt); sebagian besar invariant citizen (tanpa role internal, respons kegagalan seragam)
sudah ditegakkan di sana. Yang tersisa di 2.4.4 — dan ditarik masuk dari DEFERRED
(REVIEW_BACKLOG A5):

1. **OTP sebagai credential satu-kali**: kode dikirim ke kanal (SMS/email), diverifikasi,
   lalu menerbitkan token `persona=citizen` tanpa role internal — persis seperti jalur password.
2. **Rate-limit & proteksi brute-force** untuk login (request OTP + verifikasi). Ini DEFERRED
   yang ditarik masuk; seam-nya harus diputuskan: middleware gateway vs port di use case.

Tiga pertanyaan desain membentuk kontrak yang dipakai PR berikut:
- **Di mana OTP digenerasi & di-hash** tanpa membocorkan kripto ke domain/use case?
- **Bagaimana OTP dikirim** tanpa use case mengimpor infra (SMS/email provider)?
- **Di mana rate-limit ditegakkan**, dan apa polanya agar murah berkembang?

## Keputusan

**1. OTP = credential ephemeral terpisah, hash bcrypt, TTL pendek, attempts terbatas.**
Tabel baru `id.otps` (migrasi 006): `credential_id` (FK ke `id.credentials`), `code_hash`
(bcrypt — **bukan plaintext**, cermin `secret_hash` credential), `expires_at` (TTL pendek,
default 5 menit), `consumed_at` (sekali pakai), `attempts` (cap verifikasi per-OTP). OTP
menempel pada credential `email`/`no_hp` yang sudah ada — bukan jalur identitas baru. Kode
tak pernah di-log atau di-return ke klien (klien hanya diberi tahu "OTP dikirim").

**2. Seam kripto OTP: `port` `OTPCodec` (Generate+Verify), impl di `identity/adapter/auth`.**
Mencerminkan `port.PasswordVerifier` persis: domain & use case identity tetap nol-dependency
kripto. `Generate()` memakai **`crypto/rand`** (bukan `math/rand`) untuk 6 digit, lalu bcrypt
hash; `Verify(hash, code)` timing-safe (delegasi bcrypt `CompareHashAndPassword`). Satu-satunya
tempat `crypto/rand` + bcrypt OTP masuk = adapter `auth`, sebelah `password.go`.

**3. Seam pengiriman: `port.MessagingPort` (port baru, lintas-modul).**
`port/messaging.go` mendefinisikan `SendSMS` / `SendEmail`. Use case `RequestOTP` mengirim
lewat port ini — tidak mengimpor provider apa pun. Implementasi konkret (Twilio/SNS/SMTP)
hidup di `infra/messaging`, di-wire saat live wiring. Error pengiriman dibungkus
`MessagingError` (Code: `INVALID_RECIPIENT` | `TRANSIENT` | `PERMANENT`) agar caller bisa
memetakan ke status yang tepat kelak **tanpa membocorkan detail** ke klien — sekarang
sederhana (semua kegagalan → satu error generik), seam siap untuk retry/circuit-breaker.

**4. Rate-limit = `port.RateLimiter` di use case (Opsi B), bukan middleware gateway.**
OTP brute-force adalah serangan **per-kredensial**, bukan per-IP/DDoS: penyerang menebak kode
untuk satu email/no_hp, atau membanjiri satu kanal. Limiter per-IP di gateway tak menangkap ini
(serangan bisa dari banyak IP) dan tak bisa membedakan kebijakan request vs verify. Karena itu
penegakan ada di use case, ber-key per kredensial per-langkah:
- `RequestOTP`: batasi penerbitan/flooding (default 3 / 15 menit per kredensial).
- `VerifyOTP`: dua lapis — cap `attempts` per-OTP (default 5, di entity) **dan** limiter
  per-kredensial (default 10 / 15 menit) untuk mencegah rapid-fire lintas banyak OTP.

`port.RateLimiter.Allow(ctx, key, limit, window)` library-agnostic. Impl awal in-memory
(`infra/ratelimit`, single-instance, cukup untuk Tier 1); swap ke Redis untuk multi-instance =
ubah wiring saja, use case tak tersentuh (titik ekstensi #1).

**Defense-in-depth (Opsi C) ditunda, bukan ditolak.** Layer pertama per-IP di gateway dapat
ditambah kelak sebagai komplemen (bukan pengganti) limiter per-kredensial — keduanya berdiri di
seam berbeda dan tidak saling mengubah. Dicatat sebagai titik ekstensi.

**5. Kebijakan OTP sebagai struct ter-inject (`OTPPolicy`), bukan const tersebar.**
TTL, panjang kode, dan ambang limit hidup di satu `OTPPolicy` dengan `DefaultOTPPolicy()`.
Sekarang nilai default di kode; memindahkannya ke `core/config` kelak = isi struct dari config
saat wiring, **tanpa ubah signature** use case.

**6. Rate-limit terlampaui = HTTP 429 (`core.ErrTooManyRequests`, baru).** Additive di
`core/errors.go` + mapping gateway. Berbeda dari 401 (kredensial salah) & 403. OTP/kode salah
tetap memakai respons **seragam** 401 (`errInvalidCredential`) seperti jalur password — tidak
membocorkan apakah kredensial ada, OTP kedaluwarsa, atau attempts habis.

## Konsekuensi
- Port baru: `port/messaging.go` (`MessagingPort`, `MessagingError`), `port/ratelimit.go`
  (`RateLimiter`), `port.OTPCodec` (di `port/otp.go`). Domain port baru
  `identity/domain.OTPRepository`.
- Adapter baru: `identity/adapter/auth.OTPCodec` (crypto/rand+bcrypt),
  `identity/adapter/db.OTPRepo` (Postgres), migrasi `006_create_otps`.
- Use case baru: `RequestOTP`, `VerifyOTP` — keduanya pra-otentikasi (tanpa AuthContext),
  menerbitkan token citizen tanpa role internal (invariant 2.4.3 dipertahankan).
- `core.ErrTooManyRequests` + mapping 429 (additive). `infra/ratelimit` (in-memory) +
  `infra/messaging` impl **belum di-wire** ke server (ikut preseden token codec & sync engine:
  diuji di test dulu; live wiring saat HTTP/router Phase 5.1.1).
- OTP hanya untuk credential `email`/`no_hp` (kanal kirim ada). `nik` tetap jalur password
  (tak ada kanal kirim OTP); `nip` tetap eksklusif jalur employee.

## Alternatif yang dipertimbangkan
- **Rate-limit sebagai middleware gateway per-IP (Opsi A).** Satu tempat, tapi tak menangkap
  brute-force per-kredensial (multi-IP) & tak bisa kebijakan berbeda request vs verify. Ditolak
  sebagai *satu-satunya* lapis; boleh hadir kelak sebagai komplemen (Opsi C).
- **OTP plaintext di DB dengan TTL.** Lebih sederhana, tapi bocornya tabel = semua OTP aktif
  terekspos. Ditolak — hash bcrypt konsisten dengan kebijakan credential.
- **`math/rand` untuk kode.** Tidak aman secara kriptografis (dapat diprediksi). Ditolak —
  wajib `crypto/rand`.
- **OTP sebagai baris di `id.credentials` (cred_type baru).** Mencampur credential permanen
  dengan token ephemeral (TTL, attempts, consumed) → mengotori tabel & uniknya. Ditolak;
  tabel terpisah lebih bersih.

## Keputusan tertunda
- **Live wiring** `RequestOTP`/`VerifyOTP` ke handler HTTP + `infra/messaging` & `infra/ratelimit`
  konkret — saat router Phase 5.1.1 (ikut preseden login 2.4.3 yang juga belum ter-wire).
- **Limiter terdistribusi (Redis)** untuk multi-instance + lapis gateway per-IP (Opsi C) —
  additive di balik `port.RateLimiter`.
- **Konfigurasi `OTPPolicy` dari `core/config`** (`GOV_AUTH_OTP_*`) — saat ada kebutuhan tenant
  menyetel TTL/limit; sekarang default kode cukup.
- **Purge OTP kedaluwarsa** — lazy (entri mati setelah `expires_at`); job pembersih saat
  `core/scheduler` ada (Phase-3.6+), cermin `revoked_tokens`.
- **API-key machine principal & token lifecycle/refresh** — ADR terpisah (di luar 2.4.4).
