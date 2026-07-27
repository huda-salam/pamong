package middleware

import (
	"context"
	"net/http"

	"github.com/google/uuid"
)

// requestIDHeader adalah nama header korelasi standar (masuk & keluar).
const requestIDHeader = "X-Request-ID"

type requestIDKey struct{}

// RequestID memberi tiap request satu correlation id (PRD gateway F3 langkah 2): dipakai
// klien yang mengirim X-Request-ID (dihormati), atau di-generate bila absen. Id disematkan ke
// context (untuk log/trace downstream lewat RequestIDFrom) dan di-echo ke response header agar
// klien bisa merujuknya saat melapor masalah.
//
// Ini seam korelasi ringan; tracing OTEL penuh (span/propagation W3C) adalah ranah
// infra/observability dan di-wire terpisah — DEFERRED(Phase-5.1.x).
func RequestID() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get(requestIDHeader)
			if id == "" {
				id = uuid.NewString()
			}
			w.Header().Set(requestIDHeader, id)
			ctx := context.WithValue(r.Context(), requestIDKey{}, id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequestIDFrom mengembalikan correlation id yang disematkan RequestID, atau "" bila tak ada.
func RequestIDFrom(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDKey{}).(string); ok {
		return v
	}
	return ""
}
