package port

import (
	"context"

	"github.com/google/uuid"
)

// RoleOrigin adalah LAPIS ASAL sebuah nama role: dari mana nama itu datang di klaim token.
// Bukan sinonim dari permission.Layer — Layer adalah properti DEFINISI role (global/scoped/
// tenant, dibaca dari katalog), sedangkan RoleOrigin adalah properti RUJUKAN: klaim mana yang
// membawanya. Keduanya dipisah karena justru pemetaan origin→definisi itulah yang harus
// dikurung: nama dari klaim tenant hanya boleh me-resolve ke katalog tenant.
//
// JWT Pamong sudah memisahkan `central_roles` dari `tenant_roles` sejak PR-2.4.1; RoleOrigin
// adalah pemisahan itu yang DIPERTAHANKAN sampai titik evaluasi, alih-alih diratakan jadi satu
// daftar nama telanjang (ADR-019, REVIEW_BACKLOG B8).
type RoleOrigin int

const (
	// RoleOriginTenant — nama datang dari klaim tenant_roles (gov.tenant_roles).
	RoleOriginTenant RoleOrigin = iota
	// RoleOriginCentral — nama datang dari klaim central_roles (id.central_roles).
	RoleOriginCentral
)

// RoleRef adalah rujukan ke satu role yang MEMBAWA lapis asalnya. Ia menggantikan nama
// telanjang sebagai masukan evaluasi: tanpa Origin, role tenant bernama sama dengan role
// sentral akan me-resolve ke definisi sentral dan mewarisi LayerGlobal (B8).
type RoleRef struct {
	Origin RoleOrigin
	Name   string
}

// PermissionEvaluator mengevaluasi keputusan RBAC: diberi role yang dipegang actor
// (nama + lapis asal), apakah sebuah permission diberikan. Diimplementasi oleh
// core/permission.Engine. gateway.Context memakainya untuk RequirePermission
// tanpa bergantung pada implementasi konkret (core/* hanya lewat port).
type PermissionEvaluator interface {
	// Allows melaporkan apakah salah satu role memberi permission perm.
	Allows(roles []RoleRef, perm string) bool
}

// ScopedEvaluator mengevaluasi keputusan permission DATA-LEVEL (ABAC, PR-2.3.5) untuk SATU
// actor (actor-bound): apakah actor boleh melakukan perm atas resource yang dimiliki unitID.
// Diimplementasi oleh core/permission.ScopedEngine.Bind(Authority). gateway.Context memakainya
// untuk RequirePermissionInUnit tanpa bergantung pada tipe core. Berbeda dari
// PermissionEvaluator yang menjawab "punya permission?" tanpa scope.
type ScopedEvaluator interface {
	AllowsInUnit(ctx context.Context, perm string, unitID uuid.UUID) (bool, error)

	// AllowsSubtree melaporkan apakah actor berwenang atas unitID BESERTA SELURUH KETURUNANNYA
	// pada hierarki OPD. Ia BUKAN AllowsInUnit yang dipanggil berulang: pertanyaannya berbeda
	// secara mendasar, dan hanya wewenang yang memang menjangkau turunan (grant ber-Subtree atas
	// unit itu/leluhurnya, atau TenantWide) yang boleh menjawab ya.
	//
	// Dipakai saat actor MEMBERIKAN jangkauan subtree kepada orang lain (ADR-021): tanpa
	// pertanyaan ini, pemegang wewenang atas satu unit SAJA bisa menerbitkan assignment/delegasi
	// ber-`include_subtree` pada unit itu dan dengan begitu memberi jangkauan atas seluruh
	// keturunan yang ia sendiri tak punya.
	AllowsSubtree(ctx context.Context, perm string, unitID uuid.UUID) (bool, error)
}
