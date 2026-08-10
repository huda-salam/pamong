package gateway_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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
		{"TooManyRequests", core.ErrTooManyRequests("terlalu sering"), http.StatusTooManyRequests},
		{"Unauthorized", core.ErrUnauthorized("tak terbukti"), http.StatusUnauthorized},
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

// Error yang BUKAN FrameworkError tak boleh membocorkan teksnya ke body.
//
// Pesan pgx memuat host, port, user, dan nama database. Sejak PR-W1 rute /auth/* dilayani tanpa
// otentikasi, pemanggil anonim bisa memicu kegagalan DB (mis. tenant DB tak terjangkau saat
// resolusi role) dan membaca topologi infrastruktur dari respons 500.
func TestWriteError_NonFrameworkError_TakBocorkanDetail(t *testing.T) {
	rahasia := "failed to connect to `user=govapp database=gov_identity`: dial tcp 10.0.3.7:5432"
	w := httptest.NewRecorder()

	gateway.WriteError(w, errors.New(rahasia))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("mau 500, dapat %d", w.Code)
	}
	body := w.Body.String()
	for _, bocoran := range []string{"govapp", "gov_identity", "10.0.3.7", "5432"} {
		if strings.Contains(body, bocoran) {
			t.Fatalf("body 500 memuat detail infrastruktur %q: %s", bocoran, body)
		}
	}
	var out map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("body bukan JSON: %v", err)
	}
	if out["code"] != "INTERNAL" {
		t.Errorf("code = %q, mau INTERNAL", out["code"])
	}
}
