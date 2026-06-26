package db

import (
	"context"

	"github.com/huda-salam/pamong/core/permission"
	"github.com/huda-salam/pamong/tenantrole/domain"
)

// TenantRoleCatalog adalah permission.RoleCatalog berbasis DB untuk lapis tenant. Memuat
// snapshot definisi role tenant saat dibangun (dan saat Refresh) sehingga Lookup murni
// in-memory: interface RoleCatalog (PR-2.3.1, Lookup tanpa context) tidak berubah dan tidak
// ada I/O di jalur evaluasi permission — sama persis pola CentralRoleCatalog (lapis central).
//
// Berbeda dari central yang satu snapshot per-proses, catalog tenant bersifat PER-TENANT
// (sumbernya repo yang terikat pool satu tenant). Saat wiring auth (2.4), tiap tenant punya
// catalog-nya sendiri, lalu digabung dengan central via permission.CompositeCatalog. Cache &
// refresh-on-change lintas tenant adalah ranah wiring 2.4; di 2.3.3 dikonstruksi langsung.
type TenantRoleCatalog struct {
	repo  domain.TenantRoleRepository
	roles map[string]permission.Role
}

var _ permission.RoleCatalog = (*TenantRoleCatalog)(nil)

// NewTenantRoleCatalog membangun catalog dan memuat snapshot awal dari DB tenant.
func NewTenantRoleCatalog(ctx context.Context, repo domain.TenantRoleRepository) (*TenantRoleCatalog, error) {
	c := &TenantRoleCatalog{repo: repo, roles: map[string]permission.Role{}}
	if err := c.Refresh(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

// Refresh memuat ulang snapshot definisi role tenant dari DB.
func (c *TenantRoleCatalog) Refresh(ctx context.Context) error {
	list, err := c.repo.List(ctx)
	if err != nil {
		return err
	}
	next := make(map[string]permission.Role, len(list))
	for _, r := range list {
		next[r.Name] = permission.Role{
			Name:        r.Name,
			Layer:       permission.LayerTenant, // role tenant selalu lapis tenant
			Permissions: r.Permissions,
		}
	}
	c.roles = next
	return nil
}

// Lookup mengembalikan definisi role dan true bila terdaftar (in-memory, tanpa I/O).
func (c *TenantRoleCatalog) Lookup(name string) (permission.Role, bool) {
	r, ok := c.roles[name]
	return r, ok
}
