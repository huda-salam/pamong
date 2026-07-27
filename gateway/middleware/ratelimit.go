package middleware

import (
	"net/http"
	"time"

	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/gateway"
	"github.com/huda-salam/pamong/port"
)

// RateLimit membatasi laju request PER-PRINCIPAL (PRD gateway F3 langkah 5): kunci dirakit
// dari tenant + person sehingga satu principal yang membanjiri tak memengaruhi principal lain.
// Melewati batas → 429 (core.ErrTooManyRequests). Fail-closed: bila store limiter error,
// request DITOLAK (429) — lebih aman menolak daripada membuka pintu saat proteksi tak pasti.
//
// Dipasang SETELAH RequireAuth agar principal selalu ada (request anonymous sudah ditolak
// 401 sebelum sampai sini). Limiter di-inject sebagai port.RateLimiter (in-memory kini,
// Redis untuk multi-instance kelak — swap di wiring, titik ekstensi #1).
//
// window fixed (mis. 1 detik) dengan limit = RPS. Burst (token-bucket) belum dimodelkan
// fixed-window — DEFERRED(Phase-5.1.x) saat pindah ke token-bucket/Redis.
func RateLimit(limiter port.RateLimiter, limit int, window time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c := gateway.FromRequest(r)
			allowed, err := limiter.Allow(r.Context(), rateLimitKey(c), limit, window)
			if err != nil {
				gateway.WriteError(w, core.ErrTooManyRequests("pembatasan laju tidak tersedia"))
				return
			}
			if !allowed {
				gateway.WriteError(w, core.ErrTooManyRequests("terlalu banyak permintaan"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// rateLimitKey merakit kunci per-principal. Person (uuid) selalu ada di jalur ini (RequireAuth
// mendahului); tenant kosong untuk citizen — tetap unik per person. Prefix "rl:req:" memisahkan
// namespace dari pemakaian limiter lain (mis. OTP) yang berbagi store.
func rateLimitKey(c *gateway.Context) string {
	return "rl:req:" + c.TenantID() + ":" + c.PersonID().String()
}
