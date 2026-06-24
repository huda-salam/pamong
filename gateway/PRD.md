# PRD: API Gateway & Middleware

## Tujuan
Menyediakan satu titik masuk HTTP yang konsisten: mengumpulkan endpoint dari semua modul,
menerapkan lintas-potong (auth, tenant, rate limit, idempotency, optimistic lock, audit)
secara seragam, dan menyediakan gateway.Context sebagai AuthContext untuk use case.

## Konteks & batasan
### Jadi tanggung jawab
- Agregasi rute; auto-CRUD endpoint Tier 1; middleware stack; tenant resolution;
  gateway.Context
### BUKAN tanggung jawab
- Business logic (use case modul)
- Issue/verify token internal (delegasi ke identity; gateway memanggil verify)

## Model data / tipe kunci
```go
// gateway.Context membawa identitas, tenant, trace; implement port.AuthContext
type Context struct { ... }
func (c *Context) PersonID() uuid.UUID
func (c *Context) TenantID() string
func (c *Context) RequirePermission(perm string) error
// dll sesuai port.AuthContext
```

## Kebutuhan fungsional

### F1 — Router aggregator
- Saat bootstrap, kumpulkan rute dari semua modul (didaftarkan di Bootstrap masing-masing).
- Konflik rute → gagal boot dengan pesan jelas.

### F2 — Auto-generate CRUD endpoint (Tier 1)
- Dari EntityDef, sediakan endpoint GET/POST/PATCH/DELETE/LIST tanpa kode modul.
- Endpoint menerapkan permission default, validasi deklaratif, optimistic lock,
  idempotency, audit.

### F3 — Middleware stack (urutan penting)
```
1. Recovery (panic -> 500 terstruktur)
2. Request ID / tracing (correlation id)
3. Auth (verify JWT; tolak bila invalid/expired/revoked)
4. Tenant resolver (tentukan DB tenant dari tenant_registry; inject ke context)
5. Rate limit (per tenant/per user)
6. Idempotency (cek/simpan key untuk endpoint mutasi)
7. -> Handler -> Use case (permission check di dalam use case)
8. Optimistic lock (cek version saat update; conflict -> 409)
9. Audit (catat mutasi setelah sukses)
10. Error mapping (error types framework -> HTTP status)
11. CORS
```

### F4 — gateway.Context (AuthContext)
- Implementasi port.AuthContext: PersonID, Persona, TenantID, HasRole, HasCentralRole,
  RequirePermission, IsCitizen, IsCrossTenant.
- Membaca claim dari JWT (di-verify di middleware auth). Modul tidak decode token.

### F5 — Tenant resolver (DB-per-tenant)
- Baca tenant_registry: bila tier shared → set search_path di DB default; bila dedicated
  DB/server → gunakan koneksi/pool ke host/db tenant.
- Connection pooling per unique host.
- Tenant tidak dikenal / nonaktif → tolak.

## Kebutuhan non-fungsional
- Overhead middleware total: < 15ms per request (di luar use case & DB).
- Rate limit & idempotency store cepat (cache).
- Isolasi tenant ketat: request tenant A tidak pernah menyentuh DB tenant B.

## Dependency
- identity — verify token, resolusi tenant assignment
- core/domain — entity def untuk auto-CRUD
- core/permission — RequirePermission
- core/audit — pencatatan mutasi
- infra/db — koneksi tenant; infra/cache — rate limit & idempotency store

## Anti-pattern / yang harus dihindari
- Business logic di handler/middleware.
- Urutan middleware salah (mis. audit sebelum sukses, idempotency setelah mutasi).
- Tenant resolver yang mengasumsikan satu DB untuk semua tenant.

## Keputusan tertunda
- WebSocket untuk real-time (notifikasi, workflow update) — adapter terpisah, Phase lanjut.
- API versioning strategy (URL vs header) — diputuskan via ADR sebelum implementasi penuh.

## Acceptance criteria
- [ ] Rute semua modul ter-agregasi; konflik rute → gagal boot.
- [ ] Entity Tier 1 → endpoint CRUD penuh berfungsi tanpa kode modul.
- [ ] Request tanpa/with token invalid → ditolak (401).
- [ ] Rate limit aktif (429 saat lewat batas).
- [ ] Idempotency: request mutasi duplikat (key sama) → response sama tanpa efek ganda.
- [ ] Optimistic lock: update dengan version basi → 409.
- [ ] Request tenant berbeda terisolasi (tidak menyentuh DB tenant lain).
- [ ] Mutasi sukses tercatat di audit.
- [ ] gateway.Context.RequirePermission menegakkan permission.
