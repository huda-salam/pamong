// Package config memuat konfigurasi aplikasi berlapis (env > local > env-file > default)
// dan men-decode-nya ke AppConfig yang di-inject ke modul saat Bootstrap.
//
// Ini SATU-SATUNYA tempat yang boleh membaca environment variable. Modul bisnis
// menerima *AppConfig lewat parameter, tidak pernah memanggil os.Getenv sendiri
// (linter: config-no-direct-env, CODING_PHILOSOPHY #3).
package config

import (
	"fmt"
	"strings"
)

// AppConfig adalah konfigurasi runtime aplikasi, hasil merge seluruh lapis config.
// Tag `yaml` dipakai untuk file berlapis; tag `env` dipakai untuk override env var
// (format GOV_{SECTION}_{KEY}). Field tanpa tag env tidak bisa di-override dari env.
type AppConfig struct {
	Env string `yaml:"env" env:"GOV_ENV"`
	// TenantID hanya relevan untuk deployment single-tenant atau konteks CLI
	// (mis. `pamongctl migrate --tenant`). Di server multi-tenant, tenant berasal dari
	// request (tenant resolver), bukan dari config (ADR-004).
	TenantID string `yaml:"tenant_id" env:"GOV_TENANT_ID"`

	DB         DBConfig            `yaml:"db"`
	IdentityDB IdentityDBConfig    `yaml:"identity_db"`
	CentralDB  CentralDBConfig     `yaml:"central_db"`
	EventBus   EventBusConfig      `yaml:"eventbus"`
	Storage    StorageConfig       `yaml:"storage"`
	Cache      CacheConfig         `yaml:"cache"`
	Observ     ObservabilityConfig `yaml:"observability"`
	Auth       AuthConfig          `yaml:"auth"`
	RateLimit  RateLimitConfig     `yaml:"ratelimit"`
}

// DBConfig — DEFAULT/SHARED koneksi tenant DB (ADR-004). BUKAN "satu tenant DB":
// dengan DB-per-tenant, host & nama DB tiap tenant berasal dari id.tenant_registry
// (runtime), bukan dari sini. Yang di sini adalah parameter BERSAMA untuk menjangkau
// tenant DB: kredensial, port, pool, dan host default untuk Tier 1 (shared server).
// Name hanya fallback single-tenant/dev; TenantConnManager meng-override host+name
// per-tenant dari registry.
type DBConfig struct {
	Host     string `yaml:"host" env:"GOV_DB_HOST"` // host default Tier 1 (shared); per-tenant dari registry
	Port     int    `yaml:"port" env:"GOV_DB_PORT"`
	Name     string `yaml:"name" env:"GOV_DB_NAME"` // fallback single-tenant/dev; per-tenant dari registry
	User     string `yaml:"user" env:"GOV_DB_USER"`
	Password string `yaml:"password" env:"GOV_DB_PASSWORD"`
	PoolMax  int    `yaml:"pool_max" env:"GOV_DB_POOL_MAX"`
	PoolIdle int    `yaml:"pool_idle" env:"GOV_DB_POOL_IDLE"`
}

// IdentityDBConfig — koneksi PENUH ke identity DB sentral (terpisah dari tenant DB).
// Inilah satu-satunya koneksi yang wajib dari config: registry tenant hidup di sini,
// jadi bootstrap selalu connect ke sini dulu (ADR-004).
type IdentityDBConfig struct {
	Host     string `yaml:"host" env:"GOV_IDENTITY_DB_HOST"`
	Port     int    `yaml:"port" env:"GOV_IDENTITY_DB_PORT"`
	Name     string `yaml:"name" env:"GOV_IDENTITY_DB_NAME"`
	User     string `yaml:"user" env:"GOV_IDENTITY_DB_USER"`
	Password string `yaml:"password" env:"GOV_IDENTITY_DB_PASSWORD"`
	PoolMax  int    `yaml:"pool_max" env:"GOV_IDENTITY_DB_POOL_MAX"`
	PoolIdle int    `yaml:"pool_idle" env:"GOV_IDENTITY_DB_POOL_IDLE"`
}

// CentralDBConfig — koneksi ke DB sentral untuk data master/referensi shared semua
// tenant (entity ResidencyCentral, ADR-005). Bila Host kosong, central pool jatuh ke
// identity DB (gov_identity = "satu-satunya yang shared"); abstraksi ini memungkinkan
// pemisahan ke gov_central khusus nanti tanpa mengubah kode domain. Lihat
// AppConfig.CentralDBResolved().
type CentralDBConfig struct {
	Host     string `yaml:"host" env:"GOV_CENTRAL_DB_HOST"`
	Port     int    `yaml:"port" env:"GOV_CENTRAL_DB_PORT"`
	Name     string `yaml:"name" env:"GOV_CENTRAL_DB_NAME"`
	User     string `yaml:"user" env:"GOV_CENTRAL_DB_USER"`
	Password string `yaml:"password" env:"GOV_CENTRAL_DB_PASSWORD"`
	PoolMax  int    `yaml:"pool_max" env:"GOV_CENTRAL_DB_POOL_MAX"`
	PoolIdle int    `yaml:"pool_idle" env:"GOV_CENTRAL_DB_POOL_IDLE"`
}

