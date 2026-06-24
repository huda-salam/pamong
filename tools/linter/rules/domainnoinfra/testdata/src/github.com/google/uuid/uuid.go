// Package uuid adalah stub minimal untuk keperluan testdata analysistest.
// Tidak ada logika nyata — hanya tipe yang dibutuhkan agar analisis bisa berjalan.
package uuid

// UUID adalah representasi UUID 128-bit.
type UUID [16]byte

// New membuat UUID baru (stub).
func New() UUID { return UUID{} }
