package db

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core/permission"
	"github.com/huda-salam/pamong/delegation/domain"
)

// DelegationScopedGrantResolver membangun scoped-grant dari delegasi AKTIF untuk delegatee
// (ToUserID): tiap permission yang didelegasikan menjadi Grant dengan jangkauan unit delegasi.
// Inilah masukan Authority.DelegatedGrants (Tahap 2 ABAC) — jalur MANDIRI yang membuat delegatee
// memegang perm meski tak ada di role-nya. Kedaluwarsa difilter di repo (lazy).
type DelegationScopedGrantResolver struct {
	repo domain.DelegationRepository
	now  func() time.Time
}

func NewDelegationScopedGrantResolver(repo domain.DelegationRepository) *DelegationScopedGrantResolver {
	return &DelegationScopedGrantResolver{repo: repo, now: time.Now}
}

// Grants mengembalikan scoped-grant hasil delegasi aktif untuk user (sebagai delegatee).
func (r *DelegationScopedGrantResolver) Grants(ctx context.Context, userID uuid.UUID) ([]permission.Grant, error) {
	dels, err := r.repo.ListActiveByDelegatee(ctx, userID, r.now())
	if err != nil {
		return nil, err
	}
	var out []permission.Grant
	for _, d := range dels {
		for _, p := range d.Permissions {
			g := permission.Grant{Permission: p}
			if d.UnitKerjaID == nil {
				g.TenantWide = true
			} else {
				g.UnitKerjaID = *d.UnitKerjaID
				g.Subtree = d.IncludeSubtree
			}
			out = append(out, g)
		}
	}
	return out, nil
}
