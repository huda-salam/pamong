// Package pgx adalah stub minimal untuk keperluan testdata analysistest.
// Kehadirannya memungkinkan import "github.com/jackc/pgx/v5" di testdata dirty
// ter-resolve, sehingga analyzer bisa mendeteksi pelanggaran impor library infra.
package pgx

import "errors"

// ErrNoRows adalah error yang dikembalikan saat query tidak menemukan baris.
var ErrNoRows = errors.New("no rows in result set")
