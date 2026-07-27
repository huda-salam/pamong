package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/huda-salam/pamong/gateway/middleware"
)

func TestRequestID_GenerateBilaAbsen(t *testing.T) {
	var seen string
	h := middleware.RequestID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = middleware.RequestIDFrom(r.Context())
	}))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))

	if seen == "" {
		t.Fatal("request id harus di-generate saat header absen")
	}
	if got := w.Header().Get("X-Request-ID"); got != seen {
		t.Fatalf("header echo = %q, mau sama dengan context id %q", got, seen)
	}
}

func TestRequestID_HormatiHeaderMasuk(t *testing.T) {
	const incoming = "req-abc-123"
	var seen string
	h := middleware.RequestID()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = middleware.RequestIDFrom(r.Context())
	}))

	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.Header.Set("X-Request-ID", incoming)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if seen != incoming {
		t.Fatalf("id = %q, mau hormati header masuk %q", seen, incoming)
	}
	if got := w.Header().Get("X-Request-ID"); got != incoming {
		t.Fatalf("header echo = %q, mau %q", got, incoming)
	}
}
