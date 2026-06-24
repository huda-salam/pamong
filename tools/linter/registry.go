// Package linter mendaftarkan semua analyzer GovFramework menjadi satu binary.
//
// pamongctl lint memanggil multichecker.Main dengan daftar Analyzer ini, sehingga
// satu perintah menjalankan seluruh rule. Menambah rule baru = tambahkan satu
// baris di slice All — itulah kenapa tiap rule berdiri sendiri sebagai package.
package linter

import (
	"golang.org/x/tools/go/analysis"

	"github.com/huda-salam/pamong/tools/linter/rules/domainnoinfra"
	// Tambahkan rule lain di sini saat dibuat:
	// "github.com/huda-salam/pamong/tools/linter/rules/handlerpermission"
	// "github.com/huda-salam/pamong/tools/linter/rules/eventconst"
	// "github.com/huda-salam/pamong/tools/linter/rules/permissionregistered"
	// "github.com/huda-salam/pamong/tools/linter/rules/configdirectenv"
	// "github.com/huda-salam/pamong/tools/linter/rules/rawsqlannotate"
)

// All adalah seluruh analyzer GovFramework. Dipakai oleh pamongctl lint dan oleh CI.
var All = []*analysis.Analyzer{
	domainnoinfra.Analyzer,
	// handlerpermission.Analyzer,
	// eventconst.Analyzer,
	// ...
}
