package port

import (
	"context"
	"time"
)

// RateLimiter membatasi laju aksi ber-key dalam jendela waktu. Didefinisikan sebagai port agar
// use case (mis. alur login OTP) menegakkan batas tanpa bergantung pada implementasi penyimpanan
// (in-memory single-instance sekarang; Redis untuk multi-instance kelak — swap di wiring, use
// case tak berubah; titik ekstensi #1).
//
// Pola pemakaian: rate-limit brute-force/flooding bersifat PER-KREDENSIAL (bukan per-IP), maka
// key dirakit caller dari langkah + kredensial, mis. "otp:request:email:budi@example.com".
// Limiter tidak tahu makna key — ia hanya menghitung per (key, window).
type RateLimiter interface {
	// Allow mencatat satu percobaan untuk key dan melaporkan apakah masih dalam batas.
	// Mengembalikan allowed=false bila jumlah percobaan dalam window melebihi limit.
	// limit = maksimum percobaan; window = rentang waktu penghitungan. Kegagalan store
	// dikembalikan sebagai error (caller fail-closed: perlakukan error seperti tidak diizinkan).
	Allow(ctx context.Context, key string, limit int, window time.Duration) (allowed bool, err error)
}
