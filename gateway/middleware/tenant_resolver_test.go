package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

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

func doRequest(t *testing.T, h http.Handler, tenantHeader string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	if tenantHeader != "" {
		r.Header.Set(middleware.TenantHeader, tenantHeader)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestTenantResolver_Isolation(t *testing.T) {
	var seen string
	h := middleware.TenantResolver(newResolver())(captureHandler(&seen))

	// Request tenant A → context tenant A.
	if w := doRequest(t, h, "pemkot-a"); w.Code != http.StatusOK || seen != "pemkot-a" {
		t.Fatalf("tenant A: code=%d seen=%q", w.Code, seen)
	}
	// Request tenant B → context tenant B (request sebelumnya tidak bocor).
	if w := doRequest(t, h, "pemkot-b"); w.Code != http.StatusOK || seen != "pemkot-b" {
		t.Fatalf("tenant B: code=%d seen=%q", w.Code, seen)
	}
}

func TestTenantResolver_UnknownTenant_404(t *testing.T) {
	var seen string
	h := middleware.TenantResolver(newResolver())(captureHandler(&seen))
	w := doRequest(t, h, "tidak-ada")
	if w.Code != http.StatusNotFound {
		t.Fatalf("tenant tak dikenal harus 404, dapat %d", w.Code)
	}
}

func TestTenantResolver_InactiveTenant_403(t *testing.T) {
	var seen string
	h := middleware.TenantResolver(newResolver())(captureHandler(&seen))
	w := doRequest(t, h, "pemkot-x")
	if w.Code != http.StatusForbidden {
		t.Fatalf("tenant nonaktif harus 403, dapat %d", w.Code)
	}
}

func TestTenantResolver_NoTenant_PassThrough(t *testing.T) {
	var seen string = "sentinel"
	h := middleware.TenantResolver(newResolver())(captureHandler(&seen))
	w := doRequest(t, h, "")
	if w.Code != http.StatusOK || seen != "" {
		t.Fatalf("tanpa tenant harus lolos tanpa tenant: code=%d seen=%q", w.Code, seen)
	}
}
