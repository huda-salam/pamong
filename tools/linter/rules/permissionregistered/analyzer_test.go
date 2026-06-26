package permissionregistered_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/huda-salam/pamong/tools/linter/rules/permissionregistered"
)

// TestAnalyzer menjalankan analyzer terhadap paket testdata.
//
// analysistest membaca komentar `// want "..."` sebagai ekspektasi diagnostic.
// Owner modul tiap fixture = nama direktori root (good/bad), dan allow-list dibaca
// dari manifest.go masing-masing root.
//
// Fixture diletakkan di bawah src/modules/ karena rule hanya berlaku untuk modul
// bisnis (anak langsung direktori "modules").
//
// Struktur testdata:
//
//	src/modules/good/manifest.go   → meng-import "other:thing:baca"
//	src/modules/good/usecase       → pakai permission sendiri + yang di-import → tak ada diagnostic
//	src/modules/bad/manifest.go    → tanpa Imports
//	src/modules/bad/usecase        → pakai "other:thing:baca" tak terdaftar → ada diagnostic
func TestAnalyzer(t *testing.T) {
	testdata := analysistest.TestData()

	t.Run("permission sendiri & yang di-import tidak memicu diagnostic", func(t *testing.T) {
		analysistest.Run(t, testdata, permissionregistered.Analyzer, "modules/good/usecase")
	})

	t.Run("permission modul lain tanpa Imports memicu diagnostic", func(t *testing.T) {
		analysistest.Run(t, testdata, permissionregistered.Analyzer, "modules/bad/usecase")
	})
}
