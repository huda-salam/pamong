// Package gateway berisi driving adapter HTTP: context carrier, helpers respons,
// dan router. Ini adalah satu-satunya tempat di mana net/http masuk ke dalam framework.
package gateway

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core"
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
	eval             port.PermissionEvaluator
	scopedEval       port.ScopedEvaluator
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

// SetPermissionEvaluator menyuntik engine evaluasi permission (core/permission.Engine
// lewat port.PermissionEvaluator). Dipanggil middleware auth setelah katalog role
// terisi. Bila tidak diset, RequirePermission default permisif (lihat di bawah).
func (c *Context) SetPermissionEvaluator(e port.PermissionEvaluator) { c.eval = e }

func (c *Context) RequirePermission(perm string) error {
	if c.eval == nil {
		// Evaluator belum di-wire (request tanpa auth, atau sebelum populasi
		// katalog role di 2.3.2/2.3.3). Default permisif — perilaku tetap seperti
		// sebelum engine terpasang, sehingga seam ini tidak merusak alur lama.
		return nil
	}
	if c.eval.Allows(c.roleList(), perm) {
		return nil
	}
	return core.ErrPermissionDenied(perm)
}

// SetScopedEvaluator menyuntik evaluator permission data-level (core/permission.ScopedEngine
// terikat Authority actor lewat port.ScopedEvaluator). Dipanggil middleware auth (2.4) setelah
// membangun Authority dari resolver role+delegasi. Bila tidak diset, RequirePermissionInUnit
// default permisif (selaras RequirePermission).
func (c *Context) SetScopedEvaluator(e port.ScopedEvaluator) { c.scopedEval = e }

func (c *Context) RequirePermissionInUnit(perm string, unitID uuid.UUID) error {
	if c.scopedEval == nil {
		// Evaluator scoped belum di-wire (request tanpa auth, atau sebelum populasi
		// Authority di 2.4). Default permisif — selaras RequirePermission, seam tak
		// merusak alur lama.
		return nil
	}
	ok, err := c.scopedEval.AllowsInUnit(c.Context, perm, unitID)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	return core.ErrPermissionDenied(perm)
}

// roleList menggabungkan nama role tenant dan central yang dibawa context.
func (c *Context) roleList() []string {
	out := make([]string, 0, len(c.roles)+len(c.centralRoles))
	for r := range c.roles {
		out = append(out, r)
	}
	for r := range c.centralRoles {
		out = append(out, r)
	}
	return out
}

// --- context.Context (diteruskan ke context.Context yang di-embed) ---

func (c *Context) Deadline() (time.Time, bool) { return c.Context.Deadline() }
func (c *Context) Done() <-chan struct{}       { return c.Context.Done() }
func (c *Context) Err() error                  { return c.Context.Err() }
func (c *Context) Value(key any) any           { return c.Context.Value(key) }
