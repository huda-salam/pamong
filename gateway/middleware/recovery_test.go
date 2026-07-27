package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/huda-salam/pamong/gateway/middleware"
	"github.com/huda-salam/pamong/testkit"
)

func TestRecovery_Panic_500(t *testing.T) {
	h := middleware.Recovery(testkit.NewNoopLogger())(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		panic("boom")
	}))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("panic harus jadi 500, got %d", w.Code)
	}
}

func TestRecovery_TanpaPanic_Diteruskan(t *testing.T) {
	h := middleware.Recovery(testkit.NewNoopLogger())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))

	if w.Code != http.StatusCreated {
		t.Fatalf("tanpa panic harus lolos apa adanya (201), got %d", w.Code)
	}
}

// TestRecovery_PanicSetelahWrite_TakDoubleHeader memastikan panic yang terjadi SETELAH handler
// menulis status tidak memaksa WriteHeader kedua (yang menghasilkan warning superfluous).
func TestRecovery_PanicSetelahWrite_TakDoubleHeader(t *testing.T) {
	h := middleware.Recovery(testkit.NewNoopLogger())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("partial"))
		panic("mid-write")
	}))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))

	// Status tetap yang di-commit handler (202), bukan ditimpa 500.
	if w.Code != http.StatusAccepted {
		t.Fatalf("status harus tetap 202 (sudah commit), got %d", w.Code)
	}
}
