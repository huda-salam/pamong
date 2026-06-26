//go:build integration

package db_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/huda-salam/pamong/core/config"
	infradb "github.com/huda-salam/pamong/infra/db"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestProvisioner_CreateDatabaseAndMigrate memverifikasi DoD PR-2.2.3: provisioning
// membuat tenant DB baru lalu menerapkan seluruh migrasi modul sehingga tenant baru
// otomatis punya schema lengkap. Memakai PAMONG_TEST_DB_DSN sebagai superuser (admin
// ber-CREATEDB + app user) — di test keduanya sama; di produksi terpisah (ADR-006).
func TestProvisioner_CreateDatabaseAndMigrate(t *testing.T) {
	dsn := os.Getenv("PAMONG_TEST_DB_DSN")
	if dsn == "" {
		t.Skip("PAMONG_TEST_DB_DSN tidak diset — lewati integration test")
	}
	ctx := context.Background()

	pc, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	cc := pc.ConnConfig
	host, port, user, pw, maint := cc.Host, int(cc.Port), cc.User, cc.Password, cc.Database

	admin := config.ProvisionDBConfig{User: user, Password: pw, Maintenance: maint}
	shared := config.DBConfig{Host: host, Port: port, User: user, Password: pw}

	dbName := fmt.Sprintf("pamong_prov_%d", time.Now().UnixNano())
	t.Cleanup(func() { dropDatabase(host, port, user, pw, maint, dbName) })

	migs, err := infradb.LoadMigrations(os.DirFS("../../modules"))
	if err != nil {
		t.Fatalf("muat migrasi: %v", err)
	}
	if len(migs) == 0 {
		t.Fatal("tidak ada migrasi modul ditemukan untuk diuji")
	}

	prov := infradb.NewProvisioner(admin, shared, migs)
	target := infradb.ProvisionTarget{Host: host, DBName: dbName}
	if err := prov.Provision(ctx, target); err != nil {
		t.Fatalf("provision: %v", err)
	}

	// Provisioning idempoten: panggil lagi tidak boleh error (DB sudah ada → skip create).
	if err := prov.Provision(ctx, target); err != nil {
		t.Fatalf("provision idempoten: %v", err)
	}

	// Connect ke tenant DB baru sebagai app user; pastikan schema lengkap ada.
	pool, err := infradb.New(ctx, config.DBConfig{Host: host, Port: port, Name: dbName, User: user, Password: pw})
	if err != nil {
		t.Fatalf("koneksi tenant DB baru: %v", err)
	}
	defer pool.Close()

	for _, rel := range []string{"gov.migration_history", "surat_masuk.surat_masuks"} {
		var reg *string
		if err := pool.QueryRow(ctx, `SELECT to_regclass($1)::text`, rel).Scan(&reg); err != nil {
			t.Fatalf("cek relasi %s: %v", rel, err)
		}
		if reg == nil {
			t.Fatalf("relasi %s tidak ada di tenant DB hasil provisioning", rel)
		}
	}

	// Migrasi tercatat di tenant DB.
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM gov.migration_history`).Scan(&n); err != nil {
		t.Fatalf("hitung migration_history: %v", err)
	}
	if n != len(migs) {
		t.Fatalf("migration_history berisi %d, mau %d", n, len(migs))
	}
}

// dropDatabase membersihkan tenant DB uji. FORCE memutus koneksi sisa (PG13+).
func dropDatabase(host string, port int, user, pw, maint, name string) {
	ctx := context.Background()
	pool, err := infradb.New(ctx, config.DBConfig{Host: host, Port: port, Name: maint, User: user, Password: pw})
	if err != nil {
		return
	}
	defer pool.Close()
	_, _ = pool.Exec(ctx, fmt.Sprintf(`DROP DATABASE IF EXISTS %q WITH (FORCE)`, name))
}
