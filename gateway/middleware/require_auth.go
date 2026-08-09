package middleware

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/gateway"
)

// RequireAuth menolak request yang TIDAK terotentikasi (tak ada Claims terverifikasi di
// context) dengan 401. Dipasang SETELAH Auth pada rute internal/bisnis (PRD gateway F3
// langkah 3 + acceptance "request tanpa/with token invalid → ditolak").
//
// Kenapa terpisah dari Auth: Auth SENGAJA meneruskan anonymous agar rute PUBLIK (portal
// citizen) tetap dilayani tanpa token, dan agar token invalid (bukan sekadar absen) ditolak
// di Auth sendiri (401). RequireAuth-lah yang menegakkan "wajib login" untuk rute yang bukan
// publik. Ini menutup celah permisif-default PR-5.1.1 (rute bisnis dilayani tanpa auth):
// tanpa RequireAuth, request anonymous lolos ke handler dan RequirePermission default permisif
// mengizinkannya.
//
// Route-grouping publik vs internal SUDAH ADA sejak PR-W1: RequireAuth membungkus router BISNIS,
// sementara grup /auth/* (login, OTP) dipasang di top mux `cmd/server` tanpa RequireAuth — alur
// login bersifat pra-otentikasi, jadi memasangnya di dalam chain ini akan menuntut token untuk
// MEMPEROLEH token. /healthz juga dilayani di luar stack (auth-free). Lihat mountAuthRoutes.
//
// Batasan tanggung jawab: RequireAuth hanya gerbang "terbukti siapa" (401). Penolakan
// otorisasi granular "boleh aksi X?" (403) tetap di use case lewat RequirePermission.
//
// KONSEKUENSI SENGAJA: karena RequireAuth membungkus SELURUH router bisnis (berjalan sebelum
// dispatch rute), path yang tidak dikenal pun mengembalikan 401 untuk pemanggil anonim —
// bukan 404 (perubahan dari PR-5.1.1 yang men-serve router telanjang). Ini justru diinginkan:
// klien tak terautentikasi tak boleh membedakan rute yang ada vs tidak (anti-enumeration).
// Pemanggil TERautentikasi tetap mendapat 404 untuk path tak dikenal (lolos RequireAuth →
// mux menjawab 404).
func RequireAuth() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c := gateway.FromRequest(r)
			if c.PersonID() == uuid.Nil {
				gateway.WriteError(w, core.ErrUnauthorized("autentikasi diperlukan"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
