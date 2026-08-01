// Package cryptokit merakit port.CryptoPort NYATA (driver static + id.data_keys di Postgres)
// untuk integration test yang menyentuh kolom terenkripsi.
//
// Ia berdiri terpisah dari testkit root DENGAN SENGAJA: unit test infra/crypto mengimpor
// testkit root, jadi menaruh helper ini di sana akan menutup lingkaran import
// (testkit → infra/crypto → testkit).
//
// Kenapa bukan testkit.MockCrypto: yang diuji jalur clone justru SAMBUNGAN ke kripto nyata —
// format ciphertext, pengikatan baris, pemisahan realm, dan bahwa dump kolom benar-benar tak
// terbaca. Mock akan menjawab semuanya "lulus" tanpa satu pun dari itu terbukti.
package cryptokit

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/huda-salam/pamong/core/config"
	"github.com/huda-salam/pamong/infra/crypto"
	"github.com/huda-salam/pamong/infra/db"
)

// identityMigrations adalah migrasi minimum yang dibutuhkan hierarki kunci: schema id +
// registry tenant (custody) + tabel DEK. Urutannya bermakna.
var identityMigrations = []string{
	"001_create_identity.up.sql",
	"002_create_tenant_registry.up.sql",
	"007_create_data_keys.up.sql",
	"008_add_key_custody_tenant_registry.up.sql",
}

// NewService merakit crypto.Service di atas pool yang diberikan sebagai IDENTITY DB (tempat
// id.data_keys hidup, apa pun realm kuncinya) lalu mendaftarkan tenant yang disebut dengan
// custody 'platform'.
//
// Realm tenant WAJIB terdaftar: DBCustodyResolver fail-closed untuk identitas tak dikenal,
// jadi tenant yang lupa di-seed akan ditolak — bukan diam-diam jatuh ke platform. (Realm
// sentral tak perlu di-seed; custody-nya invarian kode lewat WithCentralRealm, ADR-017 §3.)
func NewService(t *testing.T, pool *db.Pool, tenants ...string) *crypto.Service {
	t.Helper()
	ctx := context.Background()

	for _, name := range identityMigrations {
		sql, err := os.ReadFile(filepath.Join(repoRoot(t), "identity", "migrations", name))
		if err != nil {
			t.Fatalf("cryptokit: baca migrasi %s: %v", name, err)
		}
		if _, err := pool.Exec(ctx, string(sql)); err != nil {
			t.Fatalf("cryptokit: apply migrasi %s: %v", name, err)
		}
	}
	for _, tenantID := range tenants {
		if _, err := pool.Exec(ctx, `INSERT INTO id.tenant_registry
			(tenant_id, nama, tier, db_host, db_name, key_custody)
			VALUES ($1, $1, 1, 'db', 'gov_test', 'platform')
			ON CONFLICT (tenant_id) DO NOTHING`, tenantID); err != nil {
			t.Fatalf("cryptokit: seed tenant %s: %v", tenantID, err)
		}
	}

	svc, err := crypto.NewFromConfig(&config.AppConfig{
		Env: "production", // driver static = jalur produksi Tier 1/2, bukan jalur dev
		Crypto: config.CryptoConfig{
			KMSDriver:   crypto.DriverStatic,
			MasterKey:   base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x5A}, 32)),
			DEKCacheTTL: time.Minute,
		},
	}, pool)
	if err != nil {
		t.Fatalf("cryptokit: crypto.NewFromConfig: %v", err)
	}
	return svc
}

// Cleanup membuang schema id yang dipasang NewService. Dipanggil pemakai lewat t.Cleanup bila
// DB uji dipakai bersama test lain.
func Cleanup(t *testing.T, pool *db.Pool) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `DROP SCHEMA IF EXISTS id CASCADE`); err != nil {
		t.Logf("cryptokit: bersihkan schema id: %v", err)
	}
}

// repoRoot menemukan akar repo dari lokasi file ini, bukan dari working directory pemanggil —
// sehingga helper ini bisa dipakai package mana pun tanpa menghitung "../.." sendiri.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cryptokit: tak bisa menemukan lokasi sumber")
	}
	return filepath.Join(filepath.Dir(file), "..", "..")
}
