package main

import (
	"context"
	"fmt"
	"os"

	"github.com/huda-salam/pamong/core/config"
	identitydb "github.com/huda-salam/pamong/identity/adapter/db"
	"github.com/huda-salam/pamong/infra/db"
	"github.com/spf13/cobra"
)

// tenantCmd: manajemen tenant. PR-2.2.3 mengisi `provision` (schema-per-tenant).
func tenantCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "tenant",
		Short: "Manajemen tenant (provisioning schema, tier)",
	}
	c.AddCommand(tenantProvisionCmd())
	return c
}

// tenantProvisionCmd: buat tenant DB + jalankan seluruh migrasi modul ke dalamnya.
// Lokasi tenant dibaca dari id.tenant_registry (identity DB); CREATE DATABASE memakai
// kredensial admin terpisah (ADR-006).
func tenantProvisionCmd() *cobra.Command {
	var tenantID, modulesDir string
	c := &cobra.Command{
		Use:   "provision",
		Short: "Buat tenant DB dari registry & jalankan migrasi (schema lengkap)",
		RunE:  func(cmd *cobra.Command, _ []string) error { return runProvision(cmd, tenantID, modulesDir) },
	}
	c.Flags().StringVar(&tenantID, "tenant", "", "tenant_id yang akan di-provision (wajib)")
	c.Flags().StringVar(&modulesDir, "modules", "modules", "direktori akar modul")
	return c
}

func runProvision(cmd *cobra.Command, tenantID, modulesDir string) error {
	if tenantID == "" {
		return fmt.Errorf("tenant tidak ditentukan: pakai --tenant")
	}
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("muat config: %w", err)
	}

	// Baca lokasi tenant dari registry sentral (identity DB).
	idPool, err := db.NewIdentity(ctx, cfg.IdentityDB)
	if err != nil {
		return fmt.Errorf("koneksi identity DB: %w", err)
	}
	defer idPool.Close()

	t, err := identitydb.NewTenantRepo(idPool).FindByID(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("baca registry tenant: %w", err)
	}

	migs, err := db.LoadMigrations(os.DirFS(modulesDir))
	if err != nil {
		return fmt.Errorf("muat migrasi dari %s: %w", modulesDir, err)
	}

	prov := db.NewProvisioner(cfg.ProvisionDB, cfg.DB, migs)
	if err := prov.Provision(ctx, db.ProvisionTarget{Host: t.DBHost, DBName: t.DBName}); err != nil {
		return fmt.Errorf("provisioning tenant %s: %w", tenantID, err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "tenant %s ter-provision: %s@%s (%d migrasi modul diterapkan)\n",
		tenantID, t.DBName, t.DBHost, len(migs))
	return nil
}
