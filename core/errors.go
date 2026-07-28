// Package core menyediakan tipe error framework yang di-map ke HTTP status oleh gateway.
// Modul bisnis WAJIB memakai fungsi di sini, bukan errors.New atau fmt.Errorf bebas
// (CODE_CONVENTION #3). Ini menjamin mapping HTTP status konsisten tanpa logic di handler.
package core

import "fmt"

// FrameworkError adalah error bertipe yang dikenali gateway untuk mapping HTTP.
type FrameworkError struct {
	Code    string
	Message string
	Field   string // diisi oleh ErrValidation; kosong untuk error lain
}

func (e *FrameworkError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("%s: %s (%s)", e.Code, e.Message, e.Field)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// ErrNotFound dipublikasikan saat entitas tidak ditemukan (HTTP 404).
func ErrNotFound(entity, id string) error {
	return &FrameworkError{
		Code:    "NOT_FOUND",
		Message: fmt.Sprintf("%s dengan id %q tidak ditemukan", entity, id),
	}
}

// ErrUnauthorized dipublikasikan saat otentikasi gagal — tak ada token, atau token tidak
// valid/kedaluwarsa/dicabut (HTTP 401). Beda dari ErrPermissionDenied (403): 401 = "tak
// terbukti siapa", 403 = "terbukti, tapi tak boleh".
func ErrUnauthorized(reason string) error {
	return &FrameworkError{
		Code:    "UNAUTHORIZED",
		Message: reason,
	}
}

// ErrPermissionDenied dipublikasikan saat actor tidak punya permission (HTTP 403).
func ErrPermissionDenied(perm string) error {
	return &FrameworkError{
		Code:    "PERMISSION_DENIED",
		Message: fmt.Sprintf("akses ditolak: permission %q diperlukan", perm),
	}
}

// ErrValidation dipublikasikan saat input tidak valid (HTTP 422).
func ErrValidation(field, reason string) error {
	return &FrameworkError{
		Code:    "VALIDATION_ERROR",
		Message: reason,
		Field:   field,
	}
}

// ErrConflict dipublikasikan saat terjadi konflik (optimistic lock, duplikat) (HTTP 409).
func ErrConflict(msg string) error {
	return &FrameworkError{
		Code:    "CONFLICT",
		Message: msg,
	}
}

// ErrUnavailable dipublikasikan saat sebuah dependency framework yang WAJIB untuk menjamin
// keamanan/integritas request tidak tersedia (HTTP 503) — mis. store idempotency gagal
// diakses. Fail-closed: lebih aman menolak (klien boleh retry) daripada memproses mutasi
// tanpa jaminan yang seharusnya. Beda dari 500: 503 = transient, retry masuk akal.
func ErrUnavailable(reason string) error {
	return &FrameworkError{
		Code:    "UNAVAILABLE",
		Message: reason,
	}
}

// ErrUnimplemented dipublikasikan saat sebuah kapabilitas yang secara sengaja ditunda dipanggil
// (HTTP 501). Beda dari 500 (bug tak terduga) & 503 (transient): 501 = "fitur ini memang belum
// ada". Dipakai adapter yang mengimplementasi sebagian kontrak port agar pemanggil GAGAL LANTANG
// alih-alih menerima jawaban yang diam-diam salah (mis. UserResolver.HasCentralRole yang butuh
// lookup identity DB — DEFERRED).
func ErrUnimplemented(feature string) error {
	return &FrameworkError{
		Code:    "UNIMPLEMENTED",
		Message: fmt.Sprintf("kapabilitas %q belum diimplementasikan", feature),
	}
}

// ErrTooManyRequests dipublikasikan saat batas laju (rate limit) terlampaui (HTTP 429).
// Dipakai proteksi brute-force/flooding — mis. terlalu banyak permintaan/verifikasi OTP untuk
// satu kredensial. Berbeda dari ErrUnauthorized (401: kredensial salah): 429 = "benar atau salah,
// kamu terlalu sering". Pesan tidak membocorkan apakah kredensial ada.
func ErrTooManyRequests(reason string) error {
	return &FrameworkError{
		Code:    "TOO_MANY_REQUESTS",
		Message: reason,
	}
}
