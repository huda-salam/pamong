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

## ScopedEvaluator kini terpasang (PR-W3b · ADR-021)

`middleware.EvaluatorFactory.Build` mengembalikan **dua** evaluator (RBAC + scoped) dari satu
panggilan, dan `Auth` memasang keduanya. Konsekuensinya: `RequirePermissionInUnit` **menegakkan**
untuk request ber-token — sebelumnya default permisif.

- **Satu factory, bukan dua.** Keduanya diturunkan dari bahan yang sama (katalog role tenant +
  klaim); dua seam terpisah akan mengundang perakitan yang menyimpang, dan penyimpangan di sini
  berarti RBAC & ABAC menjawab dari dunia yang berbeda.
- **nil scoped = PERMISIF.** Itu sebabnya konteks tanpa tenant tetap dipasangi evaluator (Authority
  kosong) alih-alih nil — lihat `cmd/server/scoped_evaluator.go`.
- **Request anonim tetap tanpa evaluator.** Penolakannya berasal dari pemisahan rute publik/internal
  saat registrasi router, bukan dari `RequirePermission*`.

## Runtime workflow terpasang (PR-W4a · ADR-022)

`gateway/workflow` adalah permukaan HTTP FRAMEWORK untuk runtime alur (bukan milik satu modul,
sama seperti CRUD auto-generate Tier 1): `POST /workflow/instances`,
`GET /workflow/instances/{id}`, `POST /workflow/instances/{id}/transitions` — semuanya di router
BISNIS, di balik stack lengkap.

- **Tumpukan per tenant lewat seam `RuntimeProvider`**, dirakit `cmd/server/workflow.go`. Handler
  tak pernah menyentuh pool DB, jadi tak pernah bisa keliru melayani tenant lain.
- **Tenant selalu dari klaim token**, tak pernah dari body/query.
- **Instance tenant lain = 404, bukan 403.** 403 sudah membocorkan bahwa ID itu ada.
- **Body transisi TIDAK punya field entity.** Guard hanya boleh membaca keadaan tersimpan; params
  hanya untuk action (ADR-022 Keputusan 2). Snapshot entity untuk guard ber-`entity.x` belum ada,
  dan evaluator MENOLAK pembacaan `entity.x` bila snapshot tak tersedia (ADR-022 Keputusan 7) —
  transisinya gagal, tidak lolos diam-diam. Penolakan terjadi di titik baca, jadi cabang yang
  ter-short-circuit tetap sah. DEFERRED(PR-W4c).
- **Transisi dikunci per instance** (`TryLockInstance`) SEBELUM action dijalankan; yang bertabrakan
  dijawab 409, tidak diantrekan. Optimistic locking pada Save saja tidak cukup — ia menolak
  penulis yang kalah setelah efek bisnisnya terlanjur terjadi (ADR-022 Keputusan 5). Kuncinya
  BARIS ber-sewa (`gov.workflow_instance_locks`), bukan lock sesi: lock sesi menahan koneksi
  selama action berjalan, dan action memakai pool yang sama.
- **Satu instance per (definisi, entitas)**, ditegakkan unique index. Tanpanya alur yang sudah
  selesai bisa dimulai ulang dan action-nya dijalankan lagi.
- **Otorisasi masih setingkat TENANT, belum entitas** — lihat docs/contracts/permissions.md
  §workflow. DEFERRED(PR-W4c).
