// Package http berisi driving adapter HTTP untuk alur auth identity (PR-W1).
//
// Handler bersifat TIPIS: parse → delegate ke use case → respond. Tidak ada business logic,
// tidak ada akses repository langsung. Berbeda dari handler modul bisnis dalam satu hal yang
// menentukan cara ia dipasang: alur login adalah PRA-OTENTIKASI, jadi rutenya TIDAK boleh berada
// di balik RequireAuth — kalau tidak, klien butuh token untuk memperoleh token. Pemasangannya
// (rute mana publik, mana menuntut token sementara) ada di cmd/server; lihat mountAuthRoutes.
//
// DTO request/response ditulis eksplisit di sini, bukan memakai ulang struct use case: bentuk
// kawat (JSON snake_case) adalah kontrak dengan klien dan harus bisa berubah terpisah dari
// bentuk internal use case.
package http

import (
	"encoding/json"
	"net/http"

	"github.com/huda-salam/pamong/gateway"
	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/identity/usecase"
)

// Handler memegang use case alur auth; di-wire di composition root (cmd/server).
type Handler struct {
	loginEmployee *usecase.LoginEmployee
	selectTenant  *usecase.SelectTenant
	loginCitizen  *usecase.LoginCitizen
	requestOTP    *usecase.RequestOTP
	verifyOTP     *usecase.VerifyOTP
}

// NewHandler merakit handler auth. Semua use case wajib non-nil — rute yang terdaftar tapi
// menunjuk use case nil akan panic saat request pertama, persis jenis kegagalan yang paling
// mahal ditemukan di produksi.
func NewHandler(
	loginEmployee *usecase.LoginEmployee,
	selectTenant *usecase.SelectTenant,
	loginCitizen *usecase.LoginCitizen,
	requestOTP *usecase.RequestOTP,
	verifyOTP *usecase.VerifyOTP,
) *Handler {
	return &Handler{
		loginEmployee: loginEmployee,
		selectTenant:  selectTenant,
		loginCitizen:  loginCitizen,
		requestOTP:    requestOTP,
		verifyOTP:     verifyOTP,
	}
}

// --- DTO kawat ---

type credentialRequest struct {
	CredType  string `json:"cred_type"` // nip | nik (internal) · nik | email | no_hp (publik)
	CredValue string `json:"cred_value"`
	Password  string `json:"password"`
}

type selectTenantRequest struct {
	TenantID string `json:"tenant_id"`
}

type otpRequestRequest struct {
	CredType  string `json:"cred_type"` // email | no_hp
	CredValue string `json:"cred_value"`
}

type otpVerifyRequest struct {
	CredType  string `json:"cred_type"`
	CredValue string `json:"cred_value"`
	Code      string `json:"code"`
}

// tokenResponse adalah bentuk respons untuk alur yang langsung menerbitkan token final.
type tokenResponse struct {
	Token string `json:"token"`
}

// loginResponse membedakan dua hasil login employee: token final (satu tenant) vs token
// SEMENTARA + daftar pilihan (lebih dari satu tenant). Klien membedakannya lewat
// need_tenant_selection, bukan dengan menebak dari ada/tidaknya daftar.
type loginResponse struct {
	Token               string         `json:"token"`
	NeedTenantSelection bool           `json:"need_tenant_selection"`
	Tenants             []tenantChoice `json:"tenants"`
}

type tenantChoice struct {
	TenantID     string `json:"tenant_id"`
	IsHomeTenant bool   `json:"is_home_tenant"`
}

// --- Handler ---

// LoginEmployee menangani POST /auth/login (portal internal, NIP/NIK + password).
func (h *Handler) LoginEmployee(w http.ResponseWriter, r *http.Request) {
	var in credentialRequest
	if !decode(w, r, &in) {
		return
	}

	res, err := h.loginEmployee.Execute(r.Context(), usecase.LoginEmployeeInput{
		CredType:  domain.CredType(in.CredType),
		CredValue: in.CredValue,
		Password:  in.Password,
	})
	if err != nil {
		gateway.WriteError(w, err) // 401 seragam / 429 saat laju terlampaui
		return
	}

	out := loginResponse{
		Token:               res.Token,
		NeedTenantSelection: res.NeedTenantSelection,
		Tenants:             make([]tenantChoice, 0, len(res.Tenants)),
	}
	for _, t := range res.Tenants {
		out.Tenants = append(out.Tenants, tenantChoice{TenantID: t.TenantID, IsHomeTenant: t.IsHomeTenant})
	}
	gateway.WriteJSON(w, http.StatusOK, out)
}

