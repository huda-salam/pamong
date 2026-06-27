// Package middleware berisi middleware stack gateway (auth, tenant resolver, dll).
// Middleware murni lintas-potong: tidak berisi business logic (itu use case).
package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/huda-salam/pamong/gateway"
	"github.com/huda-salam/pamong/port"
)

// EvaluatorFactory membangun port.PermissionEvaluator dari Claims yang sudah terverifikasi.
// Interface ini disembunyikan dari middleware sehingga middleware tidak bergantung pada
// detail core/permission (catalog, engine) — hanya pada port.PermissionEvaluator yang
// sudah dikenal. Implementasi konkret (CompositeCatalog + Engine per-request atau cached)
// dibangun di luar middleware dan disuntik saat bootstrap.
//
// Citizen (TenantID kosong) tetap dapat evaluator — biasanya Engine hanya dengan
// CentralRoleCatalog (tanpa tenant). Implementasi dapat mengembalikan nil untuk menandakan
// "default permisif" bila tidak ada catalog tersedia; Auth middleware memaknainya sebagai
// anonymous (eval tidak di-set → RequirePermission default permisif).
type EvaluatorFactory interface {
	Build(ctx context.Context, claims *port.Claims) (port.PermissionEvaluator, error)
}

// Auth mengembalikan middleware yang memverifikasi JWT pada setiap request (PRD gateway F3,
// langkah 3). Alur:
//
//  1. Ekstrak "Bearer <token>" dari header Authorization.
//  2. Panggil verifier.Verify — tolak (401) bila token tak valid, kedaluwarsa, atau dicabut.
//  3. Bangun gateway.Context dari Claims (personID, persona, roles, tenantID, dll).
//  4. Panggil factory.Build — bangun evaluator RBAC (Engine + CompositeCatalog) dari Claims;
//     suntik ke Context via SetPermissionEvaluator.
//  5. Teruskan ke handler berikutnya.
//
// Request tanpa header Authorization diteruskan sebagai anonymous (Context kosong, eval nil
// → RequirePermission default permisif). Handler/use case yang memerlukan auth wajib
// memanggil RequirePermission yang akan gagal karena tidak ada role.
//
// ScopedEvaluator (RequirePermissionInUnit) tetap default permisif hingga wiring Authority
// live di PR berikutnya (DEFERRED Phase-2.4, ROADMAP "Wiring Authority live").
func Auth(verifier port.TokenVerifier, factory EvaluatorFactory) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := extractBearer(r)
			if raw == "" {
				// Tanpa token: teruskan sebagai anonymous; downstream enforce via RequirePermission.
				next.ServeHTTP(w, r)
				return
			}

			claims, err := verifier.Verify(r.Context(), raw)
			if err != nil {
				gateway.WriteError(w, err) // ErrUnauthorized → 401
				return
			}

			c := gateway.NewContextFromClaims(r.Context(), claims)

			eval, err := factory.Build(r.Context(), claims)
			if err != nil {
				gateway.WriteError(w, err)
				return
			}
			if eval != nil {
				c.SetPermissionEvaluator(eval)
			}
			// DEFERRED(Phase-2.4): c.SetScopedEvaluator — wiring Authority (ScopedEngine)
			// menunggu emitter central-role→Grant TenantWide + DelegatedGrants live.

			next.ServeHTTP(w, gateway.WithContext(r, c))
		})
	}
}

// extractBearer mengambil token dari header "Authorization: Bearer <token>".
// Mengembalikan string kosong bila header tidak ada atau formatnya bukan Bearer.
func extractBearer(r *http.Request) string {
	v := r.Header.Get("Authorization")
	if !strings.HasPrefix(v, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(v, "Bearer ")
}