// CentralDBResolved mengembalikan koneksi central yang efektif: CentralDB bila
// dikonfigurasi (Host terisi), atau koneksi identity DB sebagai fallback (ADR-005).
// Dengan begitu ops tidak wajib mengisi dua blok identik sampai central dipisah.
func (c *AppConfig) CentralDBResolved() CentralDBConfig {
	if c.CentralDB.Host != "" {
		return c.CentralDB
	}
	return CentralDBConfig{
		Host:     c.IdentityDB.Host,
		Port:     c.IdentityDB.Port,
		Name:     c.IdentityDB.Name,
		User:     c.IdentityDB.User,
		Password: c.IdentityDB.Password,
		PoolMax:  c.IdentityDB.PoolMax,
		PoolIdle: c.IdentityDB.PoolIdle,
	}
}

// EventBusConfig — driver event bus.
type EventBusConfig struct {
	Driver string `yaml:"driver" env:"GOV_EVENTBUS_DRIVER"` // nats | redis | memory
	URL    string `yaml:"url" env:"GOV_EVENTBUS_URL"`
	Stream string `yaml:"stream" env:"GOV_EVENTBUS_STREAM"`
}

// StorageConfig — driver object storage.
type StorageConfig struct {
	Driver    string `yaml:"driver" env:"GOV_STORAGE_DRIVER"` // minio | s3 | local
	Endpoint  string `yaml:"endpoint" env:"GOV_STORAGE_ENDPOINT"`
	Bucket    string `yaml:"bucket" env:"GOV_STORAGE_BUCKET"`
	AccessKey string `yaml:"access_key" env:"GOV_STORAGE_ACCESS_KEY"`
	SecretKey string `yaml:"secret_key" env:"GOV_STORAGE_SECRET_KEY"`
}

// CacheConfig — driver cache.
type CacheConfig struct {
	Driver     string `yaml:"driver" env:"GOV_CACHE_DRIVER"` // redis | memory
	URL        string `yaml:"url" env:"GOV_CACHE_URL"`
	TTLDefault int    `yaml:"ttl_default" env:"GOV_CACHE_TTL_DEFAULT"` // detik
}

// ObservabilityConfig — logging, metrics, tracing.
type ObservabilityConfig struct {
	OTELEndpoint   string `yaml:"otel_endpoint" env:"GOV_OTEL_ENDPOINT"`
	MetricsEnabled bool   `yaml:"metrics_enabled" env:"GOV_METRICS_ENABLED"`
	TracingEnabled bool   `yaml:"tracing_enabled" env:"GOV_TRACING_ENABLED"`
	LogLevel       string `yaml:"log_level" env:"GOV_LOG_LEVEL"`   // debug | info | warn | error
	LogFormat      string `yaml:"log_format" env:"GOV_LOG_FORMAT"` // json | text
}

// AuthConfig — verifikasi token SSO.
type AuthConfig struct {
	JWKSURL  string `yaml:"jwks_url" env:"GOV_AUTH_JWKS_URL"`
	Issuer   string `yaml:"issuer" env:"GOV_AUTH_ISSUER"`
	Audience string `yaml:"audience" env:"GOV_AUTH_AUDIENCE"`
}

// RateLimitConfig — pembatasan laju request.
type RateLimitConfig struct {
	Enabled bool `yaml:"enabled" env:"GOV_RATELIMIT_ENABLED"`
	RPS     int  `yaml:"rps" env:"GOV_RATELIMIT_RPS"`
	Burst   int  `yaml:"burst" env:"GOV_RATELIMIT_BURST"`
}

// nilai sah untuk field enumeratif — divalidasi saat boot agar salah ketik gagal cepat.
var (
	validEnvs       = map[string]bool{"development": true, "staging": true, "production": true}
	validLogLevels  = map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	validLogFormats = map[string]bool{"json": true, "text": true}
)

// Validate memeriksa invariant config. Dipanggil setelah merge; gagal = panic saat boot,
// bukan error misterius saat melayani request (CODING_PHILOSOPHY #4).
func (c *AppConfig) Validate() error {
	var errs []string

	if !validEnvs[c.Env] {
		errs = append(errs, fmt.Sprintf("env %q tidak valid (harus: development|staging|production)", c.Env))
	}
	if c.Observ.LogLevel != "" && !validLogLevels[c.Observ.LogLevel] {
		errs = append(errs, fmt.Sprintf("observability.log_level %q tidak valid (harus: debug|info|warn|error)", c.Observ.LogLevel))
	}
	if c.Observ.LogFormat != "" && !validLogFormats[c.Observ.LogFormat] {
		errs = append(errs, fmt.Sprintf("observability.log_format %q tidak valid (harus: json|text)", c.Observ.LogFormat))
	}

	// Di production, koneksi sentral wajib terisi — tidak boleh jalan dengan default kosong.
	// Identity DB adalah koneksi bootstrap (registry hidup di sini); DB default tenant
	// menyediakan kredensial bersama untuk menjangkau tenant DB (ADR-004). tenant_id TIDAK
	// wajib: server multi-tenant menentukan tenant dari request, bukan config.
	if c.Env == "production" {
		if c.IdentityDB.Host == "" {
			errs = append(errs, "identity_db.host wajib di production (koneksi sentral)")
		}
		if c.IdentityDB.User == "" {
			errs = append(errs, "identity_db.user wajib di production")
		}
		if c.DB.Host == "" {
			errs = append(errs, "db.host wajib di production (host default/shared tenant DB)")
		}
		if c.DB.User == "" {
			errs = append(errs, "db.user wajib di production (kredensial bersama tenant DB)")
		}
		if c.Observ.LogFormat == "text" {
			errs = append(errs, "observability.log_format=text dilarang di production (gunakan json)")
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("config tidak valid:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return nil
}
