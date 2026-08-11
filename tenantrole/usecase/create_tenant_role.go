// Package usecase berisi orkestrator role tenant (PR-2.3.3): membuat & menugaskan role
// tenant. Dikelola admin tenant; berlaku hanya di dalam tenant-nya. Business logic murni —
// hanya bergantung pada domain/ports (hexagonal). Pola mengikuti use case role sentral.
package usecase

import (
	"strings"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/port"
	"github.com/huda-salam/pamong/tenantrole/domain"
)

// CreateTenantRole membuat definisi role tenant beserta grant permission-nya. Role tersimpan
// di tenant DB (gov.tenant_roles) dan dibaca lapis catalog DB tenant (core/permission) untuk
// evaluasi. Mutasi ter-audit lewat dekorator repo (ADR-003).
//
// CONTAINMENT ISI (PR-W3b, ADR-021 Keputusan 6): permission yang MEMBERI wewenang (`iam:*`) hanya
// boleh ditaruh di dalam role oleh pembuat yang MEMEGANGNYA sendiri — lihat grantingPermissionPrefix.
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

// grantingPermissionPrefix adalah namespace permission yang MEMBERI wewenang: membuat role tenant,
// menugaskannya, membuat delegasi. Ia dipagari secara berbeda dari `identity:` (yang DILARANG mutlak
// di role tenant, lihat domain.reservedPermissionPrefix): `iam:` memang permission tenant yang sah —
// admin tenant harus bisa menyusun role IAM untuk wakilnya — tapi hanya sejauh wewenang yang ia
// pegang sendiri.
//
// Yang ditutup: containment ADR-021 menjaga DI MANA wewenang boleh diberikan, bukan APA yang boleh
// diberikan. Tanpa pagar ini, pemegang `iam:tenant_role:buat` + `iam:tenant_role:assign` dapat
// mencetak role berisi `iam:delegasi:buat` — permission yang tak pernah diberikan kepadanya —
// menugaskannya kepada dirinya sendiri di dalam unitnya (lolos containment, karena unitnya memang
// unitnya), lalu memakainya. Ia juga membuat larangan `iam:*` pada delegasi (DefaultNonDelegable)
// berhenti menjadi dekorasi: tanpa pagar ini, yang dilarang lewat pintu delegasi tinggal dicetak
// lewat pintu role.
//
// Yang SENGAJA tidak ditutup: permission bisnis (mis. `keuangan:spm:terbitkan`) tetap bebas
// ditaruh — memaksa pembuat role memegangnya berarti admin IAM harus lebih dulu menjadi bendahara,
// dan itu mematikan administrasi role sebagai pekerjaan tersendiri. Konsekuensinya dicatat eksplisit
// di ADR-021: memegang `iam:tenant_role:buat` BERSAMA `iam:tenant_role:assign` setara dengan
// memegang seluruh permission bisnis tenant dalam jangkauan unit sendiri — jadi pasangan itu
// tergolong wewenang setingkat admin tenant, bukan permission yang dibagikan ringan.
const grantingPermissionPrefix = "iam:"

// Execute: permission → containment isi → bentuk entity → validasi → persist (role + grant atomik).
func (uc *CreateTenantRole) Execute(ctx port.AuthContext, in CreateTenantRoleInput) (*domain.TenantRole, error) {
	if err := ctx.RequirePermission(domain.PermTenantRoleBuat); err != nil {
		return nil, err
	}
	// Pemeriksaan RBAC biasa (bukan ber-scope): definisi role tak punya unit — unitnya baru muncul
	// saat PENUGASAN, dan di sanalah containment jangkauan bekerja. Pertanyaan di sini murni
	// "apakah kau memegang ini sama sekali?".
	for _, p := range in.Permissions {
		if !strings.HasPrefix(p, grantingPermissionPrefix) {
			continue
		}
		if err := ctx.RequirePermission(p); err != nil {
			return nil, err
		}
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
