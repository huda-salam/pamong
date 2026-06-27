package port

// OTPCodec membuat & memverifikasi kode OTP. Cermin port.PasswordVerifier: domain & use case
// identity tetap bebas dependency kripto — generasi (crypto/rand) dan hashing/verifikasi (bcrypt)
// hidup di adapter (identity/adapter/auth). Use case hanya meminta "buatkan kode + hash" lalu
// "cocokkan kode dengan hash".
type OTPCodec interface {
	// Generate menghasilkan kode OTP plaintext (untuk dikirim ke penerima) beserta hash-nya
	// (untuk disimpan). Plaintext TIDAK pernah disimpan; hash TIDAK pernah dikirim. Kode dibuat
	// dengan sumber acak kriptografis (crypto/rand).
	Generate() (code string, hash string, err error)
	// Verify mengembalikan nil HANYA bila code cocok dengan hash; selain itu error. Wajib
	// timing-safe (delegasi bcrypt). Caller memetakan error apa pun ke respons seragam agar
	// tidak membocorkan sebab kegagalan.
	Verify(hash, code string) error
}
