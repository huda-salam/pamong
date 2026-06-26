package db

import (
	"context"
	"fmt"
	"strings"

	"github.com/huda-salam/pamong/core/config"
)

// Provisioner membuat tenant DB baru lalu menerapkan seluruh migrasi modul ke dalamnya
// (PR-2.2.3). Privilege boundary mengikuti ADR-006: `CREATE DATABASE` dijalankan dengan
// kredensial ADMIN terpisah (ber-CREATEDB), tetapi database dibuat dengan OWNER =
// kredensial runtime app, dan migrasi dijalankan SEBAGAI app user. Dengan begitu koneksi
// runtime tidak pernah butuh privilege global CREATEDB, namun tetap memiliki penuh objek
// di DB tenant-nya sendiri.
//
// Murni infra: tidak import identity/. Caller (pamongctl) membaca lokasi tenant dari
// registry lalu menyerahkannya sebagai ProvisionTarget.
type Provisioner struct {
	admin      config.ProvisionDBConfig
	shared     config.DBConfig // kredensial runtime app (owner + pelaksana migrasi)
	migrations []Migration
	open       opener // injectable untuk test; default openPool
}

// NewProvisioner membuat provisioner. `admin` = kredensial provisioning (ADR-006),
// `shared` = kredensial runtime app (GOV_DB_*), `migrations` = hasil LoadMigrations.
func NewProvisioner(admin config.ProvisionDBConfig, shared config.DBConfig, migrations []Migration) *Provisioner {
	return &Provisioner{admin: admin, shared: shared, migrations: migrations, open: openPool}
}

// ProvisionTarget adalah lokasi tenant DB yang akan dibuat (dari id.tenant_registry).
type ProvisionTarget struct {
	Host   string
	DBName string
}

// Nama database/owner divalidasi terhadap identRe (pola identifier aman, dideklarasikan
// di repository.go). Provisioning menerima nama dari registry (bukan input bebas), tapi
// validasi + quoting adalah pertahanan berlapis terhadap SQL injection pada DDL yang
// tidak bisa diparametrikan.

// Provision: connect maintenance DB (kredensial admin) → CREATE DATABASE OWNER app
// (idempoten) → connect DB baru sebagai app → jalankan migrasi. DoD: tenant baru otomatis
// punya schema lengkap (gov.* + schema tiap modul).
func (p *Provisioner) Provision(ctx context.Context, t ProvisionTarget) error {
	if !identRe.MatchString(t.DBName) {
		return fmt.Errorf("nama database tidak valid untuk provisioning: %q", t.DBName)
	}
	if p.admin.User == "" {
		return fmt.Errorf("kredensial provisioning belum dikonfigurasi (GOV_PROVISION_DB_USER)")
	}
	owner := p.shared.User
	if !identRe.MatchString(owner) {
		return fmt.Errorf("owner (db.user) tidak valid untuk provisioning: %q", owner)
	}

	if err := p.createDatabase(ctx, t, owner); err != nil {
		return err
	}
	return p.migrate(ctx, t)
}

// createDatabase membuat DB baru memakai kredensial admin, idempoten (skip bila sudah ada).
func (p *Provisioner) createDatabase(ctx context.Context, t ProvisionTarget, owner string) error {
	maint := p.admin.Maintenance
	if maint == "" {
		maint = "postgres"
	}
	adminPool, err := p.open(ctx, connParams{
		Host: t.Host, Port: p.shared.Port, Name: maint,
		User: p.admin.User, Password: p.admin.Password,
	})
	if err != nil {
		return fmt.Errorf("koneksi admin ke %s: %w", maint, err)
	}
	defer adminPool.Close()

	var exists bool
	if err := adminPool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)`, t.DBName,
	).Scan(&exists); err != nil {
		return fmt.Errorf("cek database ada: %w", err)
	}
	if exists {
		return nil // idempoten: DB sudah ada, migrasi tetap dijalankan caller
	}

	// CREATE DATABASE tidak bisa diparametrikan & tidak boleh dalam transaksi; identifier
	// sudah divalidasi identRe lalu di-quote.
	ddl := fmt.Sprintf(`CREATE DATABASE %s OWNER %s`, quoteIdent(t.DBName), quoteIdent(owner))
	if _, err := adminPool.Exec(ctx, ddl); err != nil {
		return fmt.Errorf("CREATE DATABASE %s: %w", t.DBName, err)
	}
	return nil
}

// migrate menerapkan seluruh migrasi modul ke tenant DB sebagai app user (owner).
func (p *Provisioner) migrate(ctx context.Context, t ProvisionTarget) error {
	tenantPool, err := p.open(ctx, connParams{
		Host: t.Host, Port: p.shared.Port, Name: t.DBName,
		User: p.shared.User, Password: p.shared.Password,
		PoolMax: p.shared.PoolMax, PoolIdle: p.shared.PoolIdle,
	})
	if err != nil {
		return fmt.Errorf("koneksi tenant DB %s: %w", t.DBName, err)
	}
	defer tenantPool.Close()

	if _, err := NewMigrator(tenantPool, p.migrations).Up(ctx); err != nil {
		return fmt.Errorf("migrasi tenant DB %s: %w", t.DBName, err)
	}
	return nil
}

// quoteIdent membungkus identifier Postgres dengan tanda kutip ganda, meng-escape kutip
// internal. Dipakai hanya setelah identRe lolos.
func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}
