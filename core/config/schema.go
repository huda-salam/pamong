// Package config memuat konfigurasi aplikasi berlapis (env > local > env-file > default)
// dan men-decode-nya ke AppConfig yang di-inject ke modul saat Bootstrap.
//
// Ini SATU-SATUNYA tempat yang boleh membaca environment variable. Modul bisnis
// menerima *AppConfig lewat parameter, tidak pernah memanggil os.Getenv sendiri
// (linter: config-no-direct-env, CODING_PHILOSOPHY #3).
package config

import (
	"encoding/base64"
	"fmt"
	"strings"
	"time"
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

	DB          DBConfig            `yaml:"db"`
	IdentityDB  IdentityDBConfig    `yaml:"identity_db"`
	CentralDB   CentralDBConfig     `yaml:"central_db"`
	ProvisionDB ProvisionDBConfig   `yaml:"provision_db"`
	EventBus    EventBusConfig      `yaml:"eventbus"`
	Storage     StorageConfig       `yaml:"storage"`
	Messaging   MessagingConfig     `yaml:"messaging"`
	Crypto      CryptoConfig        `yaml:"crypto"`
	Cache       CacheConfig         `yaml:"cache"`
	Observ      ObservabilityConfig `yaml:"observability"`
	Auth        AuthConfig          `yaml:"auth"`
	RateLimit   RateLimitConfig     `yaml:"ratelimit"`
	HTTP        HTTPConfig          `yaml:"http"`
	CORS        CORSConfig          `yaml:"cors"`
	Permission  PermissionConfig    `yaml:"permission"`
	Scheduler   SchedulerConfig     `yaml:"scheduler"`
}

// SchedulerConfig — loop eksekusi job terjadwal (core/scheduler). Tabelnya hidup di DB SENTRAL
// (ADR-023), jadi SATU loop per proses melayani seluruh tenant; di multi-instance, lock ber-sewa
// (gov.job_locks) yang mencegah satu job jalan ganda.
//
// SENGAJA tanpa flag enabled: scheduler adalah jalur eksekusi SLA workflow, dan komponen yang
// bisa dimatikan lewat config adalah komponen yang akan ditemukan mati di produksi (DoD 11).
// Menjalankan lebih dari satu instance aman justru karena lock-nya.
type SchedulerConfig struct {
	// IntervalSeconds = periode polling jadwal jatuh tempo. 0 = default 30s. Ketelitian
	// penjadwalan berorde nilai ini: deadline SLA berorde jam, jadi puluhan detik memadai.
	IntervalSeconds int `yaml:"interval" env:"GOV_SCHEDULER_INTERVAL"`
	// LockTTLSeconds = masa sewa lock per job. HARUS lebih lama dari durasi terpanjang yang
	// wajar untuk sebuah job: sewa yang kedaluwarsa selagi job masih berjalan mengizinkan
	// instance lain mengambil alih dan menjalankannya kedua kali. 0 = default 300s.
	LockTTLSeconds int `yaml:"lock_ttl" env:"GOV_SCHEDULER_LOCK_TTL"`
}

// Interval mengembalikan periode polling sebagai Duration (default 30 detik).
func (s SchedulerConfig) Interval() time.Duration {
	if s.IntervalSeconds <= 0 {
		return 30 * time.Second
	}
	return time.Duration(s.IntervalSeconds) * time.Second
}

// LockTTL mengembalikan masa sewa lock job sebagai Duration (default 5 menit).
func (s SchedulerConfig) LockTTL() time.Duration {
	if s.LockTTLSeconds <= 0 {
		return 5 * time.Minute
	}
	return time.Duration(s.LockTTLSeconds) * time.Second
}

