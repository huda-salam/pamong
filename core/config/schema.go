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
	Env      string `yaml:"env" env:"GOV_ENV"`
	TenantID string `yaml:"tenant_id" env:"GOV_TENANT_ID"`

	DB         DBConfig            `yaml:"db"`
	IdentityDB IdentityDBConfig    `yaml:"identity_db"`
	EventBus   EventBusConfig      `yaml:"eventbus"`
	Storage    StorageConfig       `yaml:"storage"`
	Cache      CacheConfig         `yaml:"cache"`
	Observ     ObservabilityConfig `yaml:"observability"`
	Auth       AuthConfig          `yaml:"auth"`
	RateLimit  RateLimitConfig     `yaml:"ratelimit"`
}

// DBConfig — koneksi DB tenant aktif.
type DBConfig struct {
	Host     string `yaml:"host" env:"GOV_DB_HOST"`
	Port     int    `yaml:"port" env:"GOV_DB_PORT"`
	Name     string `yaml:"name" env:"GOV_DB_NAME"`
	User     string `yaml:"user" env:"GOV_DB_USER"`
	Password string `yaml:"password" env:"GOV_DB_PASSWORD"`
	PoolMax  int    `yaml:"pool_max" env:"GOV_DB_POOL_MAX"`
	PoolIdle int    `yaml:"pool_idle" env:"GOV_DB_POOL_IDLE"`
}

// IdentityDBConfig — koneksi identity DB sentral (terpisah dari tenant DB).
type IdentityDBConfig struct {
	Host string `yaml:"host" env:"GOV_IDENTITY_DB_HOST"`
	Name string `yaml:"name" env:"GOV_IDENTITY_DB_NAME"`
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

	// Di production, kredensial DB dan tenant wajib terisi — tidak boleh jalan dengan default kosong.
	if c.Env == "production" {
		if c.DB.Host == "" {
			errs = append(errs, "db.host wajib di production")
		}
		if c.TenantID == "" {
			errs = append(errs, "tenant_id wajib di production")
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
