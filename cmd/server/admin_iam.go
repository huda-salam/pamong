// admin_iam.go merakit grup HTTP administrasi wewenang TENANT (`/admin/iam/*`, PR-W3b) di
// composition root: definisi role tenant, penugasannya (dengan scope unit kerja), dan delegasi/PLT.
//
// Ia menutup dosa yang paling lama menganggur di repo ini: `RequirePermissionInUnit` — seluruh
// lapis ABAC (ScopedEngine, hierarki OPD, resolver grant, delegasi) sudah lengkap & teruji sejak
// PR-2.3.5, tapi TAK PUNYA SATU PUN pemanggil produksi. Memasang evaluator tanpa pemanggil akan
// mengulang persis pola yang Sub-phase 5.0 ada untuk membayarnya (DoD 11), jadi wiring evaluator
// (scoped_evaluator.go) dan permukaan yang MEMAKAINYA mendarat bersama.
//
// Semua di sini murni WIRING: kebijakan permission & containment ada di use case
// (core/permission.RequireAuthorityOver), kebijakan audit di dekorator repo (ADR-003).
package main

import (
	"fmt"
	"net/http"

	"github.com/huda-salam/pamong/core/audit"
	delegationdb "github.com/huda-salam/pamong/delegation/adapter/db"
	delegationhttp "github.com/huda-salam/pamong/delegation/adapter/http"
	delegationdomain "github.com/huda-salam/pamong/delegation/domain"
	delegationusecase "github.com/huda-salam/pamong/delegation/usecase"
	"github.com/huda-salam/pamong/infra/db"
	"github.com/huda-salam/pamong/port"
	tenantroledb "github.com/huda-salam/pamong/tenantrole/adapter/db"
	tenantrolehttp "github.com/huda-salam/pamong/tenantrole/adapter/http"
	tenantroleusecase "github.com/huda-salam/pamong/tenantrole/usecase"
)

// adminIAMRoutes adalah kontrak minimal yang dibutuhkan mountAdminIAMRoutes, dipenuhi oleh
// handler tenantrole + delegation. Seam ini ada agar PEMASANGAN rute (dan karenanya stack
// middleware di sekelilingnya — terutama RequireAuth) dapat diuji tanpa merakit DB tenant.
// Cermin adminIdentityRoutes, dengan kesimpulan yang sama: grup ini HARUS di balik RequireAuth.
type adminIAMRoutes interface {
	CreateTenantRole(http.ResponseWriter, *http.Request)
	AssignTenantRole(http.ResponseWriter, *http.Request)
	CreateDelegation(http.ResponseWriter, *http.Request)
}

// adminIAMHandler menggabungkan dua handler komponen menjadi satu permukaan rute. Keduanya tetap
// paket terpisah (tenantrole & delegation adalah komponen berbeda yang merujuk PRD induk yang
// sama, core/permission/PRD.md); yang disatukan hanya PEMASANGANNYA.
//
// Field bernama, bukan embedding: keduanya bertipe `Handler` dari paket berbeda, dan embedding
// keduanya tak bisa dikompilasi. Metode diteruskan eksplisit di bawah — itu sekalian membuat
// permukaan rute grup ini terbaca dari satu tempat.
type adminIAMHandler struct {
	tenantRole *tenantrolehttp.Handler
	delegation *delegationhttp.Handler
}

var _ adminIAMRoutes = (*adminIAMHandler)(nil)

func (h *adminIAMHandler) CreateTenantRole(w http.ResponseWriter, r *http.Request) {
	h.tenantRole.CreateTenantRole(w, r)
}

func (h *adminIAMHandler) AssignTenantRole(w http.ResponseWriter, r *http.Request) {
	h.tenantRole.AssignTenantRole(w, r)
}

func (h *adminIAMHandler) CreateDelegation(w http.ResponseWriter, r *http.Request) {
	h.delegation.CreateDelegation(w, r)
}

// wireAdminIAM merakit use case administrasi wewenang tenant beserta repo ber-AUDIT.
//
// Pilihan perakitan yang tidak sembarang:
//
//  1. **Repo TENANT, bukan identity.** Role tenant & delegasi hidup di tenant DB. `TenantRoutingConn`
//     me-resolve tenant dari KLAIM token per-request (port.TenantFrom), jadi satu perakitan saat
//     boot melayani semua tenant tanpa tenant pernah datang dari body.
//
//  2. **Semuanya dibungkus dekorator audit.** Seluruh grup ini mutasi wewenang oleh aktor
//     terotentikasi — justru kelas mutasi yang paling perlu dapat dijawab "siapa memberi wewenang
//     ini, kapan" (ADR-003). Audit engine memakai store TENANT (gov.audit_logs), bukan store
//     sentral: entrinya milik tenant tempat wewenang itu berlaku, dan `audit.Reader` membangun
//     partisi chain dari tenant entri.
//
//  3. **Tak ada CryptoPort di sini.** Tak satu pun field di grup ini berkelas `personal_id`
//     (nama role, label, permission, id unit) — jadi tak ada yang perlu disegel, dan menambahkan
//     kripto "supaya konsisten" hanya akan mengaburkan mana yang benar-benar pengenal.
func wireAdminIAM(tenantConn db.TxConn, auditStore audit.Store) (*adminIAMHandler, error) {
	if tenantConn == nil {
		return nil, fmt.Errorf("koneksi tenant nil")
	}
	if auditStore == nil {
		return nil, fmt.Errorf("audit store nil")
	}
	auditEngine := audit.NewEngine(auditStore)

	roles := tenantroledb.NewAuditedTenantRoleRepo(tenantroledb.NewTenantRoleRepo(tenantConn), auditEngine)
	assignments := tenantroledb.NewAuditedTenantRoleAssignmentRepo(
		tenantroledb.NewTenantRoleAssignmentRepo(tenantConn), auditEngine)
	delegations := delegationdb.NewAuditedDelegationRepo(
		delegationdb.NewDelegationRepo(tenantConn), auditEngine)

	return &adminIAMHandler{
		tenantRole: tenantrolehttp.NewHandler(
			tenantroleusecase.NewCreateTenantRole(roles),
			tenantroleusecase.NewAssignTenantRole(assignments),
		),
		// DefaultNonDelegable, BUKAN NewNonDelegableSet() kosong: himpunan kosong membuat delegasi
		// jadi jalur pemberian wewenang tanpa pagar apa pun — termasuk `identity:*` yang sudah
		// dipagari agar tak bisa masuk role tenant, dan `iam:*` yang melimpahkan kemampuan
		// melimpahkan. Ia pelengkap containment unit, bukan penggantinya: yang satu membatasi APA
		// yang boleh dilimpahkan, yang lain membatasi DI MANA.
		delegation: delegationhttp.NewHandler(
			delegationusecase.NewCreateDelegation(delegations, delegationdomain.DefaultNonDelegable()),
		),
	}, nil
}

// mountAdminIAMRoutes memasang grup /admin/iam/* pada ROUTER BISNIS — alasannya identik dengan
// /admin/identity/* (lihat mountAdminIdentityRoutes): rute menuntut token, aktornya harus
// ber-principal nyata agar rate limit per-orang punya arti, dan seluruhnya mutasi sehingga
// Idempotency-Key layak dihormati.
func mountAdminIAMRoutes(r port.Router, h adminIAMRoutes) {
	r.Post("/admin/iam/tenant-roles", h.CreateTenantRole)
	r.Post("/admin/iam/tenant-role-assignments", h.AssignTenantRole)
	r.Post("/admin/iam/delegations", h.CreateDelegation)
}
