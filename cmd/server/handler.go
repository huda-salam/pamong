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
	corsOrigins    []string
	logger         port.Logger
}

// buildServerHandler merakit stack middleware KEAMANAN (PR-5.1.2) di sekeliling router bisnis
// dan menutup celah permisif-default PR-5.1.1: rute bisnis wajib auth, RequirePermission
// menegakkan RBAC live, laju dibatasi. Urutan (PRD gateway F3), terluar → terdalam:
//
//	Recovery → CORS → RequestID → Auth → RequireAuth → TenantResolver → RateLimit → router
//
// Catatan urutan:
//   - Recovery terluar: menangkap panic dari semua middleware & handler (termasuk /healthz).
//   - CORS sebelum Auth: preflight OPTIONS dijawab tanpa kredensial aplikasi.
//   - TenantResolver SETELAH Auth: tenant hanya dari klaim token tersigning; ia menyuntik
//     port.WithTenant ke context (via SetTenantID) → TenantRoutingConn route DB dengan benar.
//   - RateLimit SETELAH RequireAuth: principal dijamin ada (anonymous sudah ditolak 401).
//   - Optimistic-lock (409) & audit ditegakkan di lapis repository (audited repos + WHERE
//     version=), bukan middleware. Idempotency middleware = DEFERRED(Phase-5.1.2b, butuh
//     tabel gov.idempotency_keys).
//
// /healthz dilayani auth-free lewat top mux (liveness untuk orchestrator); sisanya lewat stack.
func buildServerHandler(d serverDeps) http.Handler {
	// Urutan dibangun sebagai slice agar RateLimit dapat disisipkan sebagai lapisan TERDALAM
	// (dijalankan SETELAH Auth+TenantResolver) — bukan membungkus seluruh chain. Ini krusial:
	// RateLimit membaca principal dari gateway.Context (lewat FromRequest), yang baru terisi
	// setelah Auth berjalan. Bila RateLimit diletakkan di luar (outermost), ia berjalan sebelum
	// Auth → FromRequest mengembalikan konteks anonim → semua request berbagi satu bucket nil-
	// principal (bukan per-principal). Karena itu ia HARUS jadi entri terakhir chain.
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
