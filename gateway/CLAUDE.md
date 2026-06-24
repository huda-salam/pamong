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
- router/ — agregasi rute
- middleware/ — auth, tenant_resolver, ratelimit, idempotency, optlock, cors, audit
- context.go — gateway.Context (implement AuthContext)

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