// PermissionConfig — perilaku penegakan RBAC di gateway. CatalogTTLSeconds mengatur umur
// cache snapshot catalog role tenant di evaluator factory: setelah lewat, catalog dibangun
// ulang dari tenant DB sehingga perubahan DEFINISI role tenant (permission yang dipetakan
// ke sebuah nama role) terlihat tanpa restart, dengan jeda ≤ TTL. CAKUPAN TERBATAS: refresh
// ini TIDAK menyegarkan role apa yang dipegang user, maupun penonaktifan user — keduanya
// dibawa klaim token (berlaku lewat masa token / revocation jti), bukan lewat catalog.
// 0 = default 300s; negatif = tak pernah kedaluwarsa. Invalidasi seketika (event-driven saat
// admin mengubah definisi role) menyusul — lihat DEFERRED di cmd/server/evaluator_factory.go.
type PermissionConfig struct {
	CatalogTTLSeconds int `yaml:"catalog_ttl" env:"GOV_PERMISSION_CATALOG_TTL"`
}

// CatalogTTL mengembalikan umur cache catalog tenant sebagai Duration. Bila tidak diset
// (<= 0), memakai default aman 300 detik (5 menit) — cukup segar untuk perubahan role,
// cukup jarang agar tak membebani tenant DB tiap request. Untuk menonaktifkan kedaluwarsa
// secara eksplisit, set negatif via kode (bukan jalur config umum).
func (p PermissionConfig) CatalogTTL() time.Duration {
	if p.CatalogTTLSeconds == 0 {
		return 300 * time.Second
	}
	if p.CatalogTTLSeconds < 0 {
		return 0
	}
	return time.Duration(p.CatalogTTLSeconds) * time.Second
}

// HTTPConfig — server HTTP (gateway). Addr = alamat listen (mis. ":8080").
type HTTPConfig struct {
	Addr string `yaml:"addr" env:"GOV_HTTP_ADDR"`
}

// CORSConfig — kebijakan Cross-Origin Resource Sharing (gateway middleware CORS).
// AllowedOrigins adalah ALLOWLIST origin eksplisit (mis. "https://admin.pemkot.go.id").
// Kosong = same-origin only (default aman untuk konteks pemerintahan; tak ada "*" implisit).
// Override env memakai daftar dipisah koma: GOV_CORS_ALLOWED_ORIGINS="https://a,https://b".
type CORSConfig struct {
	AllowedOrigins []string `yaml:"allowed_origins" env:"GOV_CORS_ALLOWED_ORIGINS"`
}

