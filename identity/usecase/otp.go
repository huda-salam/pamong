package usecase

import (
	"errors"
	"time"

	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/identity/domain"
)

// Jalur OTP (PR-2.4.4) melengkapi LoginCitizen: persona citizen bisa login via no_hp/email TANPA
// password. Seperti seluruh alur login, ini PRA-OTENTIKASI (tanpa AuthContext, tanpa permission
// check) — yang melindungi adalah kepemilikan kanal (kode dikirim ke no_hp/email) + rate limit.
// Invariant 2.4.3 dipertahankan: token citizen TIDAK pernah membawa role internal (resolver role
// tak pernah dipanggil di jalur ini).

// otpCredTypes membatasi jalur OTP ke kanal yang BISA dikirimi kode: email & no_hp. NIK sengaja
// TIDAK termasuk (tak ada kanal kirim — NIK tetap jalur password); NIP eksklusif jalur employee.
var otpCredTypes = map[domain.CredType]bool{
	domain.CredEmail: true,
	domain.CredNoHP:  true,
}

// OTPPolicy mengumpulkan parameter kebijakan OTP di satu tempat (TTL + ambang rate limit).
// Disimpan sebagai struct ter-inject (bukan const tersebar) agar memindahkannya ke core/config
// kelak = isi struct dari config saat wiring, tanpa ubah signature use case (ADR-008 §5).
type OTPPolicy struct {
	TTL           time.Duration // umur OTP sejak diterbitkan
	RequestLimit  int           // maksimum penerbitan OTP per RequestWindow per kredensial
	RequestWindow time.Duration
	VerifyLimit   int // maksimum percobaan verifikasi per VerifyWindow per kredensial (lintas OTP)
	VerifyWindow  time.Duration
}

// DefaultOTPPolicy mengembalikan kebijakan default yang aman untuk login interaktif.
func DefaultOTPPolicy() OTPPolicy {
	return OTPPolicy{
		TTL:           5 * time.Minute,
		RequestLimit:  3,
		RequestWindow: 15 * time.Minute,
		VerifyLimit:   10,
		VerifyWindow:  15 * time.Minute,
	}
}

// errInvalidOTP adalah respons SERAGAM untuk semua kegagalan verifikasi OTP (credential tak ada,
// OTP tak ada/kedaluwarsa/sudah dipakai/attempts habis, kode salah, person non-aktif) — tidak
// membocorkan tahap mana yang gagal. Cermin errInvalidCredential pada jalur password.
func errInvalidOTP() error {
	return core.ErrUnauthorized("kode OTP tidak valid atau telah kedaluwarsa")
}

// errTooManyOTP dipakai saat rate limit (penerbitan/verifikasi) terlampaui → HTTP 429.
func errTooManyOTP() error {
	return core.ErrTooManyRequests("terlalu banyak percobaan, silakan coba lagi nanti")
}

// errOTPSendFailed dipakai saat pengiriman kode lewat MessagingPort gagal — masalah server
// transient (provider down/timeout), dipetakan ke HTTP 500 (error biasa, bukan FrameworkError).
// Pesan generik: TIDAK membocorkan detail provider (lihat MessagingError.Err yang hanya untuk log
// internal). Pengembalian error ini membuat kegagalan kirim terlihat ke pemanggil sehingga warga
// bisa mencoba ulang; refinement (retry/circuit-breaker, enumeration-resistance penuh) = ADR-008.
func errOTPSendFailed() error {
	return errors.New("gagal mengirim kode OTP, silakan coba lagi nanti")
}
