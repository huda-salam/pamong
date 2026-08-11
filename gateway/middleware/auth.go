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

// EvaluatorFactory membangun kedua evaluator permission dari Claims yang sudah terverifikasi:
// RBAC (port.PermissionEvaluator) dan data-level/ABAC (port.ScopedEvaluator). Interface ini
// menyembunyikan detail core/permission (catalog, engine, Authority) dari middleware — yang
// konkret dirakit di composition root dan disuntik lewat interface ini.
//
// Keduanya datang dari SATU panggilan, bukan dua factory, karena keduanya diturunkan dari bahan
// yang sama (katalog role tenant + klaim). Dua seam terpisah akan mengundang perakitan yang
// menyimpang — dan penyimpangan di sini berarti RBAC dan ABAC menjawab dari dunia yang berbeda.
//
// Citizen (TenantID kosong) tetap dapat evaluator RBAC — biasanya Engine hanya dengan
// CentralRoleCatalog (tanpa tenant). Implementasi dapat mengembalikan nil untuk menandakan
// "default permisif" bila tidak ada catalog tersedia; Auth middleware memaknainya sebagai
// anonymous (eval tidak di-set → RequirePermission default permisif).
type EvaluatorFactory interface {
	Build(ctx context.Context, claims *port.Claims) (port.PermissionEvaluator, port.ScopedEvaluator, error)
}

// Auth mengembalikan middleware yang memverifikasi JWT pada setiap request (PRD gateway F3,
// langkah 3). Alur:
//
//  1. Ekstrak "Bearer <token>" dari header Authorization.
//  2. Panggil verifier.Verify — tolak (401) bila token tak valid, kedaluwarsa, atau dicabut.
//  3. Bangun gateway.Context dari Claims (personID, persona, roles, tenantID, dll).
//  4. Panggil factory.Build — bangun evaluator RBAC (Engine + CompositeCatalog) DAN evaluator
//     data-level (ScopedEngine + Authority) dari Claims; suntik ke Context via
//     SetPermissionEvaluator & SetScopedEvaluator.
//  5. Teruskan ke handler berikutnya.
//
// Request tanpa header Authorization diteruskan sebagai anonymous (Context kosong, eval nil).
// CATATAN: dengan eval nil, RequirePermission default PERMISIF (mengembalikan nil) — BUKAN
// menolak. Penolakan request anonymous TIDAK berasal dari RequirePermission; ia bergantung
// pada (a) route publik vs internal yang dipisah saat registrasi router, dan (b) evaluator
// yang ter-wire (factory.Build) untuk request ber-token. Jangan mengandalkan RequirePermission
// sendirian untuk menolak anonymous.
//
// Sejak PR-W3b, request ber-token JUGA membawa ScopedEvaluator, sehingga
// RequirePermissionInUnit benar-benar MENEGAKKAN (sebelumnya default permisif). Request anonim
// tetap tanpa evaluator: penolakannya berasal dari pemisahan rute publik/internal, bukan dari
// RequirePermission* — lihat catatan di atas.
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

			eval, scoped, err := factory.Build(r.Context(), claims)
			if err != nil {
				gateway.WriteError(w, err)
				return
			}
			if eval != nil {
				c.SetPermissionEvaluator(eval)
			}
			if scoped != nil {
				c.SetScopedEvaluator(scoped)
			}

			next.ServeHTTP(w, gateway.WithContext(r, c))
		})
	}
}

// extractBearer mengambil token dari header "Authorization: Bearer <token>".
// Mengembalikan string kosong bila header tidak ada atau formatnya bukan Bearer.
func extractBearer(r *http.Request) string {
	if after, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		return after
	}
	return ""
}
