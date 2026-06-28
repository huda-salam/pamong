package eventbus

import "time"

// RetryPolicy mengatur batas percobaan ulang dan interval backoff untuk OutboxRelay.
// Saat Dispatch gagal, relay menghitung waktu percobaan berikutnya dan menyimpannya
// di next_retry_at. Setelah attempts >= MaxAttempts, baris dipindah ke DLQ
// (failed_at di-set) dan tidak dipercobakan lagi sampai operator me-reset secara manual.
type RetryPolicy struct {
	MaxAttempts int
	BackoffBase time.Duration
	BackoffMax  time.Duration
}

// DefaultRetryPolicy mengembalikan kebijakan aman: 5 percobaan, backoff awal 5 detik,
// maksimum 1 jam. Menghasilkan jadwal: 5s → 10s → 20s → 40s → DLQ.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxAttempts: 5,
		BackoffBase: 5 * time.Second,
		BackoffMax:  time.Hour,
	}
}

// NextRetry menghitung kapan percobaan berikutnya dilakukan setelah kegagalan ke-n.
// attempts adalah jumlah percobaan SETELAH kegagalan ini (sudah ter-increment).
// Mengembalikan (waktu retry, false) bila masih bisa dicoba, atau (zero, true) bila
// sudah mencapai batas — sinyal masuk DLQ.
func (p RetryPolicy) NextRetry(attempts int) (nextRetryAt time.Time, isDLQ bool) {
	if attempts >= p.MaxAttempts {
		return time.Time{}, true
	}
	// exponential backoff: base * 2^(attempts-1)
	// attempts=1 → base*1, attempts=2 → base*2, attempts=3 → base*4, dst.
	shift := attempts - 1
	if shift < 0 {
		shift = 0
	}
	backoff := p.BackoffBase * (1 << uint(shift))
	if backoff > p.BackoffMax || backoff < 0 { // backoff < 0 = overflow
		backoff = p.BackoffMax
	}
	return time.Now().Add(backoff), false
}
