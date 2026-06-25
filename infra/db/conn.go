// Package db menyediakan adapter Postgres (pgx/v5) yang mengimplementasi port.DBConn.
// Seluruh kode yang menyentuh pgx HANYA ada di sini — modul domain tidak pernah
// mengimport pgx secara langsung (linter: domain-no-infra-import).
package db

import (
	"errors"

	"github.com/huda-salam/pamong/port"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Conn adalah alias untuk port.DBConn agar adapter bisa memakai nama yang lebih pendek.
type Conn = port.DBConn

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
