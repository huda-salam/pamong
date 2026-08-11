// Package http adalah driving adapter administrasi role tenant (`/admin/iam/*`, PR-W3b):
// membuat definisi role tenant dan menugaskannya ke user, dengan opsi scope unit kerja.
//
// Ia dipasang pada ROUTER BISNIS — di dalam stack lengkap (Auth → RequireAuth → TenantResolver →
// RateLimit → Idempotency) — sama seperti `/admin/identity/*` dan untuk alasan yang sama: setiap
// endpointnya memutasi wewenang. Jangan menirunya dari `/auth/*`, yang justru sengaja berada di
// LUAR RequireAuth karena login adalah pra-otentikasi.
//
// Tenant tak pernah datang dari body: repo menulis ke tenant DB yang di-resolve dari KLAIM token
// (infra/db.TenantRoutingConn ← port.TenantFrom), jadi tak ada field tenant_id di DTO mana pun
// di sini. Endpoint ini karenanya selalu bekerja pada tenant si aktor.
package http

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/huda-salam/pamong/gateway"
	"github.com/huda-salam/pamong/tenantrole/domain"
	"github.com/huda-salam/pamong/tenantrole/usecase"
)

// Handler adalah driving adapter role tenant.
type Handler struct {
	createRole *usecase.CreateTenantRole
	assignRole *usecase.AssignTenantRole
}

// NewHandler merakit handler. Use case wajib non-nil dan itu DITEGAKKAN di sini: rute yang
// terdaftar tapi menunjuk use case nil baru panic pada request pertama, di produksi.
func NewHandler(createRole *usecase.CreateTenantRole, assignRole *usecase.AssignTenantRole) *Handler {
	switch {
	case createRole == nil:
		panic("tenantrole/adapter/http: CreateTenantRole nil")
	case assignRole == nil:
		panic("tenantrole/adapter/http: AssignTenantRole nil")
	}
	return &Handler{createRole: createRole, assignRole: assignRole}
}

// --- DTO kawat ---
//
// Ditulis eksplisit, bukan memakai ulang struct use case: bentuk kawat adalah kontrak dengan
// klien dan harus bisa berubah terpisah dari bentuk internal.

type createRoleRequest struct {
	Name        string   `json:"name"`
	Label       string   `json:"label"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
}

// assignRoleRequest — UnitKerjaID adalah POINTER dan bedanya bermakna otorisasi: null berarti
// "seluruh tenant" (jangkauan TERLUAS) dan karenanya menuntut wewenang se-tenant, bukan sekadar
// wewenang di satu unit (ADR-021). Karena itu ia tak boleh dinormalkan menjadi uuid.Nil di sini:
// domain menolak unit ber-UUID nol justru supaya "kosong" tak bisa menyelundup sebagai "sebuah
// unit yang kebetulan nol".
type assignRoleRequest struct {
	UserID         uuid.UUID  `json:"user_id"`
	RoleID         uuid.UUID  `json:"role_id"`
	UnitKerjaID    *uuid.UUID `json:"unit_kerja_id"`
	IncludeSubtree bool       `json:"include_subtree"`
	ValidFrom      time.Time  `json:"valid_from"`
	ValidUntil     *time.Time `json:"valid_until"`
}

type idResponse struct {
	ID uuid.UUID `json:"id"`
}

// --- Handler ---
//
// Permission di baris PERTAMA, sebelum Bind (CLAUDE.md aturan #3). Use case memeriksa lagi — dan
// pada AssignTenantRole ia memeriksa LEBIH: gerbang di sini murni RBAC ("boleh menugaskan"),
// sedangkan containment jangkauan ("boleh menugaskan DI SINI") hidup di use case supaya pemanggil
// non-HTTP (CLI, importer, workflow) ikut terlindungi.
//
// Gerbang RBAC di handler menilai KLAIM TOKEN saja, jadi wewenang yang datang lewat DELEGASI tak
// terlihat di sini — dan itu benar untuk permukaan ini: `iam:*` termasuk
// `domain.DefaultNonDelegable`, jadi tak seorang pun bisa memegang permission grup ini lewat
// delegasi. Bila kelak ada permukaan yang permission-nya BOLEH didelegasikan, gerbang handler-nya
// harus dipikir ulang (ia akan menolak PLT sebelum jalur delegasi di use case pernah dievaluasi) —
// lihat ADR-021 §Konsekuensi.

// CreateTenantRole menangani POST /admin/iam/tenant-roles.
func (h *Handler) CreateTenantRole(w http.ResponseWriter, r *http.Request) {
	ctx := gateway.FromRequest(r)
	if err := ctx.RequirePermission(domain.PermTenantRoleBuat); err != nil {
		gateway.WriteError(w, err)
		return
	}
	var in createRoleRequest
	if !decode(w, r, &in) {
		return
	}

	role, err := h.createRole.Execute(ctx, usecase.CreateTenantRoleInput{
		Name:        in.Name,
		Label:       in.Label,
		Description: in.Description,
		Permissions: in.Permissions,
	})
	if err != nil {
		gateway.WriteError(w, err)
		return
	}
	gateway.WriteJSON(w, http.StatusCreated, idResponse{ID: role.ID})
}

// AssignTenantRole menangani POST /admin/iam/tenant-role-assignments.
func (h *Handler) AssignTenantRole(w http.ResponseWriter, r *http.Request) {
	ctx := gateway.FromRequest(r)
	if err := ctx.RequirePermission(domain.PermTenantRoleAssign); err != nil {
		gateway.WriteError(w, err)
		return
	}
	var in assignRoleRequest
	if !decode(w, r, &in) {
		return
	}

	a, err := h.assignRole.Execute(ctx, usecase.AssignTenantRoleInput{
		UserID:         in.UserID,
		RoleID:         in.RoleID,
		UnitKerjaID:    in.UnitKerjaID,
		IncludeSubtree: in.IncludeSubtree,
		ValidFrom:      in.ValidFrom,
		ValidUntil:     in.ValidUntil,
	})
	if err != nil {
		gateway.WriteError(w, err)
		return
	}
	gateway.WriteJSON(w, http.StatusCreated, idResponse{ID: a.ID})
}
