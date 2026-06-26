package permission

import (
	"context"

	"github.com/google/uuid"

	"github.com/huda-salam/pamong/port"
)

// ScopedEngine menambahkan evaluasi data-level (ABAC + hierarki OPD + delegasi) di atas Engine
// RBAC, tanpa mengubah kontrak Engine (titik ekstensi #1, Open/Closed). Keputusan akhir:
//
//		AllowsInUnit =  ( Engine.Allows(RoleNames, perm)  AND  jangkauan RoleGrants menutupi unit )
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
	if s.engine.Allows(auth.RoleNames, perm) {
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
