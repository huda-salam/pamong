package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

func (fakeFactory) Build(context.Context, *port.Claims) (port.PermissionEvaluator, port.ScopedEvaluator, error) {
	// Scoped nil = default permisif di gateway.Context — cukup untuk test stack yang tak menguji
	// ABAC; penegakannya diuji terpisah (scoped_evaluator_test.go + DoD e2e).
	return permission.NewEngine(permission.NewMemoryCatalog()), nil, nil
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

// fakeIdemStore adalah port.IdempotencyStore in-memory minimal untuk membuktikan middleware
// idempotency terpasang di chain dan menerima principal+tenant (bukan konteks anonim).
type fakeIdemRec struct {
	fingerprint string
	status      int
	body        []byte
	completed   bool
}

type fakeIdemStore struct {
	mu sync.Mutex
	m  map[string]*fakeIdemRec
}

func newFakeIdemStore() *fakeIdemStore {
	return &fakeIdemStore{m: make(map[string]*fakeIdemRec)}
}

func (s *fakeIdemStore) Reserve(_ context.Context, tenant string, person uuid.UUID, key, fp string) (*port.IdempotencyRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	kk := tenant + "|" + person.String() + "|" + key
	if r, ok := s.m[kk]; ok {
		return &port.IdempotencyRecord{Fingerprint: r.fingerprint, Status: r.status, Body: r.body, Completed: r.completed}, false, nil
	}
	s.m[kk] = &fakeIdemRec{fingerprint: fp}
	return nil, true, nil
}

func (s *fakeIdemStore) Complete(_ context.Context, tenant string, person uuid.UUID, key string, status int, body []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.m[tenant+"|"+person.String()+"|"+key]; ok {
		r.status, r.body, r.completed = status, append([]byte(nil), body...), true
	}
	return nil
}

func (s *fakeIdemStore) Release(_ context.Context, tenant string, person uuid.UUID, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, tenant+"|"+person.String()+"|"+key)
	return nil
}

// TestServerHandler_Idempotency_TerpasangDiChain membuktikan idempotency middleware aktif di
// stack dan berjalan SETELAH Auth+TenantResolver (ia butuh principal+tenant): dua POST identik
// ber-Idempotency-Key hanya menjalankan handler sekali, request kedua di-replay.
func TestServerHandler_Idempotency_TerpasangDiChain(t *testing.T) {
	var runs int
	router := gateway.NewRouter()
	router.Post("/surat", func(w http.ResponseWriter, _ *http.Request) {
		runs++
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	router.Get("/healthz", healthz)

	claims := &port.Claims{PersonID: uuid.New(), Persona: "employee", TenantID: "pemkot-a"}
	h := buildServerHandler(serverDeps{
		router:         router,
		verifier:       &fakeVerifier{tokens: map[string]*port.Claims{"tok": claims}},
		evalFactory:    fakeFactory{},
		tenantResolver: fakeTenantResolver{},
		rateLimiter:    ratelimit.NewMemory(nil),
		rateLimit:      config.RateLimitConfig{Enabled: true, RPS: 100},
		idempotency:    newFakeIdemStore(),
		logger:         testkit.NewNoopLogger(),
	})

	do := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/surat", strings.NewReader(`{"perihal":"x"}`))
		r.Header.Set("Authorization", "Bearer tok")
		r.Header.Set("Idempotency-Key", "key-1")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}

	w1 := do()
	if w1.Code != http.StatusCreated {
		t.Fatalf("POST#1 = %d, mau 201", w1.Code)
	}
	w2 := do()
	if w2.Code != http.StatusCreated || w2.Header().Get("Idempotent-Replay") != "true" {
		t.Fatalf("POST#2 harus replay 201 dgn header Idempotent-Replay; got %d replay=%q", w2.Code, w2.Header().Get("Idempotent-Replay"))
	}
	if runs != 1 {
		t.Fatalf("handler harus jalan sekali (request kedua di-replay); runs=%d", runs)
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
