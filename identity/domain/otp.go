package domain

import (
	"time"

	"github.com/google/uuid"
)

// OTP adalah kode satu-kali-pakai milik sebuah credential (email/no_hp), untuk login persona
// citizen tanpa password (jalur OTP, PR-2.4.4). Ephemeral: berumur pendek (TTL), sekali pakai
// (Consumed), dan jumlah percobaan verifikasinya dibatasi (Attempts) untuk mencegah tebakan.
//
// CodeHash menyimpan HASH kode (bcrypt) — bukan plaintext (cermin Credential.SecretHash). Kode
// plaintext hanya hidup sesaat: dibuat, dikirim ke penerima lewat MessagingPort, lalu dilupakan.
type OTP struct {
	ID           uuid.UUID
	CredentialID uuid.UUID
	CodeHash     string
	ExpiresAt    time.Time
	ConsumedAt   *time.Time // non-nil = sudah dipakai (atau dihanguskan); tak bisa dipakai lagi
	Attempts     int        // jumlah verifikasi gagal yang sudah tercatat
	CreatedAt    time.Time
}

// MaxOTPAttempts membatasi tebakan kode per OTP. 6 digit = 10^6 kemungkinan; dengan cap ini,
// peluang tebak satu OTP ≤ MaxOTPAttempts/10^6 sebelum hangus. Penerbitan OTP juga dibatasi
// rate limiter di use case (lihat OTPPolicy) sehingga total ruang tebak penyerang kecil.
const MaxOTPAttempts = 5

// IsConsumed melaporkan apakah OTP sudah tak bisa dipakai (sudah diverifikasi sukses atau
// dihanguskan karena attempts habis).
func (o *OTP) IsConsumed() bool { return o.ConsumedAt != nil }

// IsUsable melaporkan apakah OTP masih bisa diverifikasi pada saat now: belum dipakai, belum
// kedaluwarsa, dan attempts belum mencapai batas. Fungsi murni — teruji tanpa DB.
func (o *OTP) IsUsable(now time.Time) bool {
	if o.IsConsumed() {
		return false
	}
	if !now.Before(o.ExpiresAt) {
		return false
	}
	if o.Attempts >= MaxOTPAttempts {
		return false
	}
	return true
}

// AttemptsExhausted melaporkan apakah batas percobaan tercapai (caller menghanguskan OTP).
func (o *OTP) AttemptsExhausted() bool { return o.Attempts >= MaxOTPAttempts }
