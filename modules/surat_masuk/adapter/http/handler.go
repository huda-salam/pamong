// Package http berisi driving adapter HTTP untuk modul surat_masuk.
// Handler bersifat TIPIS: parse -> delegate ke use case -> respond. Tidak ada business
// logic, tidak ada akses repository langsung (linter: handler-no-direct-repo).
package http

import (
	"encoding/json"
	"net/http"

	"github.com/huda-salam/pamong/gateway"
	"github.com/huda-salam/pamong/modules/surat_masuk/usecase"
)

// Handler memegang use case yang dibutuhkan; di-wire di bootstrap.
type Handler struct {
	create    *usecase.CreateSuratMasuk
	disposisi *usecase.DisposisiSurat
}

func NewHandler(c *usecase.CreateSuratMasuk, d *usecase.DisposisiSurat) *Handler {
	return &Handler{create: c, disposisi: d}
}

// CreateSuratMasuk menangani POST /surat-masuk.
// Permission TIDAK dicek di sini melainkan di dalam use case (sumber kebenaran tunggal),
// namun gateway tetap menyediakan AuthContext.
func (h *Handler) CreateSuratMasuk(w http.ResponseWriter, r *http.Request) {
	ctx := gateway.FromRequest(r) // gateway.Context implement port.AuthContext

	var in usecase.CreateSuratMasukInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		gateway.WriteError(w, gateway.ErrBadRequest("body tidak valid"))
		return
	}

	out, err := h.create.Execute(ctx, in)
	if err != nil {
		gateway.WriteError(w, err) // error types framework -> HTTP status otomatis
		return
	}
	gateway.WriteJSON(w, http.StatusCreated, out)
}
