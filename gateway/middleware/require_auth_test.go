package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/gateway"
	"github.com/huda-salam/pamong/gateway/middleware"
	"github.com/huda-salam/pamong/port"
)

// nextOK adalah handler downstream yang menandai bahwa ia tercapai + membalas 200.
func nextOK(reached *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		*reached = true
		w.WriteHeader(http.StatusOK)
	})
}

func TestRequireAuth_Anonim_401(t *testing.T) {
	var reached bool
	h := middleware.RequireAuth()(nextOK(&reached))

	// Request tanpa gateway.Context ter-populasi (anonim) — FromRequest mengembalikan konteks
	// kosong (personID zero).
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("anonim harus 401, got %d", w.Code)
	}
	if reached {
		t.Error("handler downstream TIDAK boleh tercapai untuk request anonim")
	}
}

func TestRequireAuth_Terotentikasi_Lolos(t *testing.T) {
	var reached bool
	h := middleware.RequireAuth()(nextOK(&reached))

	c := gateway.NewContextFromClaims(context.Background(), &port.Claims{
		PersonID: uuid.New(),
		Persona:  "employee",
		TenantID: "pemkot-a",
	})
	r := gateway.WithContext(httptest.NewRequest(http.MethodGet, "/x", nil), c)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("terotentikasi harus lolos (200), got %d", w.Code)
	}
	if !reached {
		t.Error("handler downstream harus tercapai untuk request terotentikasi")
	}
}
