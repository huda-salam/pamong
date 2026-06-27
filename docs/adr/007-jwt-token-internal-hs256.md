# ADR-007: Token internal JWT HS256, seam verify, dan revocation via jti

## Status
Accepted

## Konteks
Sub-phase 2.4 (auth flow) butuh fondasi token: identity harus bisa **menerbitkan** token
untuk sesi yang sudah terotentikasi dan **memverifikasi**-nya pada tiap request, dengan
kemampuan **mencabut** (revoke) token sebelum kedaluwarsa. PR-2.4.1 hanya membangun
primitive ini; populasi `gateway.Context` (2.4.2) dan alur login yang mengisi klaim
identitas (2.4.3/2.4.4) menyusul.

Tiga pertanyaan harus diputuskan lebih dulu karena membentuk kontrak yang dipakai PR berikut:
1. **Algoritma tanda tangan.** Token internal diterbitkan & diverifikasi oleh proses yang
   sama (Pamong = modular monolith, satu binary). Config sudah punya `JWKSURL`/`Issuer`/
   `Audience` — tapi itu untuk memverifikasi token **SSO eksternal** (IdP lain), concern
   berbeda dari token yang kita terbitkan sendiri.
2. **Di mana verify hidup, tanpa gateway import identity.** Pola seam sudah ada di
   `port.TenantResolver`: interface di `port/`, implementasi di `identity/`, gateway pakai
   lewat port.
3. **Penyimpanan daftar token tercabut.** Token berumur pendek; daftar revoked cukup hidup
   sampai token kedaluwarsa.

## Keputusan

**1. Token internal = JWT HS256 (shared secret), library `golang-jwt/v5`.**
Karena penerbit == verifier == satu proses, tidak ada masalah distribusi kunci → simetris
(HMAC) adalah pilihan paling sederhana & murah. `golang-jwt/v5` dipakai alih-alih
hand-roll: ia menangani method-pinning dengan benar (tolak `alg=none` & alg-confusion),
area di mana kesalahan kripto sulit terdeteksi. Secret berasal dari config
(`GOV_AUTH_TOKEN_SECRET`), wajib & ≥32 byte di production (ditegakkan `config.Validate`).
Umur token dari `GOV_AUTH_TOKEN_TTL` (default 1 jam).

Token internal memakai issuer/audience **konstanta sendiri** (`pamong-identity` /
`pamong-internal`), terpisah dari `AuthConfig.Issuer`/`Audience` (yang tetap untuk SSO
eksternal). Verify mewajibkan iss & aud cocok sehingga token dari namespace lain tak lolos
sebagai token internal.

**2. Seam: `port.TokenIssuer` / `port.TokenVerifier` + `port.Claims`.**
`port/token.go` mendefinisikan `Claims` (library-agnostic — tanpa tipe JWT, sama seperti
`port.TenantInfo`) serta interface issue & verify. Codec konkret (`identity/adapter/token`)
adalah **driven adapter**: satu-satunya tempat `golang-jwt` & detail kripto token masuk;
domain/use case identity tetap nol-dependency. Gateway (2.4.2) memverifikasi token lewat
`port.TokenVerifier` tanpa import `identity/` — seam yang sama dengan `TenantResolver`.

Pembagian pengisian klaim: codec mengisi klaim **infrastruktur** (`jti`, `iat`, `exp`, plus
`iss`/`aud` internal) saat Issue; klaim **identitas** (`sub`/persona/role/tenant/…) diisi
**pemanggil** — alur login. PR-2.4.1 hanya memberi fixture test, tak menyentuh resolver role.

**3. Revocation = denylist jti durable di `id.revoked_tokens`.**
Tabel di identity DB sentral (migrasi 005): `jti` (PK), `person_id`, `expires_at`,
`revoked_at`, `reason`. Verify berkonsultasi ke `IsRevoked(jti)` **setelah** tanda tangan &
klaim sah. Durable & auditable, tanpa infra baru — menghindari fail-open yang muncul bila
daftar revoked disimpan di cache ephemeral (flush = token tercabut jadi valid lagi). Token
pendek menjaga tabel kecil; entri boleh dipurge setelah `expires_at`. `Revoke` idempoten
(ON CONFLICT DO NOTHING) agar retry/event ganda aman.

**4. Kegagalan otentikasi = HTTP 401 (`core.ErrUnauthorized`, baru).**
Token tak valid/kedaluwarsa/tercabut → `core.ErrUnauthorized` (dipetakan gateway ke 401),
berbeda dari `ErrPermissionDenied` (403: "terbukti, tapi tak boleh"). Bila store revocation
gagal, Verify **fail-closed** (menolak) dengan error internal (500), bukan meloloskan.

## Konsekuensi
- Dependency baru `github.com/golang-jwt/jwt/v5` (tanpa dependency transitif di luar stdlib).
- `port/token.go` (port baru) + `identity/adapter/token` (codec) + `identity/domain.RevokedTokenStore`
  (port) + `identity/adapter/db.RevokedTokenStore` (impl) + migrasi `005`.
- `AuthConfig` bertambah `TokenSecret`/`TokenTTLSeconds` + helper `TokenTTL()`; production wajib
  secret kuat. `core.ErrUnauthorized` + mapping 401 di gateway (additive).
- Codec **belum di-wire** ke server/middleware (ikut preseden event bus & sync engine: di-wire di
  test dulu). Live wiring dari config ke gateway auth middleware + alur login = PR-2.4.2/2.4.3.

## Alternatif yang dipertimbangkan
- **RS256 / JWKS untuk token internal.** Penerbit private key, verifier public key — berguna bila
  verifier terpisah dari penerbit (kesiapan microservice). Menambah manajemen keypair + rotasi +
  endpoint JWKS; over-build untuk monolith sekarang. Karena codec berdiri di balik interface
  (`port.TokenIssuer/Verifier`), beralih ke asimetris kelak bersifat additive (titik ekstensi #1).
  Jalur JWKS yang sudah ada di config tetap untuk verifikasi token **SSO eksternal**.
- **Hand-roll JWT (crypto std, nol dependency).** Selaras gaya repo yang banyak hand-roll, tapi
  kripto token adalah area "jangan gulung sendiri": salah menangani pin algoritma/constant-time =
  celah. Ditolak demi keamanan.
- **Revocation di cache Redis (TTL alami).** Cepat & auto-expire, tapi menambah dependency dan
  berisiko fail-open bila cache di-flush. Ditolak demi durabilitas & auditabilitas.

## Keputusan tertunda
- **Revocation per-person ("cabut semua token").** Denylist per-jti tak bisa mencabut sesi yang
  jti-nya tak diketahui (mis. saat central role dicabut). Solusi: epoch `tokens_valid_after`
  per person — token dengan `iat` lebih awal ditolak. Additive (kolom/tabel + satu cek di Verify),
  dibangun saat event `identity.central_role.dicabut` di-wire (Phase-2.4).
- **Use case revoke ber-permission + ber-audit.** PR-2.4.1 mengekspos `RevokedTokenStore` sebagai
  primitive (belum ter-wire ke handler manapun). Pembungkus use case (admin "akhiri sesi" /
  handler event) dengan permission + audit ADR-003 menyusul saat ada caller nyata.
- **Purge entri revoked kedaluwarsa.** Lazy/benar tanpa job (entri mati setelah `expires_at`);
  job pembersih menyusul saat `core/scheduler` ada (Phase-3.6+).
