package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"

	"gopkg.in/yaml.v3"
)

// loaderOpts mengatur sumber config. Default: dir "config", env dari GOV_ENV.
type loaderOpts struct {
	dir string
	env string
}

// Option menyesuaikan perilaku Load.
type Option func(*loaderOpts)

// WithDir menetapkan direktori file YAML config.
func WithDir(dir string) Option { return func(o *loaderOpts) { o.dir = dir } }

// WithEnv menetapkan nama environment (menentukan file {env}.yaml yang dimuat).
func WithEnv(env string) Option { return func(o *loaderOpts) { o.env = env } }

// Load membaca config berlapis dan mengembalikan AppConfig tervalidasi.
//
// Precedence (tinggi menang), sesuai CLAUDE.md:
//  1. Environment variable GOV_*        (paling tinggi)
//  2. File config/local.yaml            (tidak di-commit)
//  3. File config/{env}.yaml            (staging.yaml, production.yaml)
//  4. File config/default.yaml          (di-commit, nilai aman)
//
// Implementasi: lapis terendah dimuat dulu, lalu setiap lapis di atasnya menimpa
// field yang hadir. yaml.Unmarshal hanya menyetel field yang ada di dokumen,
// sehingga unmarshal berurutan menghasilkan overlay alami.
func Load(opts ...Option) (*AppConfig, error) {
	o := loaderOpts{dir: "config", env: os.Getenv("GOV_ENV")}
	for _, fn := range opts {
		fn(&o)
	}
	if o.env == "" {
		o.env = "development"
	}

	var cfg AppConfig

	// Lapis 4 → 2: default, {env}, local. Urut dari terendah agar yang atas menimpa.
	layers := []string{
		filepath.Join(o.dir, "default.yaml"),
		filepath.Join(o.dir, o.env+".yaml"),
		filepath.Join(o.dir, "local.yaml"),
	}
	for _, path := range layers {
		if err := overlayYAML(&cfg, path); err != nil {
			return nil, err
		}
	}

	// Lapis 1: environment variable menimpa segalanya.
	applyEnv(reflect.ValueOf(&cfg).Elem())

	// Env hasil merge belum diisi env var GOV_ENV? Pakai env yang dipilih loader.
	if cfg.Env == "" {
		cfg.Env = o.env
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// overlayYAML men-unmarshal file YAML ke cfg bila file ada. File yang tidak ada
// dilewati (bukan error) — hanya default.yaml yang biasanya selalu ada, sisanya opsional.
func overlayYAML(cfg *AppConfig, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // lapis opsional
		}
		return fmt.Errorf("baca config %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("parse config %s: %w", path, err)
	}
	return nil
}

// applyEnv menelusuri struct secara rekursif dan menimpa field yang punya tag `env`
// bila environment variable terkait di-set. Mendukung string, int, dan bool.
func applyEnv(v reflect.Value) {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		field := v.Field(i)
		if field.Kind() == reflect.Struct {
			applyEnv(field)
			continue
		}
		envKey := t.Field(i).Tag.Get("env")
		if envKey == "" {
			continue
		}
		raw, ok := os.LookupEnv(envKey)
		if !ok {
			continue
		}
		setField(field, raw)
	}
}

// setField menyetel field dari string env sesuai tipenya. Nilai yang tidak bisa
// di-parse (mis. int salah format) diabaikan diam-diam supaya satu env var rusak
// tidak menjatuhkan boot; Validate() yang menangkap nilai akhir yang tak valid.
func setField(field reflect.Value, raw string) {
	switch field.Kind() {
	case reflect.String:
		field.SetString(raw)
	case reflect.Int, reflect.Int64:
		if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
			field.SetInt(n)
		}
	case reflect.Bool:
		if b, err := strconv.ParseBool(raw); err == nil {
			field.SetBool(b)
		}
	}
}
