package gateway_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/gateway"
)

// TestWriteError_StatusMapping memenuhi DoD PR-0.2.3: tiap error type framework
// dipetakan ke HTTP status yang benar oleh gateway.
func TestWriteError_StatusMapping(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{"NotFound", core.ErrNotFound("SuratMasuk", "x"), http.StatusNotFound},
		{"PermissionDenied", core.ErrPermissionDenied("p"), http.StatusForbidden},
		{"Validation", core.ErrValidation("f", "r"), http.StatusUnprocessableEntity},
		{"Conflict", core.ErrConflict("c"), http.StatusConflict},
		{"BadRequest", gateway.ErrBadRequest("body rusak"), http.StatusBadRequest},
		{"ErrorBiasa", errors.New("kegagalan tak terduga"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			gateway.WriteError(rec, tc.err)
			if rec.Code != tc.wantStatus {
				t.Errorf("status = %d, mau %d", rec.Code, tc.wantStatus)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q, mau application/json", ct)
			}
		})
	}
}
