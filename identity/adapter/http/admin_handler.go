package http

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/huda-salam/pamong/gateway"
	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/identity/usecase"
)

// AdminHandler adalah driving adapter untuk administrasi identity (`/admin/identity/*`, PR-W2):
// membuat person, melekatkan employment, membuat kredensial, menugaskan employment ke tenant,
// dan menugaskan role sentral.
//
// Ia dipisah dari Handler (alur auth) karena SIFAT PEMASANGANNYA berlawanan. Grup /auth/*
// sengaja berada DI LUAR RequireAuth — login adalah pra-otentikasi. Grup ini kebalikannya:
// setiap endpointnya memutasi identitas sentral, jadi ia dipasang di dalam business chain
// (Auth → RequireAuth → TenantResolver → RateLimit → Idempotency) tanpa pengecualian. Satu
// struct untuk keduanya akan membuat pemasangan yang salah tampak masuk akal.
//
// Kenapa endpoint ini ada sama sekali: sampai PR-W1 seluruh use case di bawah ini lengkap dan
// teruji tapi tak punya satu pun pemanggil produksi. Akibat yang paling mahal ada pada
// AssignEmploymentToTenant — ia satu-satunya penerbit `identity.employment.ditugaskan`, yaitu
// pemicu clone identity→tenant. Clone engine sudah ter-wire sejak PR-5.1.4, tapi tanpa
// produsen event-nya jalur itu tak pernah berjalan di luar test (GAP (b) PR-5.1.4).
type AdminHandler struct {
	createPerson     *usecase.CreatePerson
	attachEmployment *usecase.AttachEmployment
	createCredential *usecase.CreateCredential
	assignTenant     *usecase.AssignEmploymentToTenant
	assignRole       *usecase.AssignCentralRole
}

// NewAdminHandler merakit handler admin identity. Semua use case wajib non-nil dan itu
// DITEGAKKAN di sini — alasan yang sama dengan NewHandler: rute yang terdaftar tapi menunjuk
// use case nil baru panic pada request pertama, di produksi, pada pengguna.
func NewAdminHandler(
	createPerson *usecase.CreatePerson,
	attachEmployment *usecase.AttachEmployment,
	createCredential *usecase.CreateCredential,
	assignTenant *usecase.AssignEmploymentToTenant,
	assignRole *usecase.AssignCentralRole,
) *AdminHandler {
	switch {
	case createPerson == nil:
		panic("identity/adapter/http: CreatePerson nil")
	case attachEmployment == nil:
		panic("identity/adapter/http: AttachEmployment nil")
	case createCredential == nil:
		panic("identity/adapter/http: CreateCredential nil")
	case assignTenant == nil:
		panic("identity/adapter/http: AssignEmploymentToTenant nil")
	case assignRole == nil:
		panic("identity/adapter/http: AssignCentralRole nil")
	}
	return &AdminHandler{
		createPerson:     createPerson,
		attachEmployment: attachEmployment,
		createCredential: createCredential,
		assignTenant:     assignTenant,
		assignRole:       assignRole,
	}
}

// --- DTO kawat ---
//
// Ditulis eksplisit, bukan memakai ulang struct use case: bentuk kawat adalah kontrak dengan
// klien dan harus bisa berubah terpisah dari bentuk internal. Field uuid & waktu memakai tipe
// Go-nya langsung — nilai yang tak berbentuk UUID/RFC3339 gagal saat decode dan menjadi 400,
// tak pernah diam-diam menjadi uuid.Nil atau waktu nol.

type createPersonRequest struct {
	NIK         string     `json:"nik"`
	NamaLengkap string     `json:"nama_lengkap"`
	NoHP        string     `json:"no_hp"`
	Email       string     `json:"email"`
	TglLahir    *time.Time `json:"tgl_lahir"`
}

type attachEmploymentRequest struct {
	PersonID     uuid.UUID  `json:"person_id"`
	Status       string     `json:"status"` // asn | non_asn
	NIP          string     `json:"nip"`
	InstansiAsal string     `json:"instansi_asal"`
	ValidFrom    time.Time  `json:"valid_from"`
	ValidUntil   *time.Time `json:"valid_until"`
}

type createCredentialRequest struct {
	PersonID  uuid.UUID `json:"person_id"`
	CredType  string    `json:"cred_type"` // nip | nik | email | no_hp | oauth
	CredValue string    `json:"cred_value"`
	Password  string    `json:"password"` // kosong = kredensial OTP-only (ADR-008)
	IsPrimary bool      `json:"is_primary"`
}

