package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/core/permission"
	"github.com/huda-salam/pamong/gateway"
	"github.com/huda-salam/pamong/gateway/middleware"
	"github.com/huda-salam/pamong/port"
)

// ---------------------------------------------------------------------------
// Fake helpers
// ---------------------------------------------------------------------------

// fakeVerifier mengimplementasi port.TokenVerifier in-memory untuk unit test.
type fakeVerifier struct {
	tokens map[string]*port.Claims // raw → Claims
}

func (f *fakeVerifier) Verify(_ context.Context, raw string) (*port.Claims, error) {
	if c, ok := f.tokens[raw]; ok {
		return c, nil
	}
	return nil, core.ErrUnauthorized("token tidak valid atau kedaluwarsa")
}

// fakeFactory mengimplementasi middleware.EvaluatorFactory in-memory.
// Mengembalikan Engine berbasis MemoryCatalog yang sudah dikonfigurasi.
type fakeFactory struct {
	eval port.PermissionEvaluator // evaluator yang selalu dikembalikan
}

func (f *fakeFactory) Build(_ context.Context, _ *port.Claims) (port.PermissionEvaluator, error) {
	return f.eval, nil
}

// captureAuthHandler menangkap gateway.Context yang dilihat handler downstream.
type captureResult struct {
	personID      uuid.UUID
	persona       string
	tenantID      string
	isCitizen     bool
	isCrossTenant bool
	hasRoleAdmin  bool
	hasCentralSA  bool
	permErr       error // hasil RequirePermission("test:perm:baca")
}

func captureAuthHandler(res *captureResult, perm string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := gateway.FromRequest(r)
		res.personID = c.PersonID()
		res.persona = c.Persona()
		res.tenantID = c.TenantID()
		res.isCitizen = c.IsCitizen()
		res.isCrossTenant = c.IsCrossTenant()
		res.hasRoleAdmin = c.HasRole("admin_opd")
		res.hasCentralSA = c.HasCentralRole("super_admin")
		res.permErr = c.RequirePermission(perm)
		w.WriteHeader(http.StatusOK)
	})
}

func doAuthRequest(t *testing.T, h http.Handler, token string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// ---------------------------------------------------------------------------
// buildEngine membangun Engine sederhana via MemoryCatalog untuk skenario test.
// Role "admin_opd" → permission "test:data:buat" + "test:data:baca".
// Role "super_admin" → LayerGlobal + permission "test:data:baca".
// ---------------------------------------------------------------------------
func buildEngine() *permission.Engine {
	cat := permission.NewMemoryCatalog().
		Define("admin_opd", permission.LayerTenant, "test:data:buat", "test:data:baca").
		Define("super_admin", permission.LayerGlobal, "test:data:baca")
	return permission.NewEngine(cat)
}

// ---------------------------------------------------------------------------
// Skenario
// ---------------------------------------------------------------------------

func TestAuth_TokenValid_ContextTerPopulasi(t *testing.T) {
	personID := uuid.New()
	claims := &port.Claims{
		PersonID:         personID,
		Persona:          "employee",
		EmploymentStatus: "asn",
		TenantID:         "pemkot-a",
		TenantRoles:      []string{"admin_opd"},
		CentralRoles:     []string{},
		IsCrossTenant:    false,
	}
	verifier := &fakeVerifier{tokens: map[string]*port.Claims{"tok-valid": claims}}
	factory := &fakeFactory{eval: buildEngine()}

	var res captureResult
	h := middleware.Auth(verifier, factory)(captureAuthHandler(&res, "test:data:buat"))

	w := doAuthRequest(t, h, "tok-valid")
	if w.Code != http.StatusOK {
		t.Fatalf("expect 200, got %d", w.Code)
	}
	if res.personID != personID {
		t.Errorf("personID: want %s got %s", personID, res.personID)
	}
	if res.persona != "employee" {
		t.Errorf("persona: want employee got %s", res.persona)
	}
	if res.tenantID != "pemkot-a" {
		t.Errorf("tenantID: want pemkot-a got %s", res.tenantID)
	}
	if res.isCitizen {
		t.Error("isCitizen harus false untuk persona employee")
	}
	if !res.hasRoleAdmin {
		t.Error("hasRole(admin_opd) harus true")
	}
	if res.hasCentralSA {
		t.Error("hasCentralRole(super_admin) harus false — tidak ada di CentralRoles klaim")
	}
	if res.permErr != nil {
		t.Errorf("RequirePermission(test:data:buat) harus nil, dapat: %v", res.permErr)
	}
}

func TestAuth_TokenValid_PermissionDenied(t *testing.T) {
	// admin_opd tidak punya "test:data:hapus"
	claims := &port.Claims{
		PersonID:    uuid.New(),
		Persona:     "employee",
		TenantID:    "pemkot-a",
		TenantRoles: []string{"admin_opd"},
	}
	verifier := &fakeVerifier{tokens: map[string]*port.Claims{"tok-terbatas": claims}}
	factory := &fakeFactory{eval: buildEngine()}

	var res captureResult
	h := middleware.Auth(verifier, factory)(captureAuthHandler(&res, "test:data:hapus"))

	w := doAuthRequest(t, h, "tok-terbatas")
	if w.Code != http.StatusOK { // middleware tidak tolak — handler yang dijalankan
		t.Fatalf("expect 200 dari middleware, got %d", w.Code)
	}
	if res.permErr == nil {
		t.Error("RequirePermission(test:data:hapus) harus error — admin_opd tidak punya perm ini")
	}
}

func TestAuth_TokenInvalid_401(t *testing.T) {
	verifier := &fakeVerifier{tokens: map[string]*port.Claims{}} // tidak ada token terdaftar
	factory := &fakeFactory{eval: buildEngine()}

	var res captureResult
	h := middleware.Auth(verifier, factory)(captureAuthHandler(&res, "test:data:baca"))

	w := doAuthRequest(t, h, "tok-palsu")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("token invalid harus 401, got %d", w.Code)
	}
}

