package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/core/config"
	"github.com/huda-salam/pamong/core/permission"
	"github.com/huda-salam/pamong/gateway"
	"github.com/huda-salam/pamong/infra/ratelimit"
	"github.com/huda-salam/pamong/port"
	"github.com/huda-salam/pamong/testkit"
)

// --- fakes untuk merakit handler tanpa DB ---

type fakeVerifier struct{ tokens map[string]*port.Claims }

func (f *fakeVerifier) Verify(_ context.Context, raw string) (*port.Claims, error) {
	if c, ok := f.tokens[raw]; ok {
		return c, nil
	}
	return nil, core.ErrUnauthorized("token tidak valid")
}

type fakeFactory struct{}

func (fakeFactory) Build(context.Context, *port.Claims) (port.PermissionEvaluator, error) {
	return permission.NewEngine(permission.NewMemoryCatalog()), nil
}

type fakeTenantResolver struct{}

func (fakeTenantResolver) Resolve(_ context.Context, id string) (*port.TenantInfo, error) {
	return &port.TenantInfo{TenantID: id, IsActive: true}, nil
}

// buildTestHandler merakit handler produksi dengan fake + satu rute bisnis /echo yang
// mengembalikan tenant terlihat handler (untuk bukti injeksi WithTenant).
func buildTestHandler(t *testing.T, token string, claims *port.Claims, tenantSeen *string) http.Handler {
	t.Helper()
	router := gateway.NewRouter()
	router.Get("/echo", func(w http.ResponseWriter, r *http.Request) {
		if tenantSeen != nil {
			*tenantSeen = port.TenantFrom(gateway.FromRequest(r))
		}
		w.WriteHeader(http.StatusOK)
	})
	router.Get("/healthz", healthz) // tripwire (dibayangi top mux)

	tokens := map[string]*port.Claims{}
	if token != "" {
		tokens[token] = claims
	}
	return buildServerHandler(serverDeps{
		router:         router,
		verifier:       &fakeVerifier{tokens: tokens},
		evalFactory:    fakeFactory{},
		tenantResolver: fakeTenantResolver{},
		rateLimiter:    ratelimit.NewMemory(nil),
		rateLimit:      config.RateLimitConfig{Enabled: true, RPS: 100},
		logger:         testkit.NewNoopLogger(),
	})
}

func TestServerHandler_Healthz_AuthFree(t *testing.T) {
	h := buildTestHandler(t, "", nil, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("/healthz harus 200 tanpa auth, got %d", w.Code)
	}
}

func TestServerHandler_RuteBisnis_TanpaToken_401(t *testing.T) {
	h := buildTestHandler(t, "", nil, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/echo", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("rute bisnis tanpa token harus 401 (DoD: request tanpa auth ditolak), got %d", w.Code)
	}
}

func TestServerHandler_RuteBisnis_TokenInvalid_401(t *testing.T) {
	h := buildTestHandler(t, "valid", &port.Claims{PersonID: uuid.New()}, nil)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/echo", nil)
	r.Header.Set("Authorization", "Bearer salah")
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("token invalid harus 401, got %d", w.Code)
	}
}

// TestServerHandler_RateLimit_PerPrincipal mengunci urutan middleware yang benar: RateLimit
// harus berjalan SETELAH Auth agar membaca principal dari gateway.Context dan mem-bucket
// per-principal. Bila RateLimit salah ditempatkan sebelum Auth (membungkus seluruh chain),
// FromRequest mengembalikan konteks anonim → semua principal berbagi satu bucket nil, dan
// request user B akan ikut 429 setelah user A habis (test ini gagal).
func TestServerHandler_RateLimit_PerPrincipal(t *testing.T) {
	router := gateway.NewRouter()
	router.Get("/echo", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	router.Get("/healthz", healthz)

	userA := &port.Claims{PersonID: uuid.New(), Persona: "employee", TenantID: "pemkot-a"}
	userB := &port.Claims{PersonID: uuid.New(), Persona: "employee", TenantID: "pemkot-a"}
	h := buildServerHandler(serverDeps{
		router:         router,
		verifier:       &fakeVerifier{tokens: map[string]*port.Claims{"tokA": userA, "tokB": userB}},
		evalFactory:    fakeFactory{},
		tenantResolver: fakeTenantResolver{},
		rateLimiter:    ratelimit.NewMemory(nil),
		rateLimit:      config.RateLimitConfig{Enabled: true, RPS: 1},
		logger:         testkit.NewNoopLogger(),
	})

	do := func(token string) int {
		r := httptest.NewRequest(http.MethodGet, "/echo", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}

	// User A: request pertama lolos, kedua 429 (bucket A habis, RPS=1).
	if c := do("tokA"); c != http.StatusOK {
		t.Fatalf("A#1 = %d, mau 200", c)
	}
	if c := do("tokA"); c != http.StatusTooManyRequests {
		t.Fatalf("A#2 = %d, mau 429 (bucket A habis)", c)
	}
	// User B: bucket TERPISAH — tetap lolos meski bucket A sudah habis.
	if c := do("tokB"); c != http.StatusOK {
		t.Fatalf("B#1 = %d, mau 200 (bucket B independen dari A)", c)
	}
}

func TestServerHandler_RuteBisnis_TokenValid_LolosDanTenantTerinject(t *testing.T) {
	var tenantSeen string
	claims := &port.Claims{PersonID: uuid.New(), Persona: "employee", TenantID: "pemkot-a"}
	h := buildTestHandler(t, "tok", claims, &tenantSeen)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/echo", nil)
	r.Header.Set("Authorization", "Bearer tok")
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("token valid harus lolos (200), got %d", w.Code)
	}
	// Bukti #2: TenantResolver menyuntik WithTenant → port.TenantFrom di handler mengembalikannya.
	if tenantSeen != "pemkot-a" {
		t.Fatalf("tenant terlihat handler = %q, mau pemkot-a (WithTenant ter-inject)", tenantSeen)
	}
}