type assignmentRequest struct {
	EmploymentID uuid.UUID  `json:"employment_id"`
	TenantID     string     `json:"tenant_id"`
	CrossTenant  bool       `json:"cross_tenant"`
	ValidFrom    time.Time  `json:"valid_from"`
	ValidUntil   *time.Time `json:"valid_until"`
}

type centralRoleAssignmentRequest struct {
	PersonID    uuid.UUID  `json:"person_id"`
	RoleID      uuid.UUID  `json:"role_id"`
	TenantScope []string   `json:"tenant_scope"` // wajib untuk role scoped, kosong untuk global
	ValidFrom   time.Time  `json:"valid_from"`
	ValidUntil  *time.Time `json:"valid_until"`
}

// Respons sengaja MINIMAL: id yang baru terbit plus atribut non-pengenal. NIK/NIP/cred_value
// tidak pernah dipantulkan kembali meski pemanggil sendiri yang mengirimnya — badan respons
// mengalir ke log akses, proxy, dan cache idempotency, yaitu jalur samping yang sama yang
// ditutup ADR-009 §6. Pemanggil sudah tahu nilai yang ia kirim; yang belum ia tahu hanyalah id.

type idResponse struct {
	ID uuid.UUID `json:"id"`
}

type employmentResponse struct {
	ID       uuid.UUID `json:"id"`
	PersonID uuid.UUID `json:"person_id"`
	Status   string    `json:"status"`
}

type credentialResponse struct {
	ID       uuid.UUID `json:"id"`
	PersonID uuid.UUID `json:"person_id"`
	CredType string    `json:"cred_type"`
}

type assignmentResponse struct {
	ID           uuid.UUID `json:"id"`
	EmploymentID uuid.UUID `json:"employment_id"`
	TenantID     string    `json:"tenant_id"`
	IsHomeTenant bool      `json:"is_home_tenant"`
}

type centralRoleAssignmentResponse struct {
	ID          uuid.UUID `json:"id"`
	PersonID    uuid.UUID `json:"person_id"`
	RoleID      uuid.UUID `json:"role_id"`
	TenantScope []string  `json:"tenant_scope"`
}

// --- Handler ---
//
// Pola tiap handler sama dan urutannya mengikat (CLAUDE.md aturan #3): permission di baris
// PERTAMA, sebelum Bind, sebelum log, sebelum apa pun. Use case memeriksa permission yang sama
// sekali lagi — itu bukan duplikasi sia-sia melainkan dua gerbang untuk dua permukaan:
// use case juga dipanggil workflow & CLI, handler juga melayani body yang belum tepercaya.
// Urutan "permission dulu, baru parse" itulah yang membuat pemanggil tak berizin tak pernah
// bisa membedakan body yang ditolak parser dari body yang diterima.

// CreatePerson menangani POST /admin/identity/persons.
func (h *AdminHandler) CreatePerson(w http.ResponseWriter, r *http.Request) {
	ctx := gateway.FromRequest(r)
	if err := ctx.RequirePermission(domain.PermPersonBuat); err != nil {
		gateway.WriteError(w, err)
		return
	}
	var in createPersonRequest
	if !decode(w, r, &in) {
		return
	}

	p, err := h.createPerson.Execute(ctx, usecase.CreatePersonInput{
		NIK:         in.NIK,
		NamaLengkap: in.NamaLengkap,
		NoHP:        in.NoHP,
		Email:       in.Email,
		TglLahir:    in.TglLahir,
	})
	if err != nil {
		gateway.WriteError(w, err)
		return
	}
	gateway.WriteJSON(w, http.StatusCreated, idResponse{ID: p.ID})
}

// AttachEmployment menangani POST /admin/identity/employments.
func (h *AdminHandler) AttachEmployment(w http.ResponseWriter, r *http.Request) {
	ctx := gateway.FromRequest(r)
	if err := ctx.RequirePermission(domain.PermEmploymentLampir); err != nil {
		gateway.WriteError(w, err)
		return
	}
	var in attachEmploymentRequest
	if !decode(w, r, &in) {
		return
	}

	e, err := h.attachEmployment.Execute(ctx, usecase.AttachEmploymentInput{
		PersonID:     in.PersonID,
		Status:       domain.EmploymentStatus(in.Status),
		NIP:          in.NIP,
		InstansiAsal: in.InstansiAsal,
		ValidFrom:    in.ValidFrom,
		ValidUntil:   in.ValidUntil,
	})
	if err != nil {
		gateway.WriteError(w, err)
		return
	}
	gateway.WriteJSON(w, http.StatusCreated, employmentResponse{
		ID: e.ID, PersonID: e.PersonID, Status: string(e.Status),
	})
}

