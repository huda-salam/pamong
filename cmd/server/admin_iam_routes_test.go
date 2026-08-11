package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/huda-salam/pamong/core/config"
	"github.com/huda-salam/pamong/gateway"
	"github.com/huda-salam/pamong/infra/ratelimit"
	"github.com/huda-salam/pamong/port"
	"github.com/huda-salam/pamong/testkit"
)

// PR-W3b — pemasangan grup /admin/iam/*. Seperti padanannya untuk /admin/identity/*, yang diuji
// BUKAN isi handler melainkan STACK MIDDLEWARE di sekelilingnya.
//
// Kekeliruan yang dijaga sama mematikannya: rute yang keliru dipasang di top mux (meniru /auth/*)
// akan menerima permintaan anonim — dan pada `/admin/iam/tenant-role-assignments` itu berarti siapa
// pun di jaringan bisa menugaskan role apa pun kepada siapa pun. Gerbang permission di handler TAK
// menutupnya: pada request anonim gateway.Context tak punya evaluator, dan tanpa evaluator
// RequirePermission bersifat PERMISIF.

// stubIAMRoutes mencatat handler mana yang tercapai. Memenuhi adminIAMRoutes.
type stubIAMRoutes struct{ reached string }

func (s *stubIAMRoutes) mark(name string, w http.ResponseWriter, r *http.Request) {
	s.reached = name
	_, _ = w.Write([]byte(gateway.FromRequest(r).PersonID().String()))
}

func (s *stubIAMRoutes) CreateTenantRole(w http.ResponseWriter, r *http.Request) {
	s.mark("create_tenant_role", w, r)
}
func (s *stubIAMRoutes) AssignTenantRole(w http.ResponseWriter, r *http.Request) {
	s.mark("assign_tenant_role", w, r)
}
func (s *stubIAMRoutes) CreateDelegation(w http.ResponseWriter, r *http.Request) {
	s.mark("create_delegation", w, r)
}

func buildIAMTestHandler(t *testing.T, token string, claims *port.Claims) (http.Handler, *stubIAMRoutes) {
	t.Helper()
	stub := &stubIAMRoutes{}
	router := gateway.NewRouter()
	router.Get("/healthz", healthz)
	mountAdminIAMRoutes(router, stub)

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

// iamPaths adalah seluruh rute grup ini. Menambah endpoint tanpa menambah baris di sini berarti
// pemasangannya tak pernah diuji.
var iamPaths = []struct {
	path    string
	reached string
}{
	{"/admin/iam/tenant-roles", "create_tenant_role"},
	{"/admin/iam/tenant-role-assignments", "assign_tenant_role"},
	{"/admin/iam/delegations", "create_delegation"},
}

func TestAdminIAMRoutes_TanpaToken_401(t *testing.T) {
	for _, c := range iamPaths {
		t.Run(c.path, func(t *testing.T) {
			h, stub := buildIAMTestHandler(t, "", nil)
			w := postJSON(h, c.path, `{}`, "")
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("%s tanpa token harus 401, dapat %d — rute mutasi wewenang berada di LUAR "+
					"RequireAuth", c.path, w.Code)
			}
			if stub.reached != "" {
				t.Fatalf("%s: handler %q tercapai pada request anonim", c.path, stub.reached)
			}
		})
	}
}

func TestAdminIAMRoutes_TokenSah_TercapaiDenganKlaim(t *testing.T) {
	personID := uuid.New()
	for _, c := range iamPaths {
		t.Run(c.path, func(t *testing.T) {
			h, stub := buildIAMTestHandler(t, "tok", &port.Claims{
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
					"ke AuthContext (assigned_by akan terisi uuid.Nil)", c.path, w.Body.String(), personID)
			}
		})
	}
}

// Metode selain POST tidak terpasang: seluruh grup ini pembuatan. GET yang lolos membuat mutasi
// wewenang bisa dipicu lewat navigasi/prefetch dan lolos proteksi CSRF berbasis metode.
func TestAdminIAMRoutes_HanyaPOST(t *testing.T) {
	h, stub := buildIAMTestHandler(t, "tok", &port.Claims{PersonID: uuid.New(), Persona: "employee"})
	r := httptest.NewRequest(http.MethodGet, "/admin/iam/tenant-role-assignments", nil)
	r.Header.Set("Authorization", "Bearer tok")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if stub.reached != "" {
		t.Fatalf("GET mencapai handler %q — rute mutasi terpasang untuk metode baca", stub.reached)
	}
	if w.Code == http.StatusOK {
		t.Fatalf("GET /admin/iam/tenant-role-assignments menjawab 200, mau 404/405")
	}
}
