package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/huda-salam/pamong/gateway/middleware"
)

func TestCORS_OriginDiizinkan_SetHeader(t *testing.T) {
	var reached bool
	h := middleware.CORS([]string{"https://ui.gov.example"})(nextOK(&reached))

	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.Header.Set("Origin", "https://ui.gov.example")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://ui.gov.example" {
		t.Fatalf("ACAO = %q, mau echo origin diizinkan", got)
	}
	if !reached {
		t.Error("request non-preflight harus diteruskan ke handler")
	}
}

func TestCORS_OriginTidakDiizinkan_TanpaHeader(t *testing.T) {
	var reached bool
	h := middleware.CORS([]string{"https://ui.gov.example"})(nextOK(&reached))

	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.Header.Set("Origin", "https://jahat.example")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("origin tak diizinkan tak boleh dapat ACAO, got %q", got)
	}
	if !reached {
		t.Error("request tetap diteruskan; browser yang memblok, bukan server")
	}
}

func TestCORS_Preflight_204_TanpaHandler(t *testing.T) {
	var reached bool
	h := middleware.CORS([]string{"https://ui.gov.example"})(nextOK(&reached))

	r := httptest.NewRequest(http.MethodOptions, "/x", nil)
	r.Header.Set("Origin", "https://ui.gov.example")
	r.Header.Set("Access-Control-Request-Method", "POST")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusNoContent {
		t.Fatalf("preflight harus 204, got %d", w.Code)
	}
	if reached {
		t.Error("preflight tak boleh diteruskan ke handler bisnis")
	}
	if w.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Error("preflight harus menyertakan Access-Control-Allow-Methods")
	}
}

func TestCORS_AllowlistKosong_SameOriginOnly(t *testing.T) {
	var reached bool
	h := middleware.CORS(nil)(nextOK(&reached))

	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.Header.Set("Origin", "https://ui.gov.example")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("allowlist kosong: tak ada origin cross-site diizinkan, got %q", got)
	}
	if !reached {
		t.Error("request non-preflight tetap diteruskan")
	}
}
