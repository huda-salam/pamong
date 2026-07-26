# infra/messaging — Messaging Adapter (SMS/email)

Driven adapter: implementasi port.MessagingPort. Transport pesan keluar untuk OTP identity
(RequestOTP) dan channel notifikasi email (core/notification.EmailChannel). Driver ber-registry
seperti infra/storage — mengganti driver tidak mengubah kode pemanggil.

## Bergantung pada
- port/messaging.go, core/config; stdlib (net/smtp, log/slog)

## Tidak boleh
- Menyusun konten pesan (template) — itu tugas caller/core.notification. Port ini hanya transport.
- Membocorkan detail provider ke klien — bungkus kegagalan sebagai *port.MessagingError.

## Tanggung jawab
- Factory pemilih driver dari config (log | smtp) — messaging.go
- Kirim SMS/email; petakan kegagalan → *port.MessagingError (INVALID_RECIPIENT/TRANSIENT/PERMANENT)

## File kunci
- messaging.go — entry; NewFromConfig (switch driver)
- log.go — LogDriver: dev/test, catat ke log, selalu sukses (DILARANG di production)
- smtp.go — SMTP: email nyata via stdlib net/smtp; SMS tidak didukung (PERMANENT)

## Konvensi khusus
- Driver "log" mencatat body penuh (termasuk kode OTP) — sengaja, agar dev bisa membaca kode.
  config.Validate MENOLAK driver=log di production (fail-fast); jangan longgarkan.
- Driver SMTP email-only. SMS nyata = driver provider terpisah (Twilio/gateway pemda),
  dipilih saat onboarding — tambah case baru di NewFromConfig, jangan cabang di caller.
- Klasifikasi error SMTP masih kasar (semua kegagalan kirim = TRANSIENT); driver provider
  nyata boleh memetakan lebih presisi.

## Pitfall umum
- Memakai driver=log di production → OTP bocor ke log. Sudah dijaga Validate; jangan bypass.
- Menaruh logika template/perakitan pesan di sini → langgar batas (port = transport murni).

## Test
- Unit: factory memilih driver benar; log selalu sukses; SMTP membentuk pesan RFC 5322 +
  encode subject non-ASCII, memetakan alamat kosong→INVALID_RECIPIENT, gagal kirim→TRANSIENT,
  SMS→PERMANENT. sendMail diinjeksi agar tak buka koneksi nyata.
- go test ./infra/messaging/... -race

## Rujukan
- port/messaging.go; identity/usecase/request_otp.go (pemakai OTP); core/notification/channel_email.go
- [[plan-notification-completion]] (arc N3), titik ekstensi #1 (registry pattern)
