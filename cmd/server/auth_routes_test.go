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

// PR-W1 — pemasangan grup /auth/*. Yang diuji di sini BUKAN isi handler (itu use case identity,
// diuji di identity/usecase) melainkan STACK MIDDLEWARE di sekelilingnya: rute mana yang lolos
// RequireAuth dan mana yang tidak.
//
// Kesalahan yang dijaga bersifat mematikan dan senyap: alur login yang terpasang di dalam
// business chain menuntut token untuk MEMPEROLEH token, jadi server boot bersih, /healthz hijau,
// dan tak ada satu pun klien yang bisa masuk. Persis jenis cacat yang hanya terlihat saat dirakit
// — alasan DoD 11 ada.

// stubAuthRoutes mencatat handler mana yang tercapai. Memenuhi authRoutes.
type stubAuthRoutes struct{ reached string }

func (s *stubAuthRoutes) mark(name string, w http.ResponseWriter, r *http.Request) {
	s.reached = name
	// Tulis person dari context supaya test bisa membuktikan klaim token benar-benar sampai ke
	// handler pada rute yang ber-auth (bukan sekadar "tidak 401").
	_, _ = w.Write([]byte(gateway.FromRequest(r).PersonID().String()))
}

func (s *stubAuthRoutes) LoginEmployee(w http.ResponseWriter, r *http.Request) {
	s.mark("login_employee", w, r)
}
func (s *stubAuthRoutes) SelectTenant(w http.ResponseWriter, r *http.Request) {
	s.mark("select_tenant", w, r)
}
func (s *stubAuthRoutes) LoginCitizen(w http.ResponseWriter, r *http.Request) {
	s.mark("login_citizen", w, r)
}
func (s *stubAuthRoutes) RequestOTP(w http.ResponseWriter, r *http.Request) {
	s.mark("request_otp", w, r)
}
func (s *stubAuthRoutes) VerifyOTP(w http.ResponseWriter, r *http.Request) {
	s.mark("verify_otp", w, r)
}

// buildAuthTestHandler merakit handler produksi (buildServerHandler) dengan rute auth ter-stub.
func buildAuthTestHandler(t *testing.T, token string, claims *port.Claims) (http.Handler, *stubAuthRoutes) {
	t.Helper()
	stub := &stubAuthRoutes{}
	router := gateway.NewRouter()
	router.Get("/echo", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	router.Get("/healthz", healthz)

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
		auth:           stub,
		logger:         testkit.NewNoopLogger(),
	})
	return h, stub
}

func postJSON(h http.Handler, path, body, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// Keempat rute pra-otentikasi HARUS tercapai tanpa token. Bila salah satu jatuh ke dalam business
// chain, RequireAuth menjawab 401 dan pintu masuk sistem tertutup rapat.
func TestAuthRoutes_RutePublik_TercapaiTanpaToken(t *testing.T) {
	cases := []struct {
		path    string
		reached string
	}{
		{"/auth/login", "login_employee"},
		{"/auth/public/login", "login_citizen"},
		{"/auth/public/otp/request", "request_otp"},
		{"/auth/public/otp/verify", "verify_otp"},
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			h, stub := buildAuthTestHandler(t, "", nil)
			w := postJSON(h, c.path, `{}`, "")
			if w.Code == http.StatusUnauthorized {
				t.Fatalf("%s menjawab 401 tanpa token — rute login terpasang di balik RequireAuth, "+
					"artinya klien butuh token untuk memperoleh token", c.path)
			}
			if stub.reached != c.reached {
				t.Fatalf("%s: handler yang tercapai = %q, mau %q", c.path, stub.reached, c.reached)
			}
		})
	}
}

// /auth/select-tenant adalah kebalikannya: ia menukar token SEMENTARA menjadi token final, jadi
// tanpa token ia HARUS 401. Kalau tidak, siapa pun bisa meminta token ber-tenant tanpa pernah
// membuktikan siapa dirinya — use case mengambil person_id dari AuthContext, yang pada request
// anonim adalah uuid.Nil.
func TestAuthRoutes_SelectTenant_TanpaToken_401(t *testing.T) {
	h, stub := buildAuthTestHandler(t, "", nil)
	w := postJSON(h, "/auth/select-tenant", `{"tenant_id":"pemkot-a"}`, "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("/auth/select-tenant tanpa token harus 401, dapat %d", w.Code)
	}
	if stub.reached != "" {
		t.Fatalf("handler tercapai (%q) padahal request anonim — RequireAuth tak terpasang di rute ini",
			stub.reached)
	}
}

// Dengan token sementara yang sah, select-tenant lolos DAN klaimnya sampai ke handler (person_id
// terisi). Ini yang membuat "person dari klaim tersigning, bukan dari body" benar-benar berlaku.
func TestAuthRoutes_SelectTenant_TokenSementara_LolosDenganKlaim(t *testing.T) {
	personID := uuid.New()
	h, stub := buildAuthTestHandler(t, "temp", &port.Claims{PersonID: personID, Persona: "employee"})

	w := postJSON(h, "/auth/select-tenant", `{"tenant_id":"pemkot-a"}`, "temp")
	if w.Code != http.StatusOK {
		t.Fatalf("select-tenant dengan token sementara: mau 200, dapat %d", w.Code)
	}
	if stub.reached != "select_tenant" {
		t.Fatalf("handler yang tercapai = %q", stub.reached)
	}
	if w.Body.String() != personID.String() {
		t.Fatalf("person_id yang terlihat handler = %q, mau %q — klaim token tak sampai ke AuthContext",
			w.Body.String(), personID)
	}
}

// Rute bisnis TIDAK ikut terbuka gara-gara grup auth dipasang di top mux. Penjaga regresi untuk
// kesalahan sebaliknya: memindahkan terlalu banyak keluar dari business chain.
func TestAuthRoutes_RuteBisnisTetapTertutup(t *testing.T) {
	h, _ := buildAuthTestHandler(t, "", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/echo", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("rute bisnis tanpa token harus tetap 401, dapat %d", w.Code)
	}
}

// Tanpa handler auth (auth == nil) rute /auth/* tak terpasang sama sekali → jatuh ke business
// chain → 401 untuk anonim. Menjaga cabang nil di buildServerHandler tetap benar (dipakai test
// lama yang tak merakit alur auth).
func TestAuthRoutes_TanpaHandler_TidakTerpasang(t *testing.T) {
	h := buildTestHandler(t, "", nil, nil)
	w := postJSON(h, "/auth/login", `{}`, "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("tanpa handler auth, /auth/login harus jatuh ke business chain (401), dapat %d", w.Code)
	}
}
