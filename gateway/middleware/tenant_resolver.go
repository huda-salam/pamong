// Package middleware berisi middleware stack gateway (auth, tenant resolver, dll).
// Middleware murni lintas-potong: tidak berisi business logic (itu use case).
package middleware

import (
	"net/http"

	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/gateway"
	"github.com/huda-salam/pamong/port"
)

// TenantResolver mengembalikan middleware yang menentukan tenant request DARI KLAIM TOKEN
// SAJA (di-set auth middleware ke gateway.Context), memvalidasinya terhadap registry sentral,
// lalu menyuntikkan tenant_id ter-resolve ke gateway.Context (PRD gateway F5).
//
// Tidak ada sumber tenant tak-tersigning (mis. header X-Tenant-ID): tenant_id hanya berasal
// dari klaim JWT yang ditandatangani (HS256) sehingga tak bisa dipalsukan klien. Flow tanpa
// token yang perlu menarget tenant (service/CLI) wajib lewat mekanisme ber-permission &
// ter-audit (service token ber-claim atau endpoint tenant-switch), bukan input mentah —
// diputuskan lewat ADR saat dibutuhkan.
//
// Aturan:
//   - tanpa tenant id di klaim → diteruskan tanpa tenant (mis. portal publik/citizen);
//     downstream yang memutuskan apakah tenant wajib.
//   - tenant tidak dikenal → 404; tenant nonaktif → 403 (defense-in-depth: token bisa
//     membawa tenant yang sejak itu dinonaktifkan). Isolasi: tiap request hanya pernah
//     membawa tenant-nya sendiri.
func TenantResolver(resolver port.TenantResolver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c := gateway.FromRequest(r)
			tenantID := c.TenantID() // hanya dari klaim token tersigning
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

			c.SetTenantID(info.TenantID)
			next.ServeHTTP(w, gateway.WithContext(r, c))
		})
	}
}
