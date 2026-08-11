// Package http adalah driving adapter delegasi/PLT (`/admin/iam/delegations`, PR-W3b):
// melimpahkan subset permission ke pelaksana untuk rentang waktu terbatas.
//
// Dipasang pada ROUTER BISNIS (di dalam RequireAuth) — ia memutasi wewenang, dan lebih tajam
// daripada penugasan role: delegasi adalah jalur MANDIRI di evaluator (tak tunduk
// strict-intersection role), jadi ia bisa memberi delegatee permission yang tak ada di role-nya.
package http

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/huda-salam/pamong/delegation/domain"
	"github.com/huda-salam/pamong/delegation/usecase"
	"github.com/huda-salam/pamong/gateway"
)

// Handler adalah driving adapter delegasi.
type Handler struct {
	create *usecase.CreateDelegation
}

// NewHandler merakit handler; use case wajib non-nil (rute yang menunjuk nil baru panic pada
// request pertama, di produksi).
func NewHandler(create *usecase.CreateDelegation) *Handler {
	if create == nil {
		panic("delegation/adapter/http: CreateDelegation nil")
	}
	return &Handler{create: create}
}

// createRequest — UnitKerjaID pointer: null = SELURUH TENANT, jangkauan terluas, yang menuntut
// wewenang se-tenant pada pembuatnya (ADR-021). Jangan menormalkannya ke uuid.Nil.
type createRequest struct {
	FromUserID     uuid.UUID  `json:"from_user_id"`
	ToUserID       uuid.UUID  `json:"to_user_id"`
	Permissions    []string   `json:"permissions"`
	UnitKerjaID    *uuid.UUID `json:"unit_kerja_id"`
	IncludeSubtree bool       `json:"include_subtree"`
	Reason         string     `json:"reason"`
	ValidFrom      time.Time  `json:"valid_from"`
	ValidUntil     time.Time  `json:"valid_until"`
}

type idResponse struct {
	ID uuid.UUID `json:"id"`
}

// CreateDelegation menangani POST /admin/iam/delegations. Permission di baris pertama (RBAC);
// containment jangkauan unit ditegakkan use case agar pemanggil non-HTTP ikut terlindungi.
//
// Gerbang RBAC di sini menilai KLAIM TOKEN saja, sehingga wewenang lewat delegasi tak terlihat —
// benar untuk permukaan ini karena `iam:*` non-delegable (`domain.DefaultNonDelegable`): kemampuan
// MELIMPAHKAN tak boleh ikut dilimpahkan, sebab sekali lolos ia bisa dilebarkan berantai.
func (h *Handler) CreateDelegation(w http.ResponseWriter, r *http.Request) {
	ctx := gateway.FromRequest(r)
	if err := ctx.RequirePermission(domain.PermDelegasiBuat); err != nil {
		gateway.WriteError(w, err)
		return
	}
	var in createRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&in); err != nil {
		gateway.WriteError(w, gateway.ErrBadRequest("body tidak valid"))
		return
	}

	d, err := h.create.Execute(ctx, usecase.CreateDelegationInput{
		FromUserID:     in.FromUserID,
		ToUserID:       in.ToUserID,
		Permissions:    in.Permissions,
		UnitKerjaID:    in.UnitKerjaID,
		IncludeSubtree: in.IncludeSubtree,
		Reason:         in.Reason,
		ValidFrom:      in.ValidFrom,
		ValidUntil:     in.ValidUntil,
	})
	if err != nil {
		gateway.WriteError(w, err)
		return
	}
	gateway.WriteJSON(w, http.StatusCreated, idResponse{ID: d.ID})
}
