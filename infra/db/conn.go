// Package db menyediakan adapter Postgres (pgx/v5) yang mengimplementasi port.DBConn.
// Seluruh kode yang menyentuh pgx HANYA ada di sini — modul domain tidak pernah
// mengimport pgx secara langsung (linter: domain-no-infra-import).
package db

import (
	"context"
	"errors"

	"github.com/huda-salam/pamong/port"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Conn adalah alias untuk port.DBConn agar adapter bisa memakai nama yang lebih pendek.
type Conn = port.DBConn

// TxConn adalah Conn yang juga bisa membuka TRANSAKSI. Dipakai repo yang harus menulis beberapa
// tabel secara atomik (mis. role + grant permission-nya) tapi tetap ingin dirakit sekali saat
// boot dan dirutekan per-request.
//
// Ia ada sebagai interface, bukan *Pool telanjang, justru karena itu: `*Pool` mengikat repo ke
// SATU database, sementara realita repo ini DB-per-tenant (ADR-004). Dipenuhi oleh `*Pool`
// (satu DB tetap — test, tooling, DB sentral) DAN `*TenantRoutingConn` (routing per-request dari
// klaim token). Tanpa seam ini, repo ber-transaksi hanya bisa dipakai lewat pool yang dipilih
// saat boot — yaitu tidak bisa dipakai di jalur request multi-tenant sama sekali.
type TxConn interface {
	Conn
	Begin(ctx context.Context) (*Tx, error)
}

// IsNoRows mengembalikan true jika error adalah "tidak ada baris ditemukan".
func IsNoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}

// IsUniqueViolation mengembalikan true jika error adalah pelanggaran UNIQUE constraint
// (SQLSTATE 23505). Dipakai adapter untuk memetakan duplikat ke core.ErrConflict.
func IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
