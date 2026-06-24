package port

import "context"

// Row merupakan abstraksi baris hasil query tunggal.
// pgx.Row memenuhi interface ini sehingga adapter tidak perlu wrapper tambahan di sisi Row.
type Row interface {
	Scan(dest ...any) error
}

// CommandTag merupakan abstraksi hasil eksekusi (INSERT/UPDATE/DELETE).
type CommandTag interface {
	RowsAffected() int64
}

// Rows merupakan abstraksi cursor multi-baris.
type Rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
	Close()
}

// DBConn adalah port koneksi database yang dipakai adapter persistensi.
// Definisi di sini agar core/domain dan modul domain tidak mengimport infra/db secara langsung.
type DBConn interface {
	QueryRow(ctx context.Context, sql string, args ...any) Row
	Query(ctx context.Context, sql string, args ...any) (Rows, error)
	Exec(ctx context.Context, sql string, args ...any) (CommandTag, error)
}
