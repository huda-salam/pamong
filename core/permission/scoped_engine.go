package permission

import (
	"context"

	"github.com/google/uuid"

	"github.com/huda-salam/pamong/port"
)

// ScopedEngine menambahkan evaluasi data-level (ABAC + hierarki OPD + delegasi) di atas Engine
// RBAC, tanpa mengubah kontrak Engine (titik ekstensi #1, Open/Closed). Keputusan akhir:
//
//		AllowsInUnit =  ( Engine.Allows(Roles, perm)  AND  jangkauan RoleGrants menutupi unit )
//		             OR ( jangkauan DelegatedGrants menutupi unit )
//
//	  - Jalur role: RBAC (strict-intersection + global-precedence 2.3.3, UTUH) harus lulus dulu —
//	    bila menolak, tak ada scope yang menyelamatkan; lalu salah satu grant role harus
//	    menjangkau unit resource (union antar grant).
//	  - Jalur delegasi: pelimpahan eksplisit — cukup jangkauan delegasi menutupi unit. Sengaja
//	    TIDAK tunduk pada strict-intersection role: delegatee menerima wewenang yang mungkin tak
//	    ada di role-nya (justru itu inti delegasi/PLT).
//
// Catatan MVP: interaksi halus strict×scope tidak diperdalam; "boleh pakai perm" tetap di Tahap 1,
// "menjangkau unit" di Tahap 2 (union). Tahun anggaran/periode = DEFERRED(Phase-3.x).
type ScopedEngine struct {
	engine *Engine
	tree   Hierarchy
}

// NewScopedEngine membungkus Engine RBAC dengan resolver hierarki OPD.
func NewScopedEngine(engine *Engine, tree Hierarchy) *ScopedEngine {
	return &ScopedEngine{engine: engine, tree: tree}
}

// AllowsInUnit melaporkan apakah actor (auth) boleh melakukan perm atas resource pada
// res.UnitKerjaID. Error hanya bila query hierarki gagal.
func (s *ScopedEngine) AllowsInUnit(ctx context.Context, auth Authority, perm Permission, res ResourceScope) (bool, error) {
	if s.engine.Allows(auth.Roles, perm) {
		ok, err := s.covers(ctx, auth.RoleGrants, perm, res)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	return s.covers(ctx, auth.DelegatedGrants, perm, res)
}

// AllowsSubtreeIn melaporkan apakah actor boleh melakukan perm atas res.UnitKerjaID DAN seluruh
// keturunannya. Bedanya dengan AllowsInUnit menentukan: grant yang terikat PERSIS pada unit itu
// (Subtree=false) menutupi unit itu tapi TIDAK keturunannya, jadi ia tak boleh menjawab ya di sini.
//
// Hanya dua bentuk wewenang yang menjangkau seluruh keturunan sebuah unit:
//   - TenantWide — menjangkau apa pun di tenant ini;
//   - grant ber-Subtree atas unit itu sendiri ATAU atas salah satu LELUHURNYA. Sifat tree yang
//     dipakai: bila unit berada di dalam subtree g.UnitKerjaID, maka seluruh keturunan unit juga
//     berada di dalamnya — jadi satu pemeriksaan IsWithin cukup, tanpa mengembangkan subtree.
//
// Jalur delegasi ikut dipertimbangkan dengan aturan yang sama (union), konsisten dengan
// AllowsInUnit: delegasi ber-Subtree memang melimpahkan jangkauan turunan.
func (s *ScopedEngine) AllowsSubtreeIn(ctx context.Context, auth Authority, perm Permission, res ResourceScope) (bool, error) {
	if s.engine.Allows(auth.Roles, perm) {
		ok, err := s.coversSubtree(ctx, auth.RoleGrants, perm, res)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	return s.coversSubtree(ctx, auth.DelegatedGrants, perm, res)
}

// coversSubtree melaporkan apakah ada grant atas perm yang jangkauannya menutupi res BESERTA
// keturunannya (union antar grant).
func (s *ScopedEngine) coversSubtree(ctx context.Context, grants []Grant, perm Permission, res ResourceScope) (bool, error) {
	for _, g := range grants {
		if g.Permission != perm {
			continue
		}
		if g.TenantWide {
			return true, nil
		}
		if !g.Subtree {
			continue // terikat satu unit: tak menjangkau keturunan mana pun
		}
		if g.UnitKerjaID == res.UnitKerjaID {
			return true, nil
		}
		within, err := s.tree.IsWithin(ctx, g.UnitKerjaID, res.UnitKerjaID)
		if err != nil {
			return false, err
		}
		if within {
			return true, nil
		}
	}
	return false, nil
}

// covers melaporkan apakah ada grant atas perm yang jangkauannya menutupi res (union).
func (s *ScopedEngine) covers(ctx context.Context, grants []Grant, perm Permission, res ResourceScope) (bool, error) {
	for _, g := range grants {
		if g.Permission != perm {
			continue
		}
		if g.TenantWide || g.UnitKerjaID == res.UnitKerjaID {
			return true, nil
		}
		if g.Subtree {
			within, err := s.tree.IsWithin(ctx, g.UnitKerjaID, res.UnitKerjaID)
			if err != nil {
				return false, err
			}
			if within {
				return true, nil
			}
		}
	}
	return false, nil
}

// Bind mengikat ScopedEngine ke satu Authority menghasilkan port.ScopedEvaluator (actor-bound)
// untuk disuntik ke gateway.Context. Dipakai middleware auth (2.4); menjaga paket port bebas
// dari tipe core (Authority tetap di core/permission).
func (s *ScopedEngine) Bind(auth Authority) port.ScopedEvaluator {
	return boundScopedEvaluator{engine: s, auth: auth}
}

type boundScopedEvaluator struct {
	engine *ScopedEngine
	auth   Authority
}

func (b boundScopedEvaluator) AllowsInUnit(ctx context.Context, perm string, unitID uuid.UUID) (bool, error) {
	return b.engine.AllowsInUnit(ctx, b.auth, perm, ResourceScope{UnitKerjaID: unitID})
}

func (b boundScopedEvaluator) AllowsSubtree(ctx context.Context, perm string, unitID uuid.UUID) (bool, error) {
	return b.engine.AllowsSubtreeIn(ctx, b.auth, perm, ResourceScope{UnitKerjaID: unitID})
}
