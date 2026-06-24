package gateway

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/huda-salam/pamong/core"
)

// WriteJSON menulis body JSON dengan status code yang diberikan.
func WriteJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// WriteError memetakan FrameworkError ke HTTP status yang sesuai dan menulis respons.
func WriteError(w http.ResponseWriter, err error) {
	var fe *core.FrameworkError
	if !errors.As(err, &fe) {
		WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	status := httpStatus(fe.Code)
	WriteJSON(w, status, map[string]any{
		"code":    fe.Code,
		"message": fe.Message,
		"field":   fe.Field,
	})
}

// ErrBadRequest mengembalikan error untuk input yang tidak bisa di-parse (HTTP 400).
func ErrBadRequest(msg string) error {
	return &core.FrameworkError{Code: "BAD_REQUEST", Message: msg}
}

func httpStatus(code string) int {
	switch code {
	case "NOT_FOUND":
		return http.StatusNotFound
	case "PERMISSION_DENIED":
		return http.StatusForbidden
	case "VALIDATION_ERROR":
		return http.StatusUnprocessableEntity
	case "CONFLICT":
		return http.StatusConflict
	case "BAD_REQUEST":
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}
