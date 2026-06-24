package main

import (
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/tools/go/analysis/multichecker"

	"github.com/huda-salam/pamong/tools/linter"
)

// lintCmd menjalankan seluruh custom analyzer Pamong (tools/linter) terhadap path
// yang diberikan. Berbeda dari perintah lain, ini SUDAH berfungsi penuh (PR-0.3.2).
//
// multichecker.Main membaca os.Args sendiri dan memanggil os.Exit dengan kode hasil
// analisis, jadi perintah ini terminal — tidak ada yang berjalan setelahnya.
func lintCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "lint [path...]",
		Short: "Jalankan semua custom analyzer Pamong (aturan hexagonal, dll)",
		Long: "Menjalankan analyzer di tools/linter terhadap path yang diberikan " +
			"(mis. ./...). Keluar dengan kode non-zero bila ada pelanggaran.",
		// Biarkan multichecker yang memproses argumen & flag analyzer.
		DisableFlagParsing: true,
		Run: func(cmd *cobra.Command, args []string) {
			if len(args) == 0 {
				args = []string{"./..."}
			}
			// multichecker membaca pola paket dari os.Args[1:].
			os.Args = append([]string{"pamongctl-lint"}, args...)
			multichecker.Main(linter.All...)
		},
	}
}
