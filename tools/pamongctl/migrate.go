package main

import (
	"context"
	"fmt"
	"os"

	"github.com/huda-salam/pamong/core/config"
	"github.com/huda-salam/pamong/infra/db"
	"github.com/huda-salam/pamong/infra/schema"
	"github.com/spf13/cobra"
)

// migrateCmd: jalankan migrasi (PR-1.2.3). Dua JALUR residensi, dipilih flag --central:
//
//   - default (tenant): migrasi modul (modules/{modul}/migrations/*.sql) + komponen core
//     ber-residensi tenant, diterapkan ke DB tenant yang ditunjuk konfigurasi (GOV_DB_*).
//   - --central: HANYA komponen core ber-residensi sentral (ADR-023 — scheduler), diterapkan
//     ke DB sentral (GOV_CENTRAL_DB_*, jatuh ke identity DB bila belum dipisah).
//
// Keduanya melacak di gov.migration_history pada DB masing-masing. Jalur terpisah adalah satu-
// satunya yang mencegah tabel sentral ikut dibuat di tiap tenant DB — nama schema kedua jalur
// sama-sama `gov`, jadi tak ada yang akan error bila keliru.
func migrateCmd() *cobra.Command {
	var modulesDir string
	var central bool
	c := &cobra.Command{
		Use:   "migrate",
		Short: "Jalankan migrasi database (up/down/status), multi-tenant aware",
	}
	c.PersistentFlags().StringVar(&modulesDir, "modules", "modules", "direktori akar modul")
	c.PersistentFlags().BoolVar(&central, "central", false,
		"terapkan migrasi ber-residensi SENTRAL (ADR-023) ke DB sentral, bukan migrasi tenant")

	c.AddCommand(
		&cobra.Command{
			Use:   "up",
			Short: "Terapkan semua migrasi yang belum jalan",
			RunE:  func(cmd *cobra.Command, _ []string) error { return runUp(cmd, modulesDir, central) },
		},
		&cobra.Command{
			Use:   "down",
			Short: "Rollback satu migrasi terakhir",
			RunE:  func(cmd *cobra.Command, _ []string) error { return runDown(cmd, modulesDir, central) },
		},
		&cobra.Command{
			Use:   "status",
			Short: "Tampilkan status tiap migrasi (applied/pending)",
			RunE:  func(cmd *cobra.Command, _ []string) error { return runStatus(cmd, modulesDir, central) },
		},
	)
	return c
}

// openMigrator memuat config, membuka pool DB yang sesuai jalur residensi, dan mengumpulkan
// migrasi yang boleh diterapkan ke DB itu. Pemilihan migrasi dan pemilihan DB terjadi di SATU
// tempat, sengaja: memisahnya membuat mungkin menerapkan daftar tenant ke DB sentral.
func openMigrator(ctx context.Context, modulesDir string, central bool) (*db.Pool, *db.Migrator, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, fmt.Errorf("muat config: %w", err)
	}
	if central {
		// Jalur sentral: hanya komponen core ber-residensi sentral (ADR-023). Modul bisnis
		// TIDAK ikut — seluruh entity modul ber-residensi tenant.
		migs, err := schema.CentralMigrations()
		if err != nil {
			return nil, nil, err
		}
		pool, err := db.NewCentral(ctx, cfg.CentralDBResolved())
		if err != nil {
			return nil, nil, fmt.Errorf("koneksi DB sentral: %w", err)
		}
		return pool, db.NewMigrator(pool, migs), nil
	}

	migs, err := db.LoadMigrations(os.DirFS(modulesDir))
	if err != nil {
		return nil, nil, fmt.Errorf("muat migrasi dari %s: %w", modulesDir, err)
	}
	// Migrasi komponen framework (core/*) di-embed — tanpa ini hanya modules/* yang jalan.
	core, err := schema.CoreMigrations()
	if err != nil {
		return nil, nil, err
	}
	migs = append(migs, core...)
	pool, err := db.New(ctx, cfg.DB)
	if err != nil {
		return nil, nil, fmt.Errorf("koneksi DB: %w", err)
	}
	return pool, db.NewMigrator(pool, migs), nil
}

func runUp(cmd *cobra.Command, modulesDir string, central bool) error {
	ctx := cmd.Context()
	pool, m, err := openMigrator(ctx, modulesDir, central)
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

func runDown(cmd *cobra.Command, modulesDir string, central bool) error {
	ctx := cmd.Context()
	pool, m, err := openMigrator(ctx, modulesDir, central)
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

func runStatus(cmd *cobra.Command, modulesDir string, central bool) error {
	ctx := cmd.Context()
	pool, m, err := openMigrator(ctx, modulesDir, central)
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
