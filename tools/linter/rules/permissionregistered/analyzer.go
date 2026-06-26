// Package permissionregistered menegakkan aturan permission-must-be-registered
// (CLAUDE.md "CI/CD gates → Linter rules"): kode sebuah modul tidak boleh memakai
// string permission milik modul lain kecuali permission itu terdaftar di bagian
// Imports manifest modul tersebut.
//
// Mengapa rule ini berbentuk "deteksi string literal foreign-prefix":
//   - Konvensi #8 mewajibkan permission selalu dirujuk lewat konstanta, tak pernah
//     string literal mentah; dan rule no-cross-module-import melarang sebuah modul
//     mengimpor paket Go modul lain. Konsekuensinya, permission modul SENDIRI dipakai
//     lewat konstanta lokal, sedangkan permission modul LAIN tidak bisa dirujuk lewat
//     konstanta (paket asalnya tak boleh diimpor) — satu-satunya cara merujuknya adalah
//     menulis string-nya. Maka pemakaian permission lintas-modul = string literal
//     ber-prefix modul lain. Inilah yang dideteksi rule ini.
//
// Rule ini hanya berlaku untuk modul bisnis, yaitu direktori anak langsung dari
// `modules/` yang memuat manifest.go. Lapisan core/identity/tenantrole bukan modul
// pluggable dan di luar scope (mendefinisikan permission framework dengan prefiks
// beragam secara sah).
//
// Cara kerja:
//   - Owner modul = nama direktori root modul (dir anak langsung `modules/` yang
//     memuat manifest.go), mengikuti konvensi "nama paket/modul = nama direktori".
//   - Allow-list = semua string ber-format permission yang muncul di manifest.go modul
//     itu. Karena permission modul sendiri ditulis sebagai konstanta (bukan literal),
//     satu-satunya literal permission yang sah ada di manifest.go adalah entri Imports.
//   - Setiap string literal ber-format permission di kode modul yang prefiks-nya BUKAN
//     owner DAN tidak ada di allow-list → dilaporkan.
//
// Pasangan boot-time-nya adalah core/domain Registry.Validate (validatePermissions),
// yang memastikan deklarasi Imports menunjuk modul terdaftar yang benar-benar
// meng-export permission tersebut. Rule ini menjaga sisi penggunaan kode; Validate
// menjaga koherensi deklarasinya.
//
// Pola implementasi mengikuti referensi domainnoinfra: satu paket per rule, satu
// Analyzer, test memakai analysistest.
package permissionregistered

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// Analyzer adalah entry point yang didaftarkan ke pamongctl lint.
var Analyzer = &analysis.Analyzer{
	Name: "permissionregistered",
	Doc:  "memastikan modul tidak memakai permission modul lain tanpa mendaftarkannya di Imports manifest",
	Run:  run,
}

// permRe mencocokkan string ber-format permission: tiga segmen snake_case dipisah
// titik dua ({modul}:{entity}:{aksi}). Titik dua membedakannya dari nama event yang
// memakai titik. Lowercase + snake mengikuti konvensi penamaan permission.
var permRe = regexp.MustCompile(`^[a-z][a-z0-9_]*:[a-z][a-z0-9_]*:[a-z][a-z0-9_]*$`)

func run(pass *analysis.Pass) (interface{}, error) {
	// Lewati fixture testdata saat lint dijalankan atas seluruh repo: itu sengaja
	// melanggar untuk menguji analyzer, bukan kode produksi. (analysistest memakai
	// import path tanpa "/testdata/" sehingga test tetap berjalan.)
	if strings.Contains(pass.Pkg.Path(), "/testdata/") {
		return nil, nil
	}
	if len(pass.Files) == 0 {
		return nil, nil
	}

	// Semua file dalam satu paket berbagi direktori → cukup tentukan root modul sekali.
	dir := filepath.Dir(pass.Fset.File(pass.Files[0].Pos()).Name())
	root, ok := moduleRoot(dir)
	if !ok {
		// Bukan kode di dalam modul bisnis (tak ada modules/<x>/manifest.go di atasnya).
		return nil, nil
	}
	owner := filepath.Base(root)
	allow := importedPermissions(filepath.Join(root, "manifest.go"))

	for _, file := range pass.Files {
		ast.Inspect(file, func(n ast.Node) bool {
			lit, isLit := n.(*ast.BasicLit)
			if !isLit || lit.Kind != token.STRING {
				return true
			}
			val, err := strconv.Unquote(lit.Value)
			if err != nil || !permRe.MatchString(val) {
				return true
			}
			prefix := val[:strings.Index(val, ":")]
			if prefix == owner || allow[val] {
				return true
			}
			pass.Reportf(lit.Pos(),
				"permission %q milik modul lain dan tidak terdaftar di Imports manifest modul %q. "+
					"Tambahkan {From: %q, Permission: %q} pada Permissions.Imports di manifest.go, "+
					"atau gunakan permission milik modul ini.",
				val, owner, prefix, val)
			return true
		})
	}
	return nil, nil
}

// moduleRoot menelusuri dir ke atas mencari direktori root modul bisnis, yaitu
// direktori yang memuat manifest.go DAN merupakan anak langsung dari direktori
// bernama "modules". Syarat parent "modules" membedakan modul bisnis pluggable dari
// manifest.go lain (mis. core/domain/manifest.go yang hanya mendefinisikan tipe).
// Penelusuran berhenti di batas Go module (go.mod). Mengembalikan ok=false bila tak
// ditemukan root modul bisnis di jalur ke atas.
func moduleRoot(dir string) (string, bool) {
	for {
		if filepath.Base(filepath.Dir(dir)) == "modules" &&
			fileExists(filepath.Join(dir, "manifest.go")) {
			return dir, true
		}
		if fileExists(filepath.Join(dir, "go.mod")) {
			return "", false
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// importedPermissions mengembalikan himpunan string ber-format permission yang muncul
// di manifest.go modul. Itu adalah allow-list permission lintas-modul yang sah dipakai
// (entri Imports). Bila manifest.go tak terbaca/tak ter-parse, mengembalikan himpunan
// kosong (fail-closed: pemakaian foreign apa pun akan dilaporkan).
func importedPermissions(manifestPath string) map[string]bool {
	out := make(map[string]bool)
	src, err := os.ReadFile(manifestPath)
	if err != nil {
		return out
	}
	f, err := parser.ParseFile(token.NewFileSet(), manifestPath, src, 0)
	if err != nil {
		return out
	}
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		if val, err := strconv.Unquote(lit.Value); err == nil && permRe.MatchString(val) {
			out[val] = true
		}
		return true
	})
	return out
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
