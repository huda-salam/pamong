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
	auth           authRoutes            // nil → rute /auth/* tidak dipasang
	corsOrigins    []string
	logger         port.Logger
}

// authRoutes adalah kontrak minimal yang dibutuhkan mountAuthRoutes, dipenuhi oleh
// *identity/adapter/http.Handler. Seam ini ada agar PEMASANGAN rute (mana publik, mana menuntut
// token) dapat diuji tanpa merakit seluruh alur identity beserta DB-nya — yang diuji di situ
// adalah stack middleware di sekeliling handler, bukan isi handler-nya.
//
// HATI-HATI typed-nil: `var h *identityhttp.Handler; d.auth = h` menghasilkan interface yang
// TIDAK sama dengan nil, sehingga rute tetap terpasang dan menunjuk handler nil (panic saat
// request pertama). Karena itu wireAuth tak pernah mengembalikan (nil, nil) — kegagalan
// perakitan selalu jadi error, dan run() gagal boot. Kelas bug yang sama pernah tercatat sebagai
// REVIEW_BACKLOG H6.
type authRoutes interface {
	LoginEmployee(http.ResponseWriter, *http.Request)
	SelectTenant(http.ResponseWriter, *http.Request)
	LoginCitizen(http.ResponseWriter, *http.Request)
	RequestOTP(http.ResponseWriter, *http.Request)
	VerifyOTP(http.ResponseWriter, *http.Request)
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
	if d.auth != nil {
		mountAuthRoutes(top, d)
	}
	top.Handle("/", businessChain)

	// Recovery membungkus keduanya (panic di /healthz pun jadi 500 anggun).
	return middleware.Recovery(d.logger)(top)
}

// mountAuthRoutes memasang grup /auth/* pada top mux — DI LUAR business chain (PR-W1).
//
// Ini bukan preferensi tata letak melainkan syarat kebenaran: business chain memuat RequireAuth,
// jadi alur login yang dipasang di sana akan menuntut token untuk MEMPEROLEH token. Grup ini
// juga menutup DEFERRED(Phase-5.1.x) di require_auth.go yang mengantisipasi persis pemisahan
// publik vs internal ini.
//
// Middleware yang SENGAJA tidak dipasang di sini:
//
//   - **TenantResolver** — belum ada tenant sebelum token terbit. (Ia sebenarnya lolos-begitu-saja
//     saat klaim tenant kosong, tapi memasangnya menyiratkan rute ini punya tenant.)
//   - **RateLimit (middleware gateway)** — kuncinya per-PRINCIPAL (`rateLimitKey` = tenant+person),
//     dan pada rute pra-otentikasi principal selalu uuid.Nil. Semua penyerang anonim karena itu
//     akan berbagi SATU bucket global: alih-alih membatasi penyerang, ia memberi siapa pun cara
//     mematikan login bagi semua orang dengan membanjiri satu endpoint. Proteksi brute-force jalur
//     ini hidup di use case, ber-key per KREDENSIAL (passwordAuthenticator + OTPPolicy) — sesuai
//     port.RateLimiter §"Pola pemakaian". Rate limit per-IP adalah kebutuhan berbeda yang menuntut
//     keputusan proxy tepercaya (X-Forwarded-For yang dipercaya buta = penyerang mencetak key tak
//     terbatas); belum diambil, jangan dipasang setengah-setengah.
//
// /auth/select-tenant adalah pengecualian: ia menukar token SEMENTARA menjadi token final, jadi
// justru menuntut token — Auth + RequireAuth dipasang, TenantResolver tetap tidak.
func mountAuthRoutes(top *http.ServeMux, d serverDeps) {
	public := []func(http.Handler) http.Handler{
		middleware.CORS(d.corsOrigins),
		middleware.RequestID(),
	}
	authenticated := append(append([]func(http.Handler) http.Handler{}, public...),
		middleware.Auth(d.verifier, d.evalFactory),
		middleware.RequireAuth(),
	)

	top.Handle("POST /auth/login", chain(http.HandlerFunc(d.auth.LoginEmployee), public...))
	top.Handle("POST /auth/public/login", chain(http.HandlerFunc(d.auth.LoginCitizen), public...))
	top.Handle("POST /auth/public/otp/request", chain(http.HandlerFunc(d.auth.RequestOTP), public...))
	top.Handle("POST /auth/public/otp/verify", chain(http.HandlerFunc(d.auth.VerifyOTP), public...))
	top.Handle("POST /auth/select-tenant", chain(http.HandlerFunc(d.auth.SelectTenant), authenticated...))
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
