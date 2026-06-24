# PRD: Configuration & Tenant Config

## Tujuan
Menyediakan (1) konfigurasi aplikasi yang berlapis dan tervalidasi, dan (2) tenant
config ber-scope yang bisa diperdalam (tenant → unit kerja → resource) tanpa mengubah
skema saat kebutuhan scope berkembang. Mencegah modul membaca environment langsung.

## Konteks & batasan
### Jadi tanggung jawab
- Load & merge config aplikasi (env, file berlapis), validasi, injeksi ke modul
- Penyimpanan & resolusi tenant config ber-scope bertingkat
### BUKAN tanggung jawab
- Makna config spesifik modul (modul yang menafsirkan)
- Pilihan strategy itu sendiri (core/strategy; config hanya menyimpan)

## Model data / tipe kunci
```go
type AppConfig struct {
    Env          string
    DB           DBConfig
    IdentityDB   DBConfig
    EventBus     EventBusConfig
    Storage      StorageConfig
    Cache        CacheConfig
    Observability ObsConfig
    Auth         AuthConfig
    RateLimit    RateLimitConfig
}

// Tenant config ber-scope. Key: "decisionPoint", scope bertingkat.
type ConfigScope struct {
    TenantID    string
    UnitKerjaID *uuid.UUID   // nil = level tenant
    ResourceID  *uuid.UUID   // nil = level unit/tenant
}
```

## Kebutuhan fungsional
- F1: Load config dengan precedence env > config/local.yaml > config/{env}.yaml >
  config/default.yaml. Validasi saat boot; invalid → panic dengan pesan jelas.
- F2: AppConfig di-inject ke modul lewat Bootstrap. Modul tidak os.Getenv.
- F3: Config field tambahan modul dari manifest ConfigSpec; nilai dari
  GOV_MODULE_{MODUL}_{KEY} atau UI admin.
- F4: Tenant config disimpan di gov.tenant_configs dengan scope. Resolver mengembalikan
  nilai paling spesifik yang cocok (resource > unit > tenant).
- F5: Config ber-versi + effective date untuk yang berdampak (mis. pilihan strategy).

## Kebutuhan non-fungsional
- Resolusi tenant config: < 2ms (cached, invalidasi via event).
- Load config aplikasi: saat boot.

## Dependency
- port/ + stdlib; event bus untuk invalidasi cache config.

## Anti-pattern
- os.Getenv di modul. Scope tenant-only yang di-hardcode (harus mendukung bertingkat).

## Acceptance criteria
- [ ] Precedence config benar (env menang atas semua).
- [ ] Config invalid → panic saat boot.
- [ ] Tenant config: scope unit kerja meng-override level tenant (paling spesifik menang).
- [ ] Modul menerima config via AppConfig, bukan os.Getenv.
