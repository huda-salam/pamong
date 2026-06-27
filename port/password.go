package port

// PasswordVerifier membandingkan password plaintext dengan hash tersimpan (bcrypt di adapter).
// Didefinisikan sebagai port agar use case login tetap bebas dependency kripto (hexagonal):
// detail bcrypt hidup di adapter (identity/adapter/auth), unit test memakai fake.
//
// Verify mengembalikan nil HANYA bila cocok; selain itu error. Caller (alur login) memetakan
// error apa pun ke core.ErrUnauthorized agar tidak membocorkan sebab kegagalan (hash kosong vs
// salah password) ke penyerang. Hash sebagai value parameter — adapter tidak menyimpan state.
type PasswordVerifier interface {
	// Hash menghasilkan hash bcrypt dari password plaintext (untuk pembuatan/penggantian
	// credential & seeding test). Plaintext tidak pernah disimpan.
	Hash(plain string) (string, error)
	// Verify cocokkan plaintext dengan hash; nil = cocok. Wajib timing-safe (delegasi bcrypt).
	Verify(hash, plain string) error
}
