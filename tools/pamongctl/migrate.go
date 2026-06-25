package main

import (
	"context"
	"fmt"
	"os"

	"github.com/huda-salam/pamong/core/config"
	"github.com/huda-salam/pamong/infra/db"
	"github.com/spf13/cobra"
)

// migrateCmd: jalankan migrasi per-tenant (PR-1.2.3). Definisi migrasi dibaca dari
// modules/{modul}/migrations/*.sql; tracking di gov.migration_history pada DB tenant
// yang ditunjuk konfigurasi (GOV_DB_*).
func migrateCmd() *cobra.Command {
	var modulesDir string
	c := &cobra.Command{
		Use:   "migrate",
		Short: "Jalankan migrasi database (up/down/status), multi-tenant aware",
	}
	c.PersistentFlags().StringVar(&modulesDir, "modules", "modules", "direktori akar modul")

	c.AddCommand(
		&cobra.Command{
			Use:   "up",
			Short: "Terapkan semua migrasi yang belum jalan",
			RunE:  func(cmd *cobra.Command, _ []string) error { return runUp(cmd, modulesDir) },
		},
		&cobra.Command{
			Use:   "down",
			Short: "Rollback satu migrasi terakhir",
			RunE:  func(cmd *cobra.Command, _ []string) error { return runDown(cmd, modulesDir) },
		},
		&cobra.Command{
			Use:   "status",
			Short: "Tampilkan status tiap migrasi (applied/pending)",
			RunE:  func(cmd *cobra.Command, _ []string) error { return runStatus(cmd, modulesDir) },
		},
	)
	return c
}

// openMigrator memuat config, koneksi DB tenant, dan migrasi dari disk.
func openMigrator(ctx context.Context, modulesDir string) (*db.Pool, *db.Migrator, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, fmt.Errorf("muat config: %w", err)
	}
	migs, err := db.LoadMigrations(os.DirFS(modulesDir))
	if err != nil {
		return nil, nil, fmt.Errorf("muat migrasi dari %s: %w", modulesDir, err)
	}
	pool, err := db.New(ctx, cfg.DB)
	if err != nil {
		return nil, nil, fmt.Errorf("koneksi DB: %w", err)
	}
	return pool, db.NewMigrator(pool, migs), nil
}

func runUp(cmd *cobra.Command, modulesDir string) error {
	ctx := cmd.Context()
	pool, m, err := openMigrator(ctx, modulesDir)
	if err != nil {
		return err
	}
	defer pool.Close()

	done, err := m.Up(ctx)
	if err != nil {
		return err
	}
	if len(done) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "tidak ada migrasi baru — database sudah mutakhir")
		return nil
	}
	for _, mig := range done {
		fmt.Fprintf(cmd.OutOrStdout(), "applied  %s:%s %s\n", mig.Module, mig.Version, mig.Name)
	}
	return nil
}

func runDown(cmd *cobra.Command, modulesDir string) error {
	ctx := cmd.Context()
	pool, m, err := openMigrator(ctx, modulesDir)
	if err != nil {
		return err
	}
	defer pool.Close()

	mig, err := m.Down(ctx)
	if err != nil {
		return err
	}
	if mig == nil {
		fmt.Fprintln(cmd.OutOrStdout(), "tidak ada migrasi yang bisa di-rollback")
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "rolled back  %s:%s %s\n", mig.Module, mig.Version, mig.Name)
	return nil
}

func runStatus(cmd *cobra.Command, modulesDir string) error {
	ctx := cmd.Context()
	pool, m, err := openMigrator(ctx, modulesDir)
	if err != nil {
		return err
	}
	defer pool.Close()

	st, err := m.Status(ctx)
	if err != nil {
		return err
	}
	for _, s := range st {
		mark := "pending"
		if s.Applied {
			mark = "applied"
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%-8s %s:%s %s\n", mark, s.Module, s.Version, s.Name)
	}
	return nil
}
