package http

import (
	"encoding/json"
	"net/http"

	"github.com/huda-salam/pamong/gateway"
)

// decode membaca body JSON dengan batas ukuran, memetakan kegagalan ke 400. Body yang tak
// terbentuk TIDAK boleh diam-diam menjadi nilai nol (uuid.Nil / waktu nol) — di grup ini nilai
// nol punya arti otorisasi (lihat assignRoleRequest.UnitKerjaID), jadi kegagalan decode harus
// menghentikan request, bukan melanjutkannya dengan asumsi.
func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(dst); err != nil {
		gateway.WriteError(w, gateway.ErrBadRequest("body tidak valid"))
		return false
	}
	return true
}
