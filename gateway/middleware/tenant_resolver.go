// Package middleware berisi middleware stack gateway (auth, tenant resolver, dll).
// Middleware murni lintas-potong: tidak berisi business logic (itu use case).
package middleware

import (
	"net/http"

	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/gateway"
	"github.com/huda-salam/pamong/port"
)

// TenantHeader adalah header sumber tenant bila tidak datang dari klaim token.
const TenantHeader = "X-Tenant-ID"

// TenantResolver mengembalikan middleware yang menentukan tenant request dari klaim
// token (bila ada) atau header X-Tenant-ID, memvalidasinya terhadap registry sentral,
// lalu menyuntikkan tenant_id ter-resolve ke gateway.Context (PRD gateway F5).
//
// Aturan:
//   - tanpa tenant id → diteruskan tanpa tenant (mis. portal publik/citizen); downstream
//     yang memutuskan apakah tenant wajib.
//   - tenant tidak dikenal → 404; tenant nonaktif → 403. Isolasi: tiap request hanya
//     pernah membawa tenant-nya sendiri.
func TenantResolver(resolver port.TenantResolver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tenantID := extractTenantID(r)
			if tenantID == "" {
				next.ServeHTTP(w, r)
				return
			}

			info, err := resolver.Resolve(r.Context(), tenantID)
			if err != nil {
				gateway.WriteError(w, err) // ErrNotFound → 404
				return
			}
			if !info.IsActive {
				gateway.WriteError(w, &core.FrameworkError{
					Code:    "FORBIDDEN",
					Message: "tenant nonaktif: " + tenantID,
				})
				return
			}

			c := gateway.FromRequest(r)
			c.SetTenantID(info.TenantID)
			next.ServeHTTP(w, gateway.WithContext(r, c))
		})
	}
}

// extractTenantID mengambil tenant dari klaim token (gateway.Context) bila sudah diset
// auth middleware, jika tidak dari header X-Tenant-ID.
func extractTenantID(r *http.Request) string {
	if c := gateway.FromRequest(r); c.TenantID() != "" {
		return c.TenantID()
	}
	return r.Header.Get(TenantHeader)
}
