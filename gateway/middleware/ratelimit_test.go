package middleware_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/gateway"
	"github.com/huda-salam/pamong/gateway/middleware"
	"github.com/huda-salam/pamong/port"
)

// fakeLimiter mengimplementasi port.RateLimiter dengan perilaku dapat dikontrol test.
type fakeLimiter struct {
	allow   bool
	err     error
	calls   int
	lastKey string
}

func (f *fakeLimiter) Allow(_ context.Context, key string, _ int, _ time.Duration) (bool, error) {
	f.calls++
	f.lastKey = key
	return f.allow, f.err
}

func authedRequest(t *testing.T, tenantID string) *http.Request {
	t.Helper()
	c := gateway.NewContextFromClaims(context.Background(), &port.Claims{
		PersonID: uuid.New(),
		Persona:  "employee",
		TenantID: tenantID,
	})
	return gateway.WithContext(httptest.NewRequest(http.MethodGet, "/x", nil), c)
}

func TestRateLimit_DalamBatas_Lolos(t *testing.T) {
	lim := &fakeLimiter{allow: true}
	var reached bool
	h := middleware.RateLimit(lim, 100, time.Second)(nextOK(&reached))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, authedRequest(t, "pemkot-a"))

	if w.Code != http.StatusOK || !reached {
		t.Fatalf("dalam batas harus lolos 200, got %d reached=%v", w.Code, reached)
	}
	if lim.lastKey == "" {
		t.Error("limiter harus dipanggil dengan key per-principal")
	}
}

func TestRateLimit_LewatBatas_429(t *testing.T) {
	lim := &fakeLimiter{allow: false}
	var reached bool
	h := middleware.RateLimit(lim, 1, time.Second)(nextOK(&reached))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, authedRequest(t, "pemkot-a"))

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("lewat batas harus 429, got %d", w.Code)
	}
	if reached {
		t.Error("handler tak boleh tercapai saat 429")
	}
}

func TestRateLimit_StoreError_FailClosed_429(t *testing.T) {
	lim := &fakeLimiter{allow: true, err: errors.New("store down")}
	var reached bool
	h := middleware.RateLimit(lim, 100, time.Second)(nextOK(&reached))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, authedRequest(t, "pemkot-a"))

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("error store harus fail-closed 429, got %d", w.Code)
	}
	if reached {
		t.Error("fail-closed: handler tak boleh tercapai saat store error")
	}
}
