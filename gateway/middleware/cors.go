package middleware

import (
	"net/http"
	"strings"
)

// CORS menerapkan kebijakan Cross-Origin Resource Sharing berbasis ALLOWLIST (PRD gateway F3
// langkah 11). Origin yang diizinkan dinyatakan eksplisit; permintaan dari origin di luar
// daftar tidak mendapat header CORS (browser memblokirnya). Default aman: allowlist kosong →
// tak ada origin cross-site diizinkan (same-origin only) — pilihan tepat untuk konteks
// pemerintahan (tak ada "*" implisit).
//
// Ditempatkan dekat lapisan terluar saat perakitan (setelah Recovery) agar preflight OPTIONS
// dijawab TANPA menyentuh auth/tenant — browser mengirim preflight tanpa kredensial aplikasi.
func CORS(allowedOrigins []string) func(http.Handler) http.Handler {
	allowed := make(map[string]bool, len(allowedOrigins))
	for _, o := range allowedOrigins {
		if o != "" {
			allowed[o] = true
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" {
				// Vary: Origin disetel untuk SETIAP request ber-Origin (baik diizinkan maupun
				// tidak) — respons bergantung pada Origin (ada/tidaknya ACAO), jadi cache bersama
				// (CDN/proxy) harus mem-varian-kan per-origin; kalau hanya diset saat diizinkan,
				// respons tanpa-ACAO bisa ter-cache lalu disajikan ke origin yang diizinkan.
				h := w.Header()
				h.Add("Vary", "Origin")
				if allowed[origin] {
					h.Set("Access-Control-Allow-Origin", origin)
					h.Set("Access-Control-Allow-Credentials", "true")
					if r.Method == http.MethodOptions {
						h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
						reqHeaders := r.Header.Get("Access-Control-Request-Headers")
						if reqHeaders == "" {
							reqHeaders = "Authorization, Content-Type, X-Request-ID"
						}
						h.Set("Access-Control-Allow-Headers", reqHeaders)
						h.Set("Access-Control-Max-Age", "600")
					}
				}
			}
			// Preflight berhenti di sini: jawab tanpa meneruskan ke handler bisnis.
			if r.Method == http.MethodOptions && strings.TrimSpace(r.Header.Get("Access-Control-Request-Method")) != "" {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
