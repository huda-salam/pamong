// Package domain (dirty) — sengaja melanggar aturan hexagonal.
// Komentar bertanda 'want' di setiap baris import menyatakan diagnostic yang
// HARUS dilaporkan analyzer di baris tersebut (diverifikasi oleh analysistest).
package domain

import (
	"context"
	"database/sql" // want `import terlarang di lapisan domain.*database/sql.*stdlib I/O`
	"net/http"     // want `import terlarang di lapisan domain.*net/http.*stdlib I/O`
	"os"           // want `import terlarang di lapisan domain.*os.*stdlib I/O`

	// Import ke lapisan infrastruktur module sendiri.
	"govframework/infra/db" // want `import terlarang di lapisan domain.*infra/db.*infrastruktur`

	// Library infrastruktur eksternal.
	"github.com/jackc/pgx/v5" // want `import terlarang di lapisan domain.*pgx.*adapter`
)

// Penggunaan dummy agar import tidak dianggap unused oleh compiler.
var (
	_ = sql.ErrNoRows
	_ = http.StatusOK
	_ = os.Getenv
	_ = db.Conn
	_ = pgx.ErrNoRows
	_ context.Context
)
