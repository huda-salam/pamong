// Package usecase berisi orkestrator role tenant (PR-2.3.3): membuat & menugaskan role
// tenant. Dikelola admin tenant; berlaku hanya di dalam tenant-nya. Business logic murni —
// hanya bergantung pada domain/ports (hexagonal). Pola mengikuti use case role sentral.
package usecase

import (
	"github.com/google/uuid"
	"github.com/huda-salam/pamong/port"
	"github.com/huda-salam/pamong/tenantrole/domain"
)

// CreateTenantRole membuat definisi role tenant beserta grant permission-nya. Role tersimpan
// di tenant DB (gov.tenant_roles) dan dibaca lapis catalog DB tenant (core/permission) untuk
// evaluasi. Mutasi ter-audit lewat dekorator repo (ADR-003).
//
// Tidak menerbitkan event di PR ini: belum ada konsumen (refresh/revoke token = ranah auth
// flow). DEFERRED(Phase-2.4): publish event role tenant untuk refresh token.
type CreateTenantRole struct {
	roles domain.TenantRoleRepository
}

func NewCreateTenantRole(roles domain.TenantRoleRepository) *CreateTenantRole {
	return &CreateTenantRole{roles: roles}
}

// CreateTenantRoleInput DTO masuk. Permissions berisi string {modul}:{entity}:{aksi} yang
// diberikan role ini (sumber: manifest modul); validasi terhadap registry manifest menyusul 2.3.4.
type CreateTenantRoleInput struct {
	Name        string
	Label       string
	Description string
	Permissions []string
}

// Execute: permission → bentuk entity → validasi → persist (role + grant atomik).
func (uc *CreateTenantRole) Execute(ctx port.AuthContext, in CreateTenantRoleInput) (*domain.TenantRole, error) {
	if err := ctx.RequirePermission(domain.PermTenantRoleBuat); err != nil {
		return nil, err
	}

	r := &domain.TenantRole{
		ID:          uuid.New(),
		Name:        in.Name,
		Label:       in.Label,
		Description: in.Description,
		// Dedup agar entity yang dikembalikan konsisten dengan yang dipersist: grant
		// role→permission adalah himpunan. Repo juga idempoten (ON CONFLICT) sebagai batas
		// pertahanan untuk caller non-use-case.
		Permissions: dedupStrings(in.Permissions),
	}
	if err := r.Validate(); err != nil {
		return nil, err
	}
	if err := uc.roles.Save(ctx, r); err != nil {
		return nil, err
	}
	return r, nil
}

// dedupStrings mengembalikan elemen unik dengan urutan kemunculan pertama dipertahankan.
// nil/kosong → nil (role tanpa permission tetap valid).
func dedupStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
