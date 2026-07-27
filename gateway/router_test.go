package gateway_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/huda-salam/pamong/gateway"
)

func TestRouter_DispatchByMethodDanPath(t *testing.T) {
	r := gateway.NewRouter()
	r.Post("/surat-masuk", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("created"))
	})
	r.Get("/surat-masuk", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("listed"))
	})

	cases := []struct {
		method, path string
		wantCode     int
		wantBody     string
	}{
		{http.MethodPost, "/surat-masuk", http.StatusCreated, "created"},
		{http.MethodGet, "/surat-masuk", http.StatusOK, "listed"},
	}
	for _, c := range cases {
		req := httptest.NewRequest(c.method, c.path, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != c.wantCode {
			t.Errorf("%s %s: code = %d, mau %d", c.method, c.path, rec.Code, c.wantCode)
		}
		if body := rec.Body.String(); body != c.wantBody {
			t.Errorf("%s %s: body = %q, mau %q", c.method, c.path, body, c.wantBody)
		}
	}
}

func TestRouter_MethodSalah_405(t *testing.T) {
	r := gateway.NewRouter()
	r.Get("/x", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	req := httptest.NewRequest(http.MethodDelete, "/x", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	// ServeMux method-aware (Go 1.22+) mengembalikan 405 untuk path dikenal, method salah.
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("DELETE /x (hanya GET terdaftar): code = %d, mau 405", rec.Code)
	}
}

func TestRouter_PathTakDikenal_404(t *testing.T) {
	r := gateway.NewRouter()
	r.Get("/x", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/tak-ada", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /tak-ada: code = %d, mau 404", rec.Code)
	}
}
