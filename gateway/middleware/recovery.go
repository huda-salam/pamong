package middleware

import (
	"fmt"
	"net/http"

	"github.com/huda-salam/pamong/port"
)

// Recovery adalah middleware terluar (PRD gateway F3 langkah 1): panic pada satu request
// menjadi respons 500 anggun + log, bukan koneksi ter-reset (HTTP 000) yang membingungkan
// klien. Dipasang paling luar agar menangkap panic dari SELURUH middleware lain dan handler.
//
// Dipindah dari cmd/server (PR-5.1.1) ke sini di PR-5.1.2 agar menjadi bagian resmi stack
// middleware yang urut. Ini pengaman crash tingkat server — TERPISAH dari middleware keamanan
// (auth/tenant/ratelimit).
func Recovery(logger port.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rw := &recorder{ResponseWriter: w}
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error(r.Context(), "panic saat melayani request",
						port.F("panic", fmt.Sprint(rec)),
						port.F("method", r.Method),
						port.F("path", r.URL.Path))
					// Hanya tulis 500 bila belum ada status/body terkirim; kalau handler sudah
					// menulis lalu panic di tengah, status sudah commit — memaksa WriteHeader(500)
					// hanya menghasilkan warning "superfluous" + body korup. Yang bisa dilakukan
					// hanyalah menghentikan penulisan (respons truncated, tapi ter-log).
					if !rw.wrote {
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusInternalServerError)
						_, _ = w.Write([]byte(`{"error":"internal server error"}`))
					}
				}
			}()
			next.ServeHTTP(rw, r)
		})
	}
}

// recorder membungkus http.ResponseWriter untuk menandai apakah status/body sudah terkirim,
// agar Recovery tak menulis header ganda setelah handler menulis sebagian lalu panic.
type recorder struct {
	http.ResponseWriter
	wrote bool
}

func (r *recorder) WriteHeader(code int) {
	r.wrote = true
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(b []byte) (int, error) {
	r.wrote = true // Write implisit meng-commit 200 bila WriteHeader belum dipanggil
	return r.ResponseWriter.Write(b)
}
