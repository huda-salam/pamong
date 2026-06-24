// Package domainnoinfra menegakkan aturan hexagonal architecture GovFramework:
// package di lapisan domain (domain/, usecase/) tidak boleh mengimport package
// di lapisan infrastruktur (infra/, adapter/) atau library eksternal yang
// menyentuh I/O langsung.
//
// Lapisan domain hanya boleh bergantung pada:
//   - standard library Go (kecuali yang berbau I/O langsung seperti database/sql, net/http)
//   - package port (interface yang didefinisikan domain)
//   - library murni tanpa side-effect (uuid, decimal, time, dsb — lihat allowlist)
//
// Ini adalah implementasi referensi. Rule linter lain di GovFramework mengikuti
// pola yang sama: satu package per rule, satu Analyzer, test pakai analysistest.
package domainnoinfra

import (
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// Analyzer adalah entry point yang didaftarkan ke pamongctl lint.
var Analyzer = &analysis.Analyzer{
	Name: "domainnoinfra",
	Doc:  "memastikan lapisan domain tidak mengimport lapisan infrastruktur (aturan hexagonal)",
	Run:  run,
}

// domainLayers adalah segmen path yang menandai sebuah package ada di lapisan domain.
// Dicocokkan per-segmen (bukan substring) agar "domainnoinfra" tidak salah dikira "domain".
var domainLayers = []string{"domain", "usecase"}

// forbiddenLayers adalah segmen path infrastruktur yang TIDAK boleh diimport domain.
var forbiddenLayers = []string{"infra", "adapter"}

// forbiddenStdlib adalah package standard library yang menyentuh I/O / detail teknis,
// sehingga kehadirannya di domain melanggar pemisahan port/adapter.
var forbiddenStdlib = map[string]bool{
	"database/sql": true,
	"net/http":     true,
	"os":           true, // baca env / file langsung — harus lewat config port
}

// forbiddenExternal adalah library pihak ketiga yang merupakan detail infrastruktur.
// Prefix-match: "github.com/jackc/pgx" mencakup semua sub-package-nya.
var forbiddenExternal = []string{
	"github.com/jackc/pgx",
	"gorm.io",
	"github.com/redis/go-redis",
	"github.com/nats-io/nats.go",
	"github.com/minio/minio-go",
}

func run(pass *analysis.Pass) (interface{}, error) {
	// Lewati fixture testdata: itu sengaja melanggar untuk keperluan test analyzer,
	// bukan kode produksi. (analysistest memakai path tanpa "/testdata/" sehingga
	// test tetap berjalan; multichecker pada modul nyata memakai path lengkap.)
	if strings.Contains(pass.Pkg.Path(), "/testdata/") {
		return nil, nil
	}
	// Hanya periksa package yang berada di lapisan domain.
	if !isDomainPackage(pass.Pkg.Path()) {
		return nil, nil
	}

	for _, file := range pass.Files {
		for _, imp := range file.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				continue
			}
			if reason := violation(path); reason != "" {
				pass.Reportf(imp.Pos(),
					"import terlarang di lapisan domain: %q (%s). "+
						"Lapisan domain hanya boleh bergantung pada port (interface) "+
						"dan library murni. Definisikan interface di domain/ports.go, "+
						"lalu implementasikan di adapter.",
					path, reason)
			}
		}
	}
	return nil, nil
}

// isDomainPackage menentukan apakah path package termasuk lapisan domain.
func isDomainPackage(pkgPath string) bool {
	for _, layer := range domainLayers {
		if hasSegment(pkgPath, layer) {
			return true
		}
	}
	return false
}

// hasSegment memeriksa apakah path memuat segmen (antar "/") yang sama persis dengan seg.
// Ini mencegah false match substring: "rules/domainnoinfra" bukan lapisan "domain".
func hasSegment(path, seg string) bool {
	for _, s := range strings.Split(path, "/") {
		if s == seg {
			return true
		}
	}
	return false
}

// violation mengembalikan alasan pelanggaran jika import path terlarang,
// atau string kosong jika import diperbolehkan.
func violation(importPath string) string {
	// 1. Cek import ke lapisan infrastruktur dalam module yang sama (per-segmen).
	for _, layer := range forbiddenLayers {
		if hasSegment(importPath, layer) {
			return "mengarah ke lapisan infrastruktur /" + layer
		}
	}

	// 2. Cek standard library yang menyentuh I/O.
	if forbiddenStdlib[importPath] {
		return "package stdlib I/O — gunakan port"
	}

	// 3. Cek library eksternal yang merupakan detail teknis.
	for _, ext := range forbiddenExternal {
		if importPath == ext || strings.HasPrefix(importPath, ext+"/") {
			return "library infrastruktur eksternal — gunakan adapter"
		}
	}

	return ""
}
