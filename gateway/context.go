// Package gateway berisi driving adapter HTTP: context carrier, helpers respons,
// dan router. Ini adalah satu-satunya tempat di mana net/http masuk ke dalam framework.
package gateway

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/port"
)

// Context membawa identitas actor dan tenant yang di-extract dari JWT oleh middleware auth.
// Ia memenuhi port.AuthContext sehingga bisa diteruskan langsung ke use case.
type Context struct {
	context.Context

	personID         uuid.UUID
	persona          string
	employmentStatus string
	tenantID         string
	roles            map[string]bool
	centralRoles     map[string]bool
	isCrossTenant    bool
}

var _ port.AuthContext = (*Context)(nil)

// FromRequest mengekstrak Context dari *http.Request.
// Middleware auth wajib menyimpan *Context ke request context sebelum handler dipanggil.
func FromRequest(r *http.Request) *Context {
	if c, ok := r.Context().Value(contextKey{}).(*Context); ok {
		return c
	}
	// Fallback untuk test / request tanpa auth middleware — konteks anonim.
	return &Context{Context: r.Context()}
}

// WithContext menyimpan *Context ke dalam request context untuk diambil FromRequest.
func WithContext(r *http.Request, c *Context) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), contextKey{}, c))
}

type contextKey struct{}

// --- port.AuthContext ---

func (c *Context) PersonID() uuid.UUID      { return c.personID }
func (c *Context) Persona() string          { return c.persona }
func (c *Context) EmploymentStatus() string { return c.employmentStatus }
func (c *Context) TenantID() string         { return c.tenantID }
func (c *Context) IsCitizen() bool          { return c.persona == "citizen" }

// SetTenantID dipakai middleware tenant resolver setelah memvalidasi tenant terhadap
// registry. Tenant tidak pernah diset dari input mentah tanpa resolusi.
func (c *Context) SetTenantID(id string) { c.tenantID = id }
func (c *Context) IsCrossTenant() bool   { return c.isCrossTenant }

func (c *Context) HasRole(role string) bool {
	return c.roles[role] || c.centralRoles[role]
}

func (c *Context) HasCentralRole(role string) bool {
	return c.centralRoles[role]
}

func (c *Context) RequirePermission(perm string) error {
	// Implementasi penuh ada di Phase 2 (permission engine).
	// Saat ini cukup untuk kompilasi; middleware akan mengganti dengan evaluasi nyata.
	return nil
}

// --- context.Context (diteruskan ke context.Context yang di-embed) ---

func (c *Context) Deadline() (time.Time, bool) { return c.Context.Deadline() }
func (c *Context) Done() <-chan struct{}       { return c.Context.Done() }
func (c *Context) Err() error                  { return c.Context.Err() }
func (c *Context) Value(key any) any           { return c.Context.Value(key) }
