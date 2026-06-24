// Command pamongctl adalah toolchain developer Pamong: scaffolding, validasi,
// generate migrasi, lint, dan migrasi. CLI ini adalah Lapis 1 enforcement
// (CODING_PHILOSOPHY #1): pelanggaran dicegah dengan menghasilkan struktur yang benar.
//
// Tahap ini (PR-0.3.1) baru kerangka: setiap perintah terdaftar dan tampil di --help.
// Implementasi tiap perintah menyusul di PR yang relevan (lihat ROADMAP).
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "pamongctl",
		Short: "Toolchain developer framework Pamong",
		Long: "pamongctl menegakkan konvensi Pamong sejak lapis paling awal (scaffolding) " +
			"dan mengotomasi operasi berulang: scaffold modul/entity, validasi manifest, " +
			"generate migrasi, lint, dan menjalankan migrasi.",
		SilenceUsage: true,
	}
	root.AddCommand(
		newCmd(),
		validateCmd(),
		generateCmd(),
		lintCmd(),
		migrateCmd(),
	)
	return root
}
