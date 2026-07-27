package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/huda-salam/pamong/core/config"
)

// writeYAML menulis file YAML ke dir sementara untuk menguji pelapisan.
func writeYAML(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatalf("tulis %s: %v", name, err)
	}
}

// TestLoad_Precedence membuktikan urutan precedence: env > local > {env}-file > default.
// Satu field (db.host) di-set berbeda di tiap lapis, lalu diperiksa siapa yang menang.
func TestLoad_Precedence(t *testing.T) {
	dir := t.TempDir()

	// db.host & db.port berbeda di tiap lapis; field lain untuk memastikan merge tidak menghapus.
	writeYAML(t, dir, "default.yaml", "env: development\ndb:\n  host: from-default\n  port: 1111\n  name: pamong\n")
	writeYAML(t, dir, "staging.yaml", "db:\n  host: from-envfile\n  port: 2222\n")
	writeYAML(t, dir, "local.yaml", "db:\n  host: from-local\n")

	t.Run("default menang bila hanya default ada", func(t *testing.T) {
		d2 := t.TempDir()
		writeYAML(t, d2, "default.yaml", "env: development\ndb:\n  host: only-default\n")
		cfg, err := config.Load(config.WithDir(d2), config.WithEnv("development"))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.DB.Host != "only-default" {
			t.Errorf("db.host = %q, mau only-default", cfg.DB.Host)
		}
	})

	t.Run("env-file menimpa default", func(t *testing.T) {
		// Tanpa local.yaml di dir ini.
		d2 := t.TempDir()
		writeYAML(t, d2, "default.yaml", "env: development\ndb:\n  host: from-default\n  port: 1111\n")
		writeYAML(t, d2, "staging.yaml", "db:\n  host: from-envfile\n")
		cfg, err := config.Load(config.WithDir(d2), config.WithEnv("staging"))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.DB.Host != "from-envfile" {
			t.Errorf("db.host = %q, mau from-envfile", cfg.DB.Host)
		}
		// port tidak di-override env-file → tetap dari default (merge, bukan replace).
		if cfg.DB.Port != 1111 {
			t.Errorf("db.port = %d, mau 1111 (warisan default)", cfg.DB.Port)
		}
	})

	t.Run("local menimpa env-file dan default", func(t *testing.T) {
		cfg, err := config.Load(config.WithDir(dir), config.WithEnv("staging"))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.DB.Host != "from-local" {
			t.Errorf("db.host = %q, mau from-local", cfg.DB.Host)
		}
		// port dari env-file (staging), tidak ada di local → tetap 2222.
		if cfg.DB.Port != 2222 {
			t.Errorf("db.port = %d, mau 2222 (warisan env-file)", cfg.DB.Port)
		}
	})

	t.Run("env var menimpa semua lapis file", func(t *testing.T) {
		t.Setenv("GOV_DB_HOST", "from-env")
		t.Setenv("GOV_DB_PORT", "9999")
		cfg, err := config.Load(config.WithDir(dir), config.WithEnv("staging"))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.DB.Host != "from-env" {
			t.Errorf("db.host = %q, mau from-env (env var menang atas local)", cfg.DB.Host)
		}
		if cfg.DB.Port != 9999 {
			t.Errorf("db.port = %d, mau 9999", cfg.DB.Port)
		}
	})
}

// TestLoad_EnvVarTipe memastikan parsing int & bool dari env var benar.
func TestLoad_EnvVarTipe(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "default.yaml", "env: development\n")
	t.Setenv("GOV_RATELIMIT_ENABLED", "true")
	t.Setenv("GOV_RATELIMIT_RPS", "250")

	cfg, err := config.Load(config.WithDir(dir), config.WithEnv("development"))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.RateLimit.Enabled {
		t.Error("ratelimit.enabled harus true dari env var")
	}
	if cfg.RateLimit.RPS != 250 {
		t.Errorf("ratelimit.rps = %d, mau 250", cfg.RateLimit.RPS)
	}
}

// TestLoad_SliceEnvVar memastikan []string bisa di-override env var sebagai daftar dipisah
// koma, dengan trim & pembuangan elemen kosong (dipakai GOV_CORS_ALLOWED_ORIGINS).
func TestLoad_SliceEnvVar(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "default.yaml", "env: development\n")
	t.Setenv("GOV_CORS_ALLOWED_ORIGINS", "https://a.go.id, https://b.go.id ,, https://c.go.id")

	cfg, err := config.Load(config.WithDir(dir), config.WithEnv("development"))
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.CORS.AllowedOrigins
	want := []string{"https://a.go.id", "https://b.go.id", "https://c.go.id"}
	if len(got) != len(want) {
		t.Fatalf("cors.allowed_origins = %v, mau %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("cors.allowed_origins[%d] = %q, mau %q", i, got[i], want[i])
		}
	}
}

// TestLoad_SliceYAML memastikan []string terbaca dari YAML (bukan hanya env).
func TestLoad_SliceYAML(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "default.yaml", "env: development\ncors:\n  allowed_origins:\n    - https://admin.go.id\n")

	cfg, err := config.Load(config.WithDir(dir), config.WithEnv("development"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.CORS.AllowedOrigins) != 1 || cfg.CORS.AllowedOrigins[0] != "https://admin.go.id" {
		t.Errorf("cors.allowed_origins = %v, mau [https://admin.go.id]", cfg.CORS.AllowedOrigins)
	}
}

// TestLoad_ValidasiGagal memastikan config tidak valid ditolak (gagal cepat saat boot).
func TestLoad_ValidasiGagal(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "default.yaml", "env: tidak-dikenal\n")

	_, err := config.Load(config.WithDir(dir), config.WithEnv("tidak-dikenal"))
	if err == nil {
		t.Fatal("env tidak valid harus ditolak Validate()")
	}
}

// TestLoad_ProductionWajibKredensial memastikan production menolak config tanpa
// kredensial koneksi sentral (identity DB + default tenant DB) — ADR-004.
func TestLoad_ProductionWajibKredensial(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "production.yaml", "env: production\n")

	_, err := config.Load(config.WithDir(dir), config.WithEnv("production"))
	if err == nil {
		t.Fatal("production tanpa kredensial identity_db & db harus ditolak")
	}
}

// TestLoad_ProductionMessagingLogDitolak memastikan driver messaging "log" (yang mencatat
// body OTP ke log) ditolak di production — kredensial lain diisi agar hanya aturan ini yang gagal.
func TestLoad_ProductionMessagingLogDitolak(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "production.yaml", `env: production
identity_db:
  host: db
  user: app
db:
  host: db
  user: app
auth:
  token_secret: "0123456789012345678901234567890123"
messaging:
  driver: log
`)

	_, err := config.Load(config.WithDir(dir), config.WithEnv("production"))
	if err == nil {
		t.Fatal("messaging.driver=log di production harus ditolak Validate()")
	}
}

// TestLoad_StagingMessagingLogDitolak memastikan driver "log" juga ditolak di staging (bukan
// hanya production) — staging memakai data mirip-nyata, OTP tak boleh bocor ke log di sana.
func TestLoad_StagingMessagingLogDitolak(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "staging.yaml", "env: staging\nmessaging:\n  driver: log\n")

	_, err := config.Load(config.WithDir(dir), config.WithEnv("staging"))
	if err == nil {
		t.Fatal("messaging.driver=log di staging harus ditolak Validate()")
	}
}
