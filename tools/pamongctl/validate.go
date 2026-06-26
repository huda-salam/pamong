package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/huda-salam/pamong/core/domain"
	"github.com/huda-salam/pamong/modules"
)

// validateCmd menegakkan invariant lintas-modul saat development (Lapis 1 enforcement):
// nama unik, DependsOn membentuk DAG, entity & nama tabel valid, serta kontrak
// export/import permission antar modul (PR-2.3.4). Ini bentuk "registrasi saat
// bootstrap" yang berjalan lebih awal — gagal cepat sebelum binary dijalankan.
func validateCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "validate",
		Short: "Validasi manifest, dependency DAG, permission, dan event schema",
	}
	c.AddCommand(&cobra.Command{
		Use:   "modules",
		Short: "Validasi semua manifest modul & deteksi siklus dependency",
		RunE: func(cmd *cobra.Command, args []string) error {
			reg := domain.NewRegistry()
			reg.Register(modules.All()...)
			if err := reg.Validate(); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "OK: %d modul valid\n", len(reg.Modules()))
			return nil
		},
	})
	return c
}
