package gateway

import (
	"net/http"

	"github.com/huda-salam/pamong/port"
)

// Router adalah implementasi konkret port.Router di atas net/http.ServeMux. Ia meng-agregasi
// rute dari SEMUA modul (didaftarkan saat Bootstrap lewat app.Router()) menjadi satu
// http.Handler yang di-serve binary server. Inilah "router aggregator" (PR-5.1.1).
//
// Pola rute memakai ServeMux method-aware (Go 1.22+): "METHOD /pattern". Registrasi pola yang
// bentrok membuat ServeMux panic — sengaja, agar konflik rute ketahuan saat boot (philosophy
// #4), bukan diam-diam menimpa. Middleware (auth/tenant/ratelimit/CORS/audit) dibungkus di
// LUAR Router saat perakitan server (PR-5.1.2), bukan di sini — Router murni pemetaan rute.
type Router struct {
	mux *http.ServeMux
}

var _ port.Router = (*Router)(nil)

// NewRouter membuat Router kosong siap menerima registrasi rute modul.
func NewRouter() *Router { return &Router{mux: http.NewServeMux()} }

func (r *Router) Get(pattern string, h http.HandlerFunc)    { r.handle(http.MethodGet, pattern, h) }
func (r *Router) Post(pattern string, h http.HandlerFunc)   { r.handle(http.MethodPost, pattern, h) }
func (r *Router) Put(pattern string, h http.HandlerFunc)    { r.handle(http.MethodPut, pattern, h) }
func (r *Router) Delete(pattern string, h http.HandlerFunc) { r.handle(http.MethodDelete, pattern, h) }

func (r *Router) handle(method, pattern string, h http.HandlerFunc) {
	r.mux.HandleFunc(method+" "+pattern, h)
}

// ServeHTTP membuat Router memenuhi http.Handler sehingga bisa langsung dipasang ke
// http.Server (atau dibungkus middleware).
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mux.ServeHTTP(w, req)
}