func TestAuth_TanpaToken_AnonimLolos(t *testing.T) {
	// Tanpa token, request diteruskan; RequirePermission default permisif (eval nil).
	verifier := &fakeVerifier{tokens: map[string]*port.Claims{}}
	factory := &fakeFactory{eval: buildEngine()}

	var res captureResult
	h := middleware.Auth(verifier, factory)(captureAuthHandler(&res, "test:data:baca"))

	w := doAuthRequest(t, h, "") // tanpa header Authorization
	if w.Code != http.StatusOK {
		t.Fatalf("tanpa token harus lolos (anonimus), got %d", w.Code)
	}
	if res.permErr != nil {
		// eval nil → RequirePermission default permisif
		t.Errorf("anonimus: RequirePermission harus nil (permisif), dapat: %v", res.permErr)
	}
	if res.personID != (uuid.UUID{}) {
		t.Error("anonimus: personID harus zero UUID")
	}
}

func TestAuth_Citizen_IsCitizenTrue(t *testing.T) {
	// Persona citizen: tanpa tenantID, tanpa tenant role.
	personID := uuid.New()
	claims := &port.Claims{
		PersonID: personID,
		Persona:  "citizen",
		TenantID: "", // citizen tidak ada tenant
	}
	verifier := &fakeVerifier{tokens: map[string]*port.Claims{"tok-citizen": claims}}
	factory := &fakeFactory{eval: buildEngine()}

	var res captureResult
	h := middleware.Auth(verifier, factory)(captureAuthHandler(&res, "test:data:baca"))

	w := doAuthRequest(t, h, "tok-citizen")
	if w.Code != http.StatusOK {
		t.Fatalf("citizen harus lolos, got %d", w.Code)
	}
	if !res.isCitizen {
		t.Error("isCitizen harus true untuk persona citizen")
	}
	if res.tenantID != "" {
		t.Errorf("citizen tidak boleh punya tenantID, got %q", res.tenantID)
	}
	if res.personID != personID {
		t.Errorf("personID citizen: want %s got %s", personID, res.personID)
	}
}

func TestAuth_CentralRole_GlobalMenang(t *testing.T) {
	// super_admin (LayerGlobal) harus bisa test:data:baca meski tanpa TenantRoles.
	claims := &port.Claims{
		PersonID:     uuid.New(),
		Persona:      "employee",
		TenantID:     "pemkot-a",
		CentralRoles: []string{"super_admin"},
		TenantRoles:  []string{},
	}
	verifier := &fakeVerifier{tokens: map[string]*port.Claims{"tok-sa": claims}}
	factory := &fakeFactory{eval: buildEngine()}

	var res captureResult
	h := middleware.Auth(verifier, factory)(captureAuthHandler(&res, "test:data:baca"))

	doAuthRequest(t, h, "tok-sa")
	if !res.hasCentralSA {
		t.Error("HasCentralRole(super_admin) harus true")
	}
	if res.permErr != nil {
		t.Errorf("super_admin (global) harus izin test:data:baca, dapat: %v", res.permErr)
	}
}

func TestAuth_IsCrossTenant(t *testing.T) {
	claims := &port.Claims{
		PersonID:      uuid.New(),
		Persona:       "employee",
		TenantID:      "pemkot-b",
		IsCrossTenant: true,
	}
	verifier := &fakeVerifier{tokens: map[string]*port.Claims{"tok-cross": claims}}
	factory := &fakeFactory{eval: buildEngine()}

	var res captureResult
	h := middleware.Auth(verifier, factory)(captureAuthHandler(&res, "test:data:baca"))

	doAuthRequest(t, h, "tok-cross")
	if !res.isCrossTenant {
		t.Error("isCrossTenant harus true")
	}
}

func TestAuth_BearerFormat_HanyaPrefixBenar(t *testing.T) {
	claims := &port.Claims{PersonID: uuid.New(), Persona: "employee"}
	verifier := &fakeVerifier{tokens: map[string]*port.Claims{"mytoken": claims}}
	factory := &fakeFactory{eval: buildEngine()}

	var res captureResult
	next := captureAuthHandler(&res, "test:data:baca")
	h := middleware.Auth(verifier, factory)(next)

	// "Token mytoken" bukan format Bearer — dianggap tanpa token (anonimus).
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.Header.Set("Authorization", "Token mytoken")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("format bukan Bearer harus anonimus (200), got %d", w.Code)
	}
	if res.personID != (uuid.UUID{}) {
		t.Error("format bukan Bearer: personID harus zero")
	}
}
