package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/gateway"
	"github.com/huda-salam/pamong/gateway/middleware"
	"github.com/huda-salam/pamong/port"
)

// fakeResolver: registry tenant in-memory untuk unit test middleware.
type fakeResolver struct {
	tenants map[string]*port.TenantInfo
}

func (f *fakeResolver) Resolve(_ context.Context, id string) (*port.TenantInfo, error) {
	if t, ok := f.tenants[id]; ok {
		return t, nil
	}
	return nil, core.ErrNotFound("Tenant", id)
}

func newResolver() *fakeResolver {
	return &fakeResolver{tenants: map[string]*port.TenantInfo{
		"pemkot-a": {TenantID: "pemkot-a", IsActive: true},
		"pemkot-b": {TenantID: "pemkot-b", IsActive: true},
		"pemkot-x": {TenantID: "pemkot-x", IsActive: false},
	}}
}

// captureHandler menangkap tenant_id yang terlihat handler downstream.
func captureHandler(seen *string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*seen = gateway.FromRequest(r).TenantID()
		w.WriteHeader(http.StatusOK)
	})
}

// reqWithClaimTenant membangun request yang sudah membawa gateway.Context dengan tenant_id
// dari klaim token tersigning (mensimulasikan auth middleware yang berjalan lebih dulu).
// header opsional X-Tenant-ID disisipkan untuk membuktikan ia DIABAIKAN.
func reqWithClaimTenant(claimTenant, headerTenant string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	if headerTenant != "" {
		r.Header.Set("X-Tenant-ID", headerTenant)
	}
	if claimTenant != "" {
		c := gateway.NewContextFromClaims(r.Context(), &port.Claims{
			PersonID: uuid.New(),
			Persona:  "employee",
			TenantID: claimTenant,
		})
		r = gateway.WithContext(r, c)
	}
	return r
}

func runResolver(t *testing.T, r *http.Request, seen *string) *httptest.ResponseRecorder {
	t.Helper()
	h := middleware.TenantResolver(newResolver())(captureHandler(seen))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestTenantResolver_Isolation(t *testing.T) {
	var seen string

	// Request tenant A (klaim) → context tenant A.
	if w := runResolver(t, reqWithClaimTenant("pemkot-a", ""), &seen); w.Code != http.StatusOK || seen != "pemkot-a" {
		t.Fatalf("tenant A: code=%d seen=%q", w.Code, seen)
	}
	// Request tenant B (klaim) → context tenant B (request sebelumnya tidak bocor).
	if w := runResolver(t, reqWithClaimTenant("pemkot-b", ""), &seen); w.Code != http.StatusOK || seen != "pemkot-b" {
		t.Fatalf("tenant B: code=%d seen=%q", w.Code, seen)
	}
}

func TestTenantResolver_UnknownTenant_404(t *testing.T) {
	var seen string
	w := runResolver(t, reqWithClaimTenant("tidak-ada", ""), &seen)
	if w.Code != http.StatusNotFound {
		t.Fatalf("tenant tak dikenal harus 404, dapat %d", w.Code)
	}
}

func TestTenantResolver_InactiveTenant_403(t *testing.T) {
	var seen string
	w := runResolver(t, reqWithClaimTenant("pemkot-x", ""), &seen)
	if w.Code != http.StatusForbidden {
		t.Fatalf("tenant nonaktif harus 403, dapat %d", w.Code)
	}
}

func TestTenantResolver_NoTenant_PassThrough(t *testing.T) {
	var seen = "sentinel"
	// Tanpa klaim tenant (mis. anonimus/citizen) → lolos tanpa tenant.
	w := runResolver(t, httptest.NewRequest(http.MethodGet, "/x", nil), &seen)
	if w.Code != http.StatusOK || seen != "" {
		t.Fatalf("tanpa tenant harus lolos tanpa tenant: code=%d seen=%q", w.Code, seen)
	}
}

// TestTenantResolver_HeaderDiabaikan mengunci properti keamanan: X-Tenant-ID tak pernah
// menjadi sumber tenant. Klien tak bisa memalsukan/menarget tenant lewat header.
func TestTenantResolver_HeaderDiabaikan(t *testing.T) {
	// Hanya header, tanpa klaim → diperlakukan tanpa tenant (header diabaikan).
	var seen = "sentinel"
	if w := runResolver(t, reqWithClaimTenant("", "pemkot-a"), &seen); w.Code != http.StatusOK || seen != "" {
		t.Fatalf("header-only harus lolos tanpa tenant (diabaikan): code=%d seen=%q", w.Code, seen)
	}

	// Klaim pemkot-a + header pemkot-b → klaim menang, header diabaikan total.
	seen = "sentinel"
	if w := runResolver(t, reqWithClaimTenant("pemkot-a", "pemkot-b"), &seen); w.Code != http.StatusOK || seen != "pemkot-a" {
		t.Fatalf("klaim harus menang atas header: code=%d seen=%q", w.Code, seen)
	}
}