// SelectTenant menangani POST /auth/select-tenant: menukar token SEMENTARA (hasil login dengan
// >1 tenant) menjadi token scoped final.
//
// Rute ini satu-satunya di grup auth yang MENUNTUT token. person_id diambil use case dari klaim
// tersigning lewat AuthContext — tak pernah dari body — sehingga tak ada cara memilih tenant atas
// nama orang lain. Karena itu handler cukup meneruskan gateway.Context apa adanya.
func (h *Handler) SelectTenant(w http.ResponseWriter, r *http.Request) {
	var in selectTenantRequest
	if !decode(w, r, &in) {
		return
	}

	token, err := h.selectTenant.Execute(gateway.FromRequest(r), in.TenantID)
	if err != nil {
		gateway.WriteError(w, err)
		return
	}
	gateway.WriteJSON(w, http.StatusOK, tokenResponse{Token: token})
}

// LoginCitizen menangani POST /auth/public/login (portal publik, NIK/email/no_hp + password).
// Token yang terbit berpersona citizen — tanpa tenant dan tanpa role internal, termasuk untuk
// ASN yang login lewat sini.
func (h *Handler) LoginCitizen(w http.ResponseWriter, r *http.Request) {
	var in credentialRequest
	if !decode(w, r, &in) {
		return
	}

	token, err := h.loginCitizen.Execute(r.Context(), usecase.LoginCitizenInput{
		CredType:  domain.CredType(in.CredType),
		CredValue: in.CredValue,
		Password:  in.Password,
	})
	if err != nil {
		gateway.WriteError(w, err)
		return
	}
	gateway.WriteJSON(w, http.StatusOK, tokenResponse{Token: token})
}

// RequestOTP menangani POST /auth/public/otp/request.
//
// SELALU 202 saat use case tidak error — termasuk untuk credential yang tak dikenal, person
// non-aktif, dan kuota penerbitan yang habis. Itu bukan kelalaian: RequestOTP sengaja
// mengembalikan nil pada ketiga kasus tersebut agar respons tak bisa dipakai menebak apakah
// sebuah email/no_hp terdaftar (enumeration-resistance, ADR-008). Membedakan status di sini akan
// membatalkan properti yang dijaga use case.
func (h *Handler) RequestOTP(w http.ResponseWriter, r *http.Request) {
	var in otpRequestRequest
	if !decode(w, r, &in) {
		return
	}

	if err := h.requestOTP.Execute(r.Context(), usecase.RequestOTPInput{
		CredType:  domain.CredType(in.CredType),
		CredValue: in.CredValue,
	}); err != nil {
		gateway.WriteError(w, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// VerifyOTP menangani POST /auth/public/otp/verify: menukar kode OTP dengan token citizen.
func (h *Handler) VerifyOTP(w http.ResponseWriter, r *http.Request) {
	var in otpVerifyRequest
	if !decode(w, r, &in) {
		return
	}

	token, err := h.verifyOTP.Execute(r.Context(), usecase.VerifyOTPInput{
		CredType:  domain.CredType(in.CredType),
		CredValue: in.CredValue,
		Code:      in.Code,
	})
	if err != nil {
		gateway.WriteError(w, err)
		return
	}
	gateway.WriteJSON(w, http.StatusOK, tokenResponse{Token: token})
}

// decode membaca body JSON ke dst, menulis 400 dan mengembalikan false bila gagal. Body dibatasi
// 64 KiB: rute auth dilayani TANPA otentikasi, jadi ia menerima kiriman siapa pun — tanpa batas,
// satu request bisa memaksa server mengalokasikan memori sebesar yang klien mau.
func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(dst); err != nil {
		gateway.WriteError(w, gateway.ErrBadRequest("body tidak valid"))
		return false
	}
	return true
}
