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