// HTTPAddr mengembalikan alamat listen efektif, default ":8080" bila tak diset.
func (c *AppConfig) HTTPAddr() string {
	if c.HTTP.Addr == "" {
		return ":8080"
	}
	return c.HTTP.Addr
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

// ProvisionDBConfig — kredensial ADMIN untuk provisioning tenant DB (ADR-006). TERPISAH
// dari kredensial runtime (DBConfig) demi least privilege: role ini ber-CREATEDB dan
// dipakai HANYA saat membuat tenant DB baru (`pamongctl tenant provision`), tidak pernah
// untuk melayani request. Host target & port diambil dari registry + DBConfig.Port; di
// sini hanya kredensial admin + nama DB maintenance tempat `CREATE DATABASE` dieksekusi.
type ProvisionDBConfig struct {
	User        string `yaml:"user" env:"GOV_PROVISION_DB_USER"`
	Password    string `yaml:"password" env:"GOV_PROVISION_DB_PASSWORD"`
	Maintenance string `yaml:"maintenance" env:"GOV_PROVISION_DB_MAINTENANCE"` // DB untuk connect saat CREATE DATABASE; default "postgres"
}

// EventBusConfig — driver event bus.
type EventBusConfig struct {
	Driver string `yaml:"driver" env:"GOV_EVENTBUS_DRIVER"` // nats | redis | memory
	URL    string `yaml:"url" env:"GOV_EVENTBUS_URL"`
	Stream string `yaml:"stream" env:"GOV_EVENTBUS_STREAM"`

	// Retry & DLQ — dipakai OutboxRelay (PR-3.1.4).
	// 0 = pakai default (MaxAttempts=5, BackoffBase=5s, BackoffMax=1h).
	RetryMaxAttempts int           `yaml:"retry_max_attempts" env:"GOV_EVENTBUS_RETRY_MAX_ATTEMPTS"`
	RetryBackoffBase time.Duration `yaml:"retry_backoff_base" env:"GOV_EVENTBUS_RETRY_BACKOFF_BASE"`
	RetryBackoffMax  time.Duration `yaml:"retry_backoff_max" env:"GOV_EVENTBUS_RETRY_BACKOFF_MAX"`
}

// StorageConfig — driver object storage.
type StorageConfig struct {
	Driver    string `yaml:"driver" env:"GOV_STORAGE_DRIVER"` // minio | s3 | local
	Endpoint  string `yaml:"endpoint" env:"GOV_STORAGE_ENDPOINT"`
	Bucket    string `yaml:"bucket" env:"GOV_STORAGE_BUCKET"`
	AccessKey string `yaml:"access_key" env:"GOV_STORAGE_ACCESS_KEY"`
	SecretKey string `yaml:"secret_key" env:"GOV_STORAGE_SECRET_KEY"`
}

// MessagingConfig — driver pengiriman pesan keluar (SMS/email) untuk OTP & notifikasi.
// Driver ber-registry seperti storage (titik ekstensi #1): pemanggil tetap bergantung
// pada port.MessagingPort sehingga mengganti driver tidak mengubah kode pemanggil.
//   - "log"  — driver dev/test: mencatat pesan ke log, selalu sukses, nol dependency
//     eksternal. DILARANG di production (body OTP bocor ke log) — ditolak Validate().
//   - "smtp" — email nyata via stdlib net/smtp. SMS tidak didukung driver ini (onboarding).
//
// Field SMTP* hanya relevan bila Driver == "smtp".
type MessagingConfig struct {
	Driver       string `yaml:"driver" env:"GOV_MESSAGING_DRIVER"` // log | smtp
	SMTPHost     string `yaml:"smtp_host" env:"GOV_MESSAGING_SMTP_HOST"`
	SMTPPort     int    `yaml:"smtp_port" env:"GOV_MESSAGING_SMTP_PORT"`
	SMTPUser     string `yaml:"smtp_user" env:"GOV_MESSAGING_SMTP_USER"`
	SMTPPassword string `yaml:"smtp_password" env:"GOV_MESSAGING_SMTP_PASSWORD"`
	FromEmail    string `yaml:"from_email" env:"GOV_MESSAGING_FROM_EMAIL"` // alamat pengirim (header From)
}

// CryptoConfig — enkripsi field selektif (ADR-009) & key management (ADR-010).
//
// KMS adalah DRIVER ber-registry, pola eventbus/storage/messaging (titik ekstensi #1):
// kode kripto bergantung pada KeyProvider, bukan vendor, sehingga KMS eksternal di-plug
// tanpa mengubah kode.
//   - "static" — KMS-alike bawaan, DEFAULT PRODUKSI Tier 1/2. Master KEK dari MasterKey
//     (base64 32-byte) yang WAJIB berasal dari secret store/env — JANGAN commit ke
//     default.yaml. Ber-versi (MasterKeyV2 dst) agar rotasi KEK murah: yang di-re-wrap
//     hanya DEK, data tak disentuh.
//   - "local" — dev/test saja: kunci turunan tetap, tanpa versi, nol konfigurasi.
//     DILARANG di luar development (ditolak Validate) — pola sama messaging.driver=log.
//   - "vault"/"aws-kms"/"gcp-kms"/"bssn"/... — didaftarkan ke registry saat pengadaan
//     menentukan; config ini tak perlu berubah selain Driver + Endpoint.
//
// Custody (siapa pegang KEK) BUKAN di sini: ia kebijakan per-tenant (kolom
// id.tenant_registry.key_custody, ADR-010 §3) karena tiap pemda bisa berbeda kontrak.
type CryptoConfig struct {
	KMSDriver   string `yaml:"kms_driver" env:"GOV_CRYPTO_KMS_DRIVER"` // static | local | vault | ...
	KMSEndpoint string `yaml:"kms_endpoint" env:"GOV_CRYPTO_KMS_ENDPOINT"`

	// Master KEK driver "static", base64 std-encoding 32 byte. Versi tertinggi yang terisi
	// menjadi versi AKTIF (dipakai membungkus DEK baru); versi lama tetap dibutuhkan untuk
	// membuka DEK yang sudah ada — karena itu JANGAN hapus V1 saat mengisi V2.
	MasterKey   string `yaml:"master_key" env:"GOV_CRYPTO_MASTER_KEY"`
	MasterKeyV2 string `yaml:"master_key_v2" env:"GOV_CRYPTO_MASTER_KEY_V2"`

	// DEKCacheTTL membatasi umur DEK ter-decrypt di cache in-process (bukan di DB/log).
	// 0 = default 5 menit. Menghindari round-trip KMS per-baris tanpa menahan kunci selamanya.
	DEKCacheTTL time.Duration `yaml:"dek_cache_ttl" env:"GOV_CRYPTO_DEK_CACHE_TTL"`
}

// masterKeyLen adalah panjang master KEK driver "static": 32 byte = AES-256.
const masterKeyLen = 32

// DEKCacheTTLOrDefault memberi umur cache DEK dengan default aman bila tidak diset.
func (c CryptoConfig) DEKCacheTTLOrDefault() time.Duration {
	if c.DEKCacheTTL <= 0 {
		return 5 * time.Minute
	}
	return c.DEKCacheTTL
}

// MasterKeys mengembalikan master KEK per-versi hasil decode base64, terurut implisit lewat
// kunci map (versi 1..N). Error bila ada nilai terisi yang bukan base64 32-byte — dipakai
// baik oleh Validate (gagal saat boot) maupun driver static (gagal saat konstruksi).
func (c CryptoConfig) MasterKeys() (map[int][]byte, error) {
	out := make(map[int][]byte, 2)
	for version, encoded := range map[int]string{1: c.MasterKey, 2: c.MasterKeyV2} {
		if encoded == "" {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return nil, fmt.Errorf("crypto.master_key versi %d bukan base64 yang sah: %w", version, err)
		}
		if len(raw) != masterKeyLen {
			return nil, fmt.Errorf("crypto.master_key versi %d harus %d byte, bukan %d", version, masterKeyLen, len(raw))
		}
		out[version] = raw
	}
	return out, nil
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

// AuthConfig memuat dua concern token yang TERPISAH:
//   - Token INTERNAL — diterbitkan & diverifikasi Pamong sendiri (HS256, modular monolith;
//     ADR-007). Dikendalikan TokenSecret + TokenTTLSeconds.
//   - Token SSO EKSTERNAL — diverifikasi dari IdP lain (asimetris/JWKS). Dikendalikan
//     JWKSURL + Issuer + Audience. Dipakai PR auth eksternal (bukan PR-2.4.1).
type AuthConfig struct {
	// Token internal (HS256) — identity/adapter/token.
	TokenSecret     string `yaml:"token_secret" env:"GOV_AUTH_TOKEN_SECRET"` // kunci HMAC; wajib & ≥32 byte di production
	TokenTTLSeconds int    `yaml:"token_ttl" env:"GOV_AUTH_TOKEN_TTL"`       // umur token internal (detik); 0 = default

	// TokenMaxBytes = pagar ukuran token terbit (ADR-020). Ini KEBIJAKAN OPS, bukan konstanta:
	// batas header yang sesungguhnya ada di proxy di depan aplikasi dan berbeda per deployment
	// (nginx 8 KiB per header, ALB 16 KiB), jadi angkanya harus bisa dinaikkan tanpa rilis.
	// 0 = pakai default aman adapter token (token.DefaultMaxBytes) — pagar TETAP aktif, karena
	// deployment yang lupa menyetel ini justru yang paling butuh dilindungi.
	TokenMaxBytes int `yaml:"token_max_bytes" env:"GOV_AUTH_TOKEN_MAX_BYTES"`

	// Token SSO eksternal (verifikasi token dari IdP lain).
	JWKSURL  string `yaml:"jwks_url" env:"GOV_AUTH_JWKS_URL"`
	Issuer   string `yaml:"issuer" env:"GOV_AUTH_ISSUER"`
	Audience string `yaml:"audience" env:"GOV_AUTH_AUDIENCE"`
}

// TokenTTL mengembalikan umur token internal sebagai Duration, dengan default aman bila
// tidak diset (TokenTTLSeconds <= 0).
func (a AuthConfig) TokenTTL() time.Duration {
	if a.TokenTTLSeconds <= 0 {
		return time.Hour
	}
	return time.Duration(a.TokenTTLSeconds) * time.Second
}

// RateLimitConfig — pembatasan laju request.
type RateLimitConfig struct {
	Enabled bool `yaml:"enabled" env:"GOV_RATELIMIT_ENABLED"`
	RPS     int  `yaml:"rps" env:"GOV_RATELIMIT_RPS"`
	Burst   int  `yaml:"burst" env:"GOV_RATELIMIT_BURST"`
}

// minTokenSecretLen adalah panjang minimum kunci HMAC token internal (HS256). 32 byte =
// 256 bit, setara ukuran output HS256; kunci lebih pendek melemahkan tanda tangan.
const minTokenSecretLen = 32

// MinTokenMaxBytes & MaxTokenMaxBytes mengurung auth.token_max_bytes (ADR-020) dari kedua sisi.
//
// Lantai 1 KiB: token dasar tanpa satu pun role sudah 383 byte (diukur, ADR-020), jadi nilai di
// bawah ini pasti
// salah ketik — dan akibatnya memblokir SEMUA login.
//
// Plafon 64 KiB: ambang ini juga MENURUNKAN http.Server.MaxHeaderBytes (lihat
// cmd/server.maxHeaderBytes), sehingga nilai raksasa membatalkan sendiri tujuan pagar ini —
// batas header kembali selonggar default Go 1 MiB, dan satu klien bisa menahan buffer sebesar itu
// per koneksi. 64 KiB sudah jauh di atas batas proxy mana pun yang lazim (nginx 8 KiB, ALB 16 KiB);
// deployment yang merasa butuh lebih sedang memakai token sebagai penyimpan data, bukan identitas.
// Keduanya diekspor agar composition root memakai angka yang SAMA, bukan salinannya.
const (
	MinTokenMaxBytes = 1024
	MaxTokenMaxBytes = 64 * 1024
)

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
	// Driver messaging "log" mencatat body pesan (termasuk kode OTP) ke log — HANYA boleh di
	// development. Staging & production memakai data mirip-nyata; paksa transport nyata (smtp/dst).
	if c.Messaging.Driver == "log" && c.Env != "development" {
		errs = append(errs, fmt.Sprintf(
			"messaging.driver=log hanya untuk development (body OTP bocor ke log; env=%q butuh smtp/provider nyata)", c.Env))
	}

	// Driver eventbus "memory" mengantar SINKRON di goroutine pemanggil dan tanpa durability:
	// tak ada persistence, tak ada retry, dan error handler MENGALIR BALIK ke pemanggil Publish.
	// Konsekuensinya di luar development bukan sekadar "kurang tangguh" — ia mengubah semantik
	// use case: satu subscriber yang gagal (mis. clone identity ke tenant DB yang sedang tak
	// terjangkau) menggagalkan use case SESUDAH mutasi bisnisnya commit, dan percobaan ulang
	// menabrak invariant anti-duplikat yang justru dibuat oleh percobaan pertama. Ditolak di luar
	// development, pola sama messaging=log & kms_driver=local.
	// Driver KOSONG ikut ditolak, bukan dibiarkan: infra/eventbus.newDriver memetakan "" dan
	// "memory" ke driver yang SAMA, jadi section eventbus yang lupa diisi menghasilkan persis
	// perilaku di atas — tanpa satu pun kata "memory" di file config yang bisa dicurigai.
	if (c.EventBus.Driver == "memory" || c.EventBus.Driver == "") && c.Env != "development" {
		errs = append(errs, fmt.Sprintf(
			"eventbus.driver=%q hanya untuk development (sinkron, tanpa durability/retry; env=%q butuh nats)",
			c.EventBus.Driver, c.Env))
	}

	// Driver KMS "local" memakai kunci turunan tetap yang ada di kode — cukup untuk dev/test,
	// TIDAK untuk data nyata. Ditolak di luar development (ADR-010), pola sama messaging=log.
	// Nama driver lain TIDAK divalidasi di sini: daftar driver hidup di registry infra/crypto
	// (config tak boleh tahu infra); typo gagal saat konstruksi provider — tetap saat boot.
	//
	// Driver KOSONG sengaja TIDAK ditolak di sini: selama belum ada kolom terenkripsi
	// (wiring repository = PR-3.8.3), memaksa tiap staging/production menyediakan master key
	// hanya akan menahan boot tanpa manfaat keamanan. Penolakan tetap ada, tapi di titik
	// pemakaian: crypto.NewFromConfig menolak driver kosong/local di luar development.
	if c.Crypto.KMSDriver == "local" && c.Env != "development" {
		errs = append(errs, fmt.Sprintf(
			"crypto.kms_driver=local hanya untuk development (kunci ada di kode; env=%q butuh static/KMS nyata)", c.Env))
	}
	// Driver "static" tanpa master key valid = tak bisa membungkus DEK. Gagal saat boot, bukan
	// saat baris pertama dienkripsi.
	if c.Crypto.KMSDriver == "static" {
		keys, err := c.Crypto.MasterKeys()
		switch {
		case err != nil:
			errs = append(errs, err.Error())
		case len(keys) == 0:
			errs = append(errs, "crypto.master_key wajib untuk kms_driver=static (base64 32-byte, dari secret store/env — jangan di-commit)")
		}
	}

	// Pagar ukuran token (ADR-020) yang disetel TERLALU KECIL memblokir SEMUA login — token
	// dasar tanpa satu pun role saja sudah 383 byte. Salah ketik satu digit ("614" alih-alih
	// "6144") karenanya menjatuhkan seluruh otentikasi, dan bentuk kegagalannya sama
	// membingungkannya dengan masalah yang pagar ini ada untuk mencegah. Tolak saat boot.
	switch {
	case c.Auth.TokenMaxBytes == 0: // 0 = pakai default aman adapter token; sah.
	case c.Auth.TokenMaxBytes < MinTokenMaxBytes:
		errs = append(errs, fmt.Sprintf(
			"auth.token_max_bytes=%d terlalu kecil (minimal %d; token tanpa role saja 383 byte, "+
				"nilai di bawah ini memblokir semua login) — 0 berarti pakai default aman",
			c.Auth.TokenMaxBytes, MinTokenMaxBytes))
	case c.Auth.TokenMaxBytes > MaxTokenMaxBytes:
		errs = append(errs, fmt.Sprintf(
			"auth.token_max_bytes=%d terlalu besar (maksimal %d): ambang ini juga menurunkan "+
				"MaxHeaderBytes, jadi nilai sebesar itu justru mengembalikan batas header ke "+
				"kelonggaran default Go 1 MiB — kebalikan dari tujuan pagar",
			c.Auth.TokenMaxBytes, MaxTokenMaxBytes))
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
		// Token internal HS256: tanpa secret kuat, token bisa dipalsukan (ADR-007).
		switch {
		case c.Auth.TokenSecret == "":
			errs = append(errs, "auth.token_secret wajib di production (kunci tanda tangan token internal)")
		case len(c.Auth.TokenSecret) < minTokenSecretLen:
			errs = append(errs, fmt.Sprintf("auth.token_secret minimal %d karakter untuk HS256", minTokenSecretLen))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("config tidak valid:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return nil
}
