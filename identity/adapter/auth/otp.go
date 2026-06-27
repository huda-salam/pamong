package auth

import (
	"crypto/rand"
	"fmt"
	"math/big"

	"golang.org/x/crypto/bcrypt"

	"github.com/huda-salam/pamong/port"
)

// otpDigits adalah panjang kode OTP. 6 digit = ruang 10^6, dipadukan cap percobaan per-OTP
// (domain.MaxOTPAttempts) + rate limit penerbitan membuat brute-force tak praktis.
const otpDigits = 6

// otpMax adalah batas eksklusif nilai kode (10^otpDigits). Dipakai rand.Int untuk distribusi
// seragam tanpa bias modulo.
var otpMax = new(big.Int).Exp(big.NewInt(10), big.NewInt(otpDigits), nil)

// OTPCodec mengimplementasi port.OTPCodec: generasi kode dengan crypto/rand + hash/verify bcrypt.
// Sejajar BcryptVerifier (password) — satu-satunya tempat kripto OTP masuk; domain & use case
// identity tak menyentuh crypto/rand maupun bcrypt.
type OTPCodec struct {
	cost int
}

var _ port.OTPCodec = (*OTPCodec)(nil)

// NewOTPCodec membuat codec dengan cost bcrypt default (10) — memadai untuk kode berumur pendek.
func NewOTPCodec() *OTPCodec { return &OTPCodec{cost: bcrypt.DefaultCost} }

// Generate membuat kode OTP acak-kriptografis (crypto/rand) lalu hash bcrypt-nya. Kode plaintext
// dikembalikan untuk dikirim ke penerima; hash untuk disimpan. Keduanya tak pernah tertukar peran.
func (c *OTPCodec) Generate() (code string, hash string, err error) {
	n, err := rand.Int(rand.Reader, otpMax)
	if err != nil {
		return "", "", err
	}
	code = fmt.Sprintf("%0*d", otpDigits, n) // zero-pad ke otpDigits (mis. "004261")
	h, err := bcrypt.GenerateFromPassword([]byte(code), c.cost)
	if err != nil {
		return "", "", err
	}
	return code, string(h), nil
}

// Verify mengembalikan nil bila code cocok dengan hash. Timing-safe (bcrypt CompareHashAndPassword).
// Error apa pun (tak cocok, hash rusak/kosong) = gagal; caller memetakan ke respons seragam.
func (c *OTPCodec) Verify(hash, code string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(code))
}
