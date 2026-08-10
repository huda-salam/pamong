package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/huda-salam/pamong/core/config"
	"github.com/huda-salam/pamong/gateway"
	"github.com/huda-salam/pamong/infra/ratelimit"
	"github.com/huda-salam/pamong/port"
	"github.com/huda-salam/pamong/testkit"
)

// PR-W2 — pemasangan grup /admin/identity/*. Yang diuji BUKAN isi handler (itu use case identity,
// diuji di identity/usecase & identity/adapter/http) melainkan STACK MIDDLEWARE di sekelilingnya.
//
// Grup ini adalah kebalikan dari /auth/*: setiap endpointnya memutasi identitas sentral, jadi ia
// WAJIB berada di balik RequireAuth. Kesalahan yang dijaga bersifat mematikan dan senyap —
// endpoint yang keliru dipasang di top mux (meniru pola tetangganya) akan menerima permintaan
// anonim, dan pada `/admin/identity/assignments` itu berarti siapa pun di jaringan bisa menugaskan
// dirinya ke tenant mana pun. Gerbang permission di handler tak menutupnya: pada request anonim
// gateway.Context tak punya evaluator, dan tanpa evaluator RequirePermission bersifat PERMISIF.

// stubAdminRoutes mencatat handler mana yang tercapai. Memenuhi adminIdentityRoutes.
type stubAdminRoutes struct{ reached string }

func (s *stubAdminRoutes) mark(name string, w http.ResponseWriter, r *http.Request) {
	s.reached = name
	// Tulis person dari context supaya test bisa membuktikan klaim token benar-benar sampai ke
	// handler — bukan sekadar "tidak 401".
	_, _ = w.Write([]byte(gateway.FromRequest(r).PersonID().String()))
}

func (s *stubAdminRoutes) CreatePerson(w http.ResponseWriter, r *http.Request) {
	s.mark("create_person", w, r)
}
func (s *stubAdminRoutes) AttachEmployment(w http.ResponseWriter, r *http.Request) {
	s.mark("attach_employment", w, r)
}
func (s *stubAdminRoutes) CreateCredential(w http.ResponseWriter, r *http.Request) {
	s.mark("create_credential", w, r)
}
func (s *stubAdminRoutes) AssignEmploymentToTenant(w http.ResponseWriter, r *http.Request) {
	s.mark("assign_tenant", w, r)
}
func (s *stubAdminRoutes) AssignCentralRole(w http.ResponseWriter, r *http.Request) {
	s.mark("assign_central_role", w, r)
}

// buildAdminTestHandler merakit handler produksi (buildServerHandler) dengan rute admin ter-stub
// yang dipasang lewat mountAdminIdentityRoutes — fungsi pemasangan yang sama dengan run().
func buildAdminTestHandler(t *testing.T, token string, claims *port.Claims) (http.Handler, *stubAdminRoutes) {
	t.Helper()
	stub := &stubAdminRoutes{}
	router := gateway.NewRouter()
	router.Get("/healthz", healthz)
	mountAdminIdentityRoutes(router, stub)

	tokens := map[string]*port.Claims{}
	if token != "" {
		tokens[token] = claims
	}
	h := buildServerHandler(serverDeps{
		router:         router,
		verifier:       &fakeVerifier{tokens: tokens},
		evalFactory:    fakeFactory{},
		tenantResolver: fakeTenantResolver{},
		rateLimiter:    ratelimit.NewMemory(nil),
		rateLimit:      config.RateLimitConfig{Enabled: true, RPS: 100},
		logger:         testkit.NewNoopLogger(),
	})
	return h, stub
}

// adminPaths adalah kelima rute grup ini beserta nama handler yang seharusnya tercapai.
// Menambah endpoint tanpa menambah baris di sini berarti pemasangannya tak pernah diuji.
var adminPaths = []struct {
	path    string
	reached string
}{
	{"/admin/identity/persons", "create_person"},
	{"/admin/identity/employments", "attach_employment"},
	{"/admin/identity/credentials", "create_credential"},
	{"/admin/identity/assignments", "assign_tenant"},
	{"/admin/identity/central-role-assignments", "assign_central_role"},
}

