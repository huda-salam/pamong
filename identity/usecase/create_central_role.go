package usecase

import (
	"github.com/google/uuid"
	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/port"
)

// CreateCentralRole membuat definisi role sentral (global/scoped) beserta grant permission-nya.
// Dikelola admin platform; berlaku lintas tenant. Role tersimpan di identity DB dan dibaca
// lapis catalog DB (core/permission) untuk evaluasi.
//
// Tidak menerbitkan event di PR ini: belum ada konsumen (identity.central_role.diassign untuk
// refresh/revoke token adalah ranah auth flow). DEFERRED(Phase-2.4): publish event role sentral.
type CreateCentralRole struct {
	roles domain.CentralRoleRepository
}

func NewCreateCentralRole(roles domain.CentralRoleRepository) *CreateCentralRole {
	return &CreateCentralRole{roles: roles}
}

// CreateCentralRoleInput DTO masuk. Permissions berisi string {modul}:{entity}:{aksi} yang
// diberikan role ini (sumber: manifest modul); validasi terhadap registry manifest menyusul 2.3.4.
type CreateCentralRoleInput struct {
	Name        string
	Label       string
	ScopeType   domain.ScopeType
	Description string
	Permissions []string
}

// Execute: permission -> bentuk entity -> validasi -> persist (role + grant atomik).
func (uc *CreateCentralRole) Execute(ctx port.AuthContext, in CreateCentralRoleInput) (*domain.CentralRole, error) {
	if err := ctx.RequirePermission(domain.PermCentralRoleBuat); err != nil {
		return nil, err
	}

	r := &domain.CentralRole{
		ID:          uuid.New(),
		Name:        in.Name,
		Label:       in.Label,
		ScopeType:   in.ScopeType,
		Description: in.Description,
		// Dedup agar entity yang dikembalikan konsisten dengan yang dipersist (review PR-2.3.2):
		// grant role→permission adalah himpunan, "baca" sekali/dua kali sama saja. Repo juga
		// idempoten (ON CONFLICT) sebagai batas pertahanan untuk caller non-use-case.
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
