// Package auth adalah driven adapter kriptografi credential identity: hashing & verifikasi
// password. Inilah satu-satunya tempat library bcrypt (golang.org/x/crypto) masuk — domain &
// use case identity tak pernah menyentuhnya (hexagonal; mereka bergantung pada
// port.PasswordVerifier). Cermin identity/adapter/token untuk JWT.
package auth

import (
	"errors"

	"golang.org/x/crypto/bcrypt"

	"github.com/huda-salam/pamong/port"
)

// BcryptVerifier mengimplementasi port.PasswordVerifier dengan bcrypt. bcrypt sudah
// timing-safe (CompareHashAndPassword) & menyimpan salt + cost di dalam hash, sehingga
// Verify tidak butuh parameter tambahan.
type BcryptVerifier struct {
	cost int
}

var _ port.PasswordVerifier = (*BcryptVerifier)(nil)

// NewBcryptVerifier membuat verifier dengan cost default bcrypt (10). Cost lebih tinggi bisa
// dikonfigurasi kelak; default sudah memadai untuk login interaktif.
func NewBcryptVerifier() *BcryptVerifier {
	return &BcryptVerifier{cost: bcrypt.DefaultCost}
}

// Hash menghasilkan hash bcrypt. Password yang melebihi 72 byte (batas bcrypt) ditolak
// eksplisit agar tidak terpotong diam-diam (vektor bypass: dua password beda dengan 72 byte
// awal sama akan dianggap cocok).
func (v *BcryptVerifier) Hash(plain string) (string, error) {
	if len(plain) > 72 {
		return "", errors.New("password melebihi 72 byte (batas bcrypt)")
	}
	h, err := bcrypt.GenerateFromPassword([]byte(plain), v.cost)
	if err != nil {
		return "", err
	}
	return string(h), nil
}

// Verify mengembalikan nil bila plain cocok dengan hash. Error apa pun (tidak cocok, hash
// rusak/kosong) berarti gagal — caller memetakannya ke ErrUnauthorized tanpa membocorkan sebab.
func (v *BcryptVerifier) Verify(hash, plain string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain))
}
