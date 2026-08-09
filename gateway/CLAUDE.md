# gateway/ — API Gateway & Middleware

Driving adapter: lapisan HTTP API. Mengumpulkan rute dari semua modul, menjalankan
middleware stack (auth, tenant resolver, rate limit, idempotency, optimistic lock, CORS,
audit). gateway.Context mengimplementasi port.AuthContext.

## Bergantung pada
- port/, core/* (lewat port), infra/* (lewat port)

## Tidak boleh
- Berisi business logic — hanya routing, middleware, dan delegasi ke use case

## Tanggung jawab
- Router aggregator: kumpulkan rute modul saat bootstrap
- Auto-generate CRUD endpoint dari EntityDef (Tier 1)
- Middleware stack berurutan (lihat PRD untuk urutan)
- gateway.Context: carrier auth+tenant+trace, implement port.AuthContext
- Tenant resolver: tentukan DB tenant dari tenant_registry (DB-per-tenant)
- Idempotency & optimistic lock middleware (data integrity framework-level)

## File kunci
- router.go — Router: implementasi konkret port.Router di atas net/http.ServeMux
  (method-aware Go 1.22+); agregasi rute modul, dirakit di cmd/server (PR-5.1.1)
- middleware/ — auth, tenant_resolver, ratelimit, idempotency, optlock, cors, audit (PR-5.1.2)
- context.go — gateway.Context (implement AuthContext)

## Status implementasi
- PR-5.1.1 (SELESAI): Router aggregator + wiring cmd/server + http.Server + /healthz + recovery.
  Routing DB per-tenant lewat infra/db.TenantRoutingConn (baca tenant dari context;
  port.WithTenant/TenantFrom + fallback AuthContext).
- PR-5.1.2 (SELESAI): stack middleware KEAMANAN (Recovery/CORS/RequestID/Auth/RequireAuth/
  TenantResolver/RateLimit). Rute bisnis kini WAJIB auth & RequirePermission menegakkan RBAC
  live — layak deploy (dengan identity schema ter-migrasi + GOV_AUTH_TOKEN_SECRET di prod).
- PR-5.1.2b (SELESAI): idempotency middleware (mutasi + Idempotency-Key; replay/409/422/503) +
  CORS allowlist dari config (GOV_CORS_ALLOWED_ORIGINS).
- PR-5.1.2c (SELESAI): strict-perm dari manifest → Engine (ADR-014) + refresh catalog tenant
  TTL-based (GOV_PERMISSION_CATALOG_TTL; invalidasi event-driven DEFERRED).
- PR-W1 (SELESAI): route-grouping PUBLIK vs INTERNAL. Grup `/auth/*` (login employee/citizen, OTP)
  dipasang di top mux `cmd/server` TANPA RequireAuth; `/auth/select-tenant` ber-auth (menukar token
  sementara). Sebelumnya RequireAuth memagari semua rute non-healthz sementara tak ada endpoint
  login — server tak bisa dilayani klien mana pun. Middleware `RateLimit` gateway SENGAJA tidak
  dipasang di grup ini (kuncinya per-principal; pada request anonim = satu bucket global) —
  proteksi brute-force ada di use case, per-kredensial.

## Konvensi khusus
- Urutan middleware penting (lihat PRD). Auth & tenant resolver di awal.
- Tenant resolver membaca tenant_registry: shared (search_path) vs dedicated (koneksi lain).
- Error types framework auto-map ke HTTP status di sini.

## Pitfall umum
- Menaruh logika di handler/middleware (harus di use case).
- Lupa urutan middleware (idempotency sebelum mutasi, audit setelah).

## Test
- Unit: middleware (auth tolak tanpa token, rate limit, idempotency replay).
- Integration: request lintas tenant terisolasi; CRUD Tier 1 end-to-end.
- go test ./gateway/... -race

## Rujukan
- PRD.md, port/auth.go, core/domain (generation), identity (auth)
