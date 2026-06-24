// Package usecase (dirty) — membuktikan bahwa lapisan usecase juga diperiksa,
// bukan hanya domain. Use case adalah orchestrator yang tetap harus bersih
// dari detail infrastruktur; ia memanggil port, bukan adapter konkret.
package usecase

import (
	// Mengimport adapter konkret dari use case = pelanggaran. Use case hanya
	// boleh tahu interface (port), bukan implementasinya.
	"govframework/adapter/db" // want `import terlarang di lapisan domain.*adapter/db.*infrastruktur`
)

var _ = db.Repo
