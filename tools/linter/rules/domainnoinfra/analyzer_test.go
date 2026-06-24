package domainnoinfra_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/huda-salam/pamong/tools/linter/rules/domainnoinfra"
)

// TestAnalyzer menjalankan analyzer terhadap paket testdata.
//
// analysistest membaca komentar `// want "..."` di file testdata sebagai
// ekspektasi diagnostic. Jika analyzer melaporkan diagnostic di posisi yang
// punya komentar want, dan teksnya cocok regex, test lulus. Diagnostic tak
// terduga (false positive) maupun want yang tak terpenuhi (false negative)
// keduanya menggagalkan test.
//
// Struktur testdata:
//
//	testdata/src/clean/domain   → tidak boleh ada diagnostic (kasus negatif)
//	testdata/src/dirty/domain   → harus ada diagnostic (kasus positif)
//	testdata/src/dirty/usecase  → harus ada diagnostic (lapisan usecase juga domain)
func TestAnalyzer(t *testing.T) {
	testdata := analysistest.TestData()

	t.Run("clean domain tidak memicu diagnostic", func(t *testing.T) {
		analysistest.Run(t, testdata, domainnoinfra.Analyzer, "clean/domain")
	})

	t.Run("dirty domain memicu diagnostic", func(t *testing.T) {
		analysistest.Run(t, testdata, domainnoinfra.Analyzer, "dirty/domain")
	})

	t.Run("dirty usecase memicu diagnostic", func(t *testing.T) {
		analysistest.Run(t, testdata, domainnoinfra.Analyzer, "dirty/usecase")
	})
}
