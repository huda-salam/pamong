package gateway

import (
	"encoding/json"
	"errors"
	"log/slog"
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
//
// Error yang BUKAN FrameworkError adalah kegagalan tak terduga (mis. pgx: koneksi gagal, SQL
// error). Teksnya TIDAK PERNAH masuk body: pesan pgx memuat host, port, user, dan nama database,
// dan sejak rute /auth/* dilayani tanpa otentikasi (PR-W1) pemanggil anonim bisa memicunya. Yang
// keluar hanya penanda generik; detailnya dicatat ke log proses (slog default — logger aplikasi
// menulis ke sink yang sama) agar tetap bisa didiagnosis operator.
//
// Konsekuensi yang disengaja: klien tak bisa lagi membedakan sebab kegagalan 500. Itu memang
// tujuannya — pembedaan yang berguna bagi klien harus dinyatakan sebagai FrameworkError, bukan
// bocor lewat teks error infrastruktur.
func WriteError(w http.ResponseWriter, err error) {
	var fe *core.FrameworkError
	if !errors.As(err, &fe) {
		slog.Error("kegagalan tak terduga dipetakan ke 500", "err", err.Error())
		WriteJSON(w, http.StatusInternalServerError, map[string]string{
			"code":    "INTERNAL",
			"message": "terjadi kesalahan internal",
		})
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
	case "UNAUTHORIZED":
		return http.StatusUnauthorized
	case "PERMISSION_DENIED", "FORBIDDEN":
		return http.StatusForbidden
	case "VALIDATION_ERROR":
		return http.StatusUnprocessableEntity
	case "CONFLICT":
		return http.StatusConflict
	case "TOO_MANY_REQUESTS":
		return http.StatusTooManyRequests
	case "BAD_REQUEST":
		return http.StatusBadRequest
	case "UNAVAILABLE":
		return http.StatusServiceUnavailable
	case "UNIMPLEMENTED":
		return http.StatusNotImplemented
	default:
		return http.StatusInternalServerError
	}
}