// CreateCredential menangani POST /admin/identity/credentials — jalur tulis password satu-satunya.
func (h *AdminHandler) CreateCredential(w http.ResponseWriter, r *http.Request) {
	ctx := gateway.FromRequest(r)
	if err := ctx.RequirePermission(domain.PermCredentialBuat); err != nil {
		gateway.WriteError(w, err)
		return
	}
	var in createCredentialRequest
	if !decode(w, r, &in) {
		return
	}

	c, err := h.createCredential.Execute(ctx, usecase.CreateCredentialInput{
		PersonID:  in.PersonID,
		CredType:  domain.CredType(in.CredType),
		CredValue: in.CredValue,
		Password:  in.Password,
		IsPrimary: in.IsPrimary,
	})
	if err != nil {
		gateway.WriteError(w, err)
		return
	}
	gateway.WriteJSON(w, http.StatusCreated, credentialResponse{
		ID: c.ID, PersonID: c.PersonID, CredType: string(c.CredType),
	})
}

// AssignEmploymentToTenant menangani POST /admin/identity/assignments — penugasan employment ke
// tenant, dan lewat event `identity.employment.ditugaskan` pemicu clone ke gov.user_profiles
// tenant tujuan.
//
// Handler memeriksa permission DASAR saja. Permission cross-tenant (PJ/PLT, is_home_tenant=false)
// ditegakkan use case, dan memang harus di sana: apakah penugasan ini cross-tenant baru diketahui
// SETELAH body di-parse, sementara aturan #3 mewajibkan gerbang pertama berdiri sebelum parse.
// Menegakkannya di sini berarti memindahkan keputusan otorisasi ke lapis yang membaca input
// mentah — dan menduplikasinya ke dua tempat yang bisa menyimpang.
func (h *AdminHandler) AssignEmploymentToTenant(w http.ResponseWriter, r *http.Request) {
	ctx := gateway.FromRequest(r)
	if err := ctx.RequirePermission(domain.PermAssignmentTugaskan); err != nil {
		gateway.WriteError(w, err)
		return
	}
	var in assignmentRequest
	if !decode(w, r, &in) {
		return
	}

	a, err := h.assignTenant.Execute(ctx, usecase.AssignEmploymentToTenantInput{
		EmploymentID: in.EmploymentID,
		TenantID:     in.TenantID,
		CrossTenant:  in.CrossTenant,
		ValidFrom:    in.ValidFrom,
		ValidUntil:   in.ValidUntil,
	})
	if err != nil {
		gateway.WriteError(w, err)
		return
	}
	gateway.WriteJSON(w, http.StatusCreated, assignmentResponse{
		ID: a.ID, EmploymentID: a.EmploymentID, TenantID: a.TenantID, IsHomeTenant: a.IsHomeTenant,
	})
}

// AssignCentralRole menangani POST /admin/identity/central-role-assignments — pemberian wewenang
// LINTAS TENANT, permukaan paling sensitif di grup ini.
func (h *AdminHandler) AssignCentralRole(w http.ResponseWriter, r *http.Request) {
	ctx := gateway.FromRequest(r)
	if err := ctx.RequirePermission(domain.PermCentralRoleAssign); err != nil {
		gateway.WriteError(w, err)
		return
	}
	var in centralRoleAssignmentRequest
	if !decode(w, r, &in) {
		return
	}

	a, err := h.assignRole.Execute(ctx, usecase.AssignCentralRoleInput{
		PersonID:    in.PersonID,
		RoleID:      in.RoleID,
		TenantScope: in.TenantScope,
		ValidFrom:   in.ValidFrom,
		ValidUntil:  in.ValidUntil,
	})
	if err != nil {
		gateway.WriteError(w, err)
		return
	}
	gateway.WriteJSON(w, http.StatusCreated, centralRoleAssignmentResponse{
		ID: a.ID, PersonID: a.PersonID, RoleID: a.RoleID, TenantScope: a.TenantScope,
	})
}
