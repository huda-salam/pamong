package db

import (
	"context"

	"github.com/huda-salam/pamong/core/permission"
	"github.com/huda-salam/pamong/identity/domain"
)

// CentralRoleCatalog adalah permission.RoleCatalog berbasis DB untuk lapis central. Memuat
// snapshot definisi role sentral saat dibangun (dan saat Refresh) sehingga Lookup murni
// in-memory: interface RoleCatalog (PR-2.3.1, Lookup tanpa context) tidak berubah dan tidak
// ada I/O di jalur evaluasi permission. Definisi role jarang berubah (deploy / aksi admin);
// Refresh dipicu saat perubahan itu. Penerapan titik ekstensi #1 (Open/Closed) — Engine
// tidak tersentuh saat lapis catalog DB ditambahkan.
type CentralRoleCatalog struct {
	repo  domain.CentralRoleRepository
	roles map[string]permission.Role
}

var _ permission.RoleCatalog = (*CentralRoleCatalog)(nil)

// NewCentralRoleCatalog membangun catalog dan memuat snapshot awal dari DB.
func NewCentralRoleCatalog(ctx context.Context, repo domain.CentralRoleRepository) (*CentralRoleCatalog, error) {
	c := &CentralRoleCatalog{repo: repo, roles: map[string]permission.Role{}}
	if err := c.Refresh(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

// Refresh memuat ulang snapshot definisi role sentral dari DB.
func (c *CentralRoleCatalog) Refresh(ctx context.Context) error {
	list, err := c.repo.List(ctx)
	if err != nil {
		return err
	}
	next := make(map[string]permission.Role, len(list))
	for _, r := range list {
		next[r.Name] = permission.Role{
			Name:        r.Name,
			Layer:       layerOf(r.ScopeType),
			Permissions: r.Permissions,
		}
	}
	c.roles = next
	return nil
}

// Lookup mengembalikan definisi role dan true bila terdaftar (in-memory, tanpa I/O).
func (c *CentralRoleCatalog) Lookup(name string) (permission.Role, bool) {
	r, ok := c.roles[name]
	return r, ok
}

// layerOf memetakan scope_type role sentral ke Layer engine: global → LayerGlobal (menang
// atas semua, ditegakkan penuh 2.3.3), scoped → LayerScoped (setara tenant di scope-nya).
func layerOf(s domain.ScopeType) permission.Layer {
	if s == domain.ScopeGlobal {
		return permission.LayerGlobal
	}
	return permission.LayerScoped
}
