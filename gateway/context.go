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

	// cachedRoles adalah gabungan nama role tenant+central, dihitung sekali saat
	// konstruksi (role maps tak pernah berubah sesudahnya). Menghindari realokasi
	// slice tiap pemanggilan RequirePermission.
	cachedRoles []string
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

// NewContextFromClaims membuat Context yang sudah ter-populasi dari Claims terverifikasi.
// Dipanggil oleh middleware auth (PR-2.4.2) setelah token lolos Verify; eval & scopedEval
// disuntik sesudahnya via SetPermissionEvaluator / SetScopedEvaluator.
func NewContextFromClaims(parent context.Context, c *port.Claims) *Context {
	roles := make(map[string]bool, len(c.TenantRoles))
	for _, r := range c.TenantRoles {
		roles[r] = true
	}
	central := make(map[string]bool, len(c.CentralRoles))
	for _, r := range c.CentralRoles {
		central[r] = true
	}
	roleList := make([]string, 0, len(roles)+len(central))
	for r := range roles {
		roleList = append(roleList, r)
	}
	for r := range central {
		roleList = append(roleList, r)
	}
	return &Context{
		Context:          parent,
		personID:         c.PersonID,
		persona:          c.Persona,
		employmentStatus: c.EmploymentStatus,
		tenantID:         c.TenantID,
		roles:            roles,
		centralRoles:     central,
		isCrossTenant:    c.IsCrossTenant,
		cachedRoles:      roleList,
	}
}

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

// HasCentralRole memeriksa KEBERADAAN nama central role pada klaim — ia TIDAK sadar
// tenant_scope. Untuk scoped role (mis. regional_helpdesk yang hanya berlaku di tenant
// tertentu), method ini mengembalikan true di tenant mana pun karena Context hanya
// membawa nama role, bukan katalog yang tahu mana global vs scoped. Karena itu:
//   - Keputusan OTORISASI WAJIB lewat RequirePermission (yang melalui Engine + katalog,
//     menegakkan scope), BUKAN HasCentralRole.
//   - HasCentralRole hanya untuk hint UI / cabang non-sekuriti.
//
// Invariant scope ditegakkan saat login (PR-2.4.3): CentralRoleResolver hanya memasukkan
// nama role yang berlaku untuk person+tenant ke dalam token. Lihat docs/security/REVIEW_BACKLOG.md (A4/C1).
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
// Hasil di-cache saat konstruksi via NewContextFromClaims; jalur konstruksi lain
// (mis. FromRequest fallback) menghitung sekali secara lazy.
func (c *Context) roleList() []string {
	if c.cachedRoles != nil {
		return c.cachedRoles
	}
	out := make([]string, 0, len(c.roles)+len(c.centralRoles))
	for r := range c.roles {
		out = append(out, r)
	}
	for r := range c.centralRoles {
		out = append(out, r)
	}
	c.cachedRoles = out
	return out
}

// --- context.Context (diteruskan ke context.Context yang di-embed) ---

func (c *Context) Deadline() (time.Time, bool) { return c.Context.Deadline() }
func (c *Context) Done() <-chan struct{}       { return c.Context.Done() }
func (c *Context) Err() error                  { return c.Context.Err() }
func (c *Context) Value(key any) any           { return c.Context.Value(key) }
