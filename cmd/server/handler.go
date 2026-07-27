package main

import (
	"context"
	"net/http"
	"time"

	"github.com/huda-salam/pamong/core/config"
	"github.com/huda-salam/pamong/gateway/middleware"
	"github.com/huda-salam/pamong/port"
)

// serverDeps mengumpulkan dependency untuk merakit http.Handler puncak. Dikumpulkan dalam satu
// struct agar buildServerHandler dapat dipanggil dari run() (produksi) maupun test (dengan
// fake) tanpa daftar argumen panjang.
type serverDeps struct {
	router         http.Handler
	verifier       port.TokenVerifier
	evalFactory    middleware.EvaluatorFactory
	tenantResolver port.TenantResolver
	rateLimiter    port.RateLimiter
	rateLimit      config.RateLimitConfig
	idempotency    port.IdempotencyStore // nil → middleware idempotency tidak dipasang
	corsOrigins    []string
	logger         port.Logger
}

// buildServerHandler merakit stack middleware KEAMANAN (PR-5.1.2/5.1.2b) di sekeliling router
// bisnis dan menutup celah permisif-default PR-5.1.1: rute bisnis wajib auth, RequirePermission
// menegakkan RBAC live, laju dibatasi, mutasi ber-idempotency. Urutan (PRD gateway F3),
// terluar → terdalam:
//
//	Recovery → CORS → RequestID → Auth → RequireAuth → TenantResolver → RateLimit → Idempotency → router
//
// Catatan urutan:
//   - Recovery terluar: menangkap panic dari semua middleware & handler (termasuk /healthz).
//   - CORS sebelum Auth: preflight OPTIONS dijawab tanpa kredensial aplikasi.
//   - TenantResolver SETELAH Auth: tenant hanya dari klaim token tersigning; ia menyuntik
//     port.WithTenant ke context (via SetTenantID) → TenantRoutingConn route DB dengan benar.
//   - RateLimit & Idempotency SETELAH RequireAuth+TenantResolver: keduanya membaca principal
//     (dan tenant) dari gateway.Context lewat FromRequest, yang baru terisi setelah Auth.
//     RateLimit sebelum Idempotency mengikuti urutan PRD (5→6): batasi laju dulu, baru
//     de-duplikasi request mutasi.
//   - Optimistic-lock (409) & audit ditegakkan di lapis repository (audited repos + WHERE
//     version=), bukan middleware.
//
// /healthz dilayani auth-free lewat top mux (liveness untuk orchestrator); sisanya lewat stack.
func buildServerHandler(d serverDeps) http.Handler {
	// Urutan dibangun sebagai slice; middleware yang bergantung pada principal (RateLimit,
	// Idempotency) HARUS berada SETELAH Auth+TenantResolver (bukan membungkus seluruh chain
	// dari luar). Bila diletakkan di luar Auth, FromRequest mengembalikan konteks anonim →
	// principal/tenant kosong → RateLimit ber-bucket global (bukan per-principal) & Idempotency
	// kehilangan scope tenant/principal. Karena itu keduanya ditambahkan sebagai entri TERAKHIR
	// (terdalam), sesudah TenantResolver.
	mws := []func(http.Handler) http.Handler{
		middleware.CORS(d.corsOrigins),
		middleware.RequestID(),
		middleware.Auth(d.verifier, d.evalFactory),
		middleware.RequireAuth(),
		middleware.TenantResolver(d.tenantResolver),
	}
	if d.rateLimit.Enabled && d.rateLimit.RPS > 0 {
		mws = append(mws, middleware.RateLimit(d.rateLimiter, d.rateLimit.RPS, time.Second))
	} else {
		d.logger.Info(context.Background(), "rate limit nonaktif (RATELIMIT_ENABLED=false atau RPS<=0)")
	}
	if d.idempotency != nil {
		mws = append(mws, middleware.Idempotency(d.idempotency, d.logger))
	} else {
		d.logger.Info(context.Background(), "idempotency nonaktif (store tidak dikonfigurasi)")
	}
	businessChain := chain(d.router, mws...)

	top := http.NewServeMux()
	top.HandleFunc("GET /healthz", healthz)
	top.Handle("/", businessChain)

	// Recovery membungkus keduanya (panic di /healthz pun jadi 500 anggun).
	return middleware.Recovery(d.logger)(top)
}

// healthz adalah handler liveness (tak menyentuh DB, aman dipakai orchestrator). Dilayani
// auth-free lewat top mux; juga dipasang sebagai tripwire di router bisnis (lihat run()).
func healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// chain menerapkan middleware ke handler sehingga mws[0] menjadi lapisan TERLUAR (dijalankan
// pertama saat request masuk, terakhir saat respons keluar) — urutan intuitif sesuai daftar
// PRD gateway F3. Recovery sengaja TIDAK lewat sini; ia dibungkus paling luar di
// buildServerHandler agar menangkap panic dari top mux (termasuk /healthz), bukan hanya router.
func chain(h http.Handler, mws ...func(http.Handler) http.Handler) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}
