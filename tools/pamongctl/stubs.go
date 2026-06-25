package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// notImplemented adalah respons baku untuk perintah yang kerangkanya sudah ada tapi
// implementasinya dijadwalkan di PR berikutnya. Eksplisit menyebut PR agar tidak
// terlihat seperti bug — sesuai konvensi TODO ber-referensi PR (CODE_CONVENTION #9).
func notImplemented(cmd *cobra.Command, pr string) error {
	fmt.Fprintf(cmd.OutOrStdout(), "perintah %q belum diimplementasi (dijadwalkan %s)\n", cmd.CommandPath(), pr)
	return nil
}

// validateCmd: validasi manifest, DAG dependency, permission, event schema. PR-1.1.1 / PR-5.2.2.
func validateCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "validate",
		Short: "Validasi manifest, dependency DAG, permission, dan event schema",
	}
	c.AddCommand(&cobra.Command{
		Use:   "modules",
		Short: "Validasi semua manifest modul & deteksi siklus dependency",
		RunE: func(cmd *cobra.Command, args []string) error {
			return notImplemented(cmd, "PR-1.1.1")
		},
	})
	return c
}

// migrateCmd: jalankan migrasi per-tenant. PR-1.2.3.