// Tanpa token, SEMUA rute admin harus 401 dan handler-nya tak boleh tersentuh sama sekali.
func TestAdminIdentityRoutes_TanpaToken_401(t *testing.T) {
	for _, c := range adminPaths {
		t.Run(c.path, func(t *testing.T) {
			h, stub := buildAdminTestHandler(t, "", nil)
			w := postJSON(h, c.path, `{}`, "")
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("%s tanpa token harus 401, dapat %d — rute mutasi identity berada di LUAR "+
					"RequireAuth", c.path, w.Code)
			}
			if stub.reached != "" {
				t.Fatalf("%s: handler %q tercapai pada request anonim", c.path, stub.reached)
			}
		})
	}
}

// Dengan token sah, rute tercapai DAN klaimnya sampai ke handler — itulah yang membuat
// RequirePermission di handler punya sesuatu untuk dievaluasi, dan yang membuat `assigned_by`
// terisi aktor sebenarnya alih-alih uuid.Nil.
func TestAdminIdentityRoutes_TokenSah_TercapaiDenganKlaim(t *testing.T) {
	personID := uuid.New()
	for _, c := range adminPaths {
		t.Run(c.path, func(t *testing.T) {
			h, stub := buildAdminTestHandler(t, "tok", &port.Claims{
				PersonID: personID, Persona: "employee", TenantID: "pemkot-a",
			})
			w := postJSON(h, c.path, `{}`, "tok")
			if w.Code != http.StatusOK {
				t.Fatalf("%s dengan token sah: mau 200 (stub), dapat %d", c.path, w.Code)
			}
			if stub.reached != c.reached {
				t.Fatalf("%s: handler yang tercapai = %q, mau %q", c.path, stub.reached, c.reached)
			}
			if w.Body.String() != personID.String() {
				t.Fatalf("%s: person_id yang terlihat handler = %q, mau %q — klaim token tak sampai "+
					"ke AuthContext", c.path, w.Body.String(), personID)
			}
		})
	}
}

// Metode selain POST tidak terpasang: rute ini semata-mata pembuatan. GET yang lolos akan
// membuat mutasi bisa dipicu lewat navigasi/prefetch dan lolos proteksi CSRF berbasis metode.
func TestAdminIdentityRoutes_HanyaPOST(t *testing.T) {
	h, stub := buildAdminTestHandler(t, "tok", &port.Claims{PersonID: uuid.New(), Persona: "employee"})
	r := httptest.NewRequest(http.MethodGet, "/admin/identity/assignments", nil)
	r.Header.Set("Authorization", "Bearer tok")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if stub.reached != "" {
		t.Fatalf("GET mencapai handler %q — rute mutasi terpasang untuk metode baca", stub.reached)
	}
	if w.Code == http.StatusOK {
		t.Fatalf("GET /admin/identity/assignments menjawab 200, mau 404/405")
	}
}

// Grup admin dipasang di ROUTER BISNIS, jadi ia tak boleh membayangi rute auth: /auth/login tetap
// tercapai tanpa token. Penjaga regresi untuk pemasangan yang keliru menaruh admin di top mux
// (yang akan mendahului seluruh business chain).
func TestAdminIdentityRoutes_TidakMenggangguGrupAuth(t *testing.T) {
	stubAuth := &stubAuthRoutes{}
	router := gateway.NewRouter()
	router.Get("/healthz", healthz)
	mountAdminIdentityRoutes(router, &stubAdminRoutes{})

	h := buildServerHandler(serverDeps{
		router:         router,
		verifier:       &fakeVerifier{tokens: map[string]*port.Claims{}},
		evalFactory:    fakeFactory{},
		tenantResolver: fakeTenantResolver{},
		rateLimiter:    ratelimit.NewMemory(nil),
		rateLimit:      config.RateLimitConfig{Enabled: true, RPS: 100},
		auth:           stubAuth,
		logger:         testkit.NewNoopLogger(),
	})

	r := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code == http.StatusUnauthorized || stubAuth.reached != "login_employee" {
		t.Fatalf("/auth/login terganggu pemasangan grup admin: status %d, reached %q",
			w.Code, stubAuth.reached)
	}
}
