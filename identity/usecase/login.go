package usecase

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/port"
)

// Alur login (PR-2.4.3) menerbitkan token internal scoped setelah memverifikasi credential.
// Login bersifat PRA-OTENTIKASI: seperti Resolver, ia TIDAK menerima port.AuthContext dan tidak
// melakukan permission check — actor belum punya identitas sampai token terbit (chicken-and-egg).
// Yang melindungi adalah verifikasi credential + password, bukan permission.
//
// INVARIANT KUNCI (ditegakkan di sini): saat menerbitkan token, hanya nama role yang BERLAKU
// untuk person pada tenant tsb yang dibakar ke klaim. Role sentral disaring per-tenant oleh
// CentralRoleResolver (global selalu, scoped hanya bila tenant cocok); role tenant berasal dari
// resolver yang terikat ke DB tenant itu. Inilah yang membuat HasCentralRole scope-blind di
// gateway aman (Context cuma membawa nama role yang sudah lolos saring). Token citizen tidak
// pernah memanggil resolver role → tidak mungkin membawa role internal.

// CentralRoleResolver me-resolve nama role sentral yang berlaku untuk person pada satu tenant
// (scope DISARING di sini). Consumer-defined port; dipenuhi oleh
// identity/adapter/db.CentralRoleResolver.
type CentralRoleResolver interface {
	EffectiveRoles(ctx context.Context, personID uuid.UUID, tenantID string) ([]string, error)
}

// TenantRoleResolver me-resolve nama role tenant untuk person pada satu tenant. Implementasinya
// terikat ke DB tenant ybs (isolasi struktural), karena itu menerima tenantID untuk memilih
// koneksi yang benar. Concrete-nya (atas tenantrole.TenantRoleResolver + TenantConnManager)
// di-wire di layer bootstrap — lihat DEFERRED(Phase-2.4) live wiring.
type TenantRoleResolver interface {
	EffectiveRoles(ctx context.Context, personID uuid.UUID, tenantID string) ([]string, error)
}

// LoginResult adalah hasil alur login employee.
//
//   - Tenant tunggal: Token = token scoped final, NeedTenantSelection=false.
//   - Tenant >1     : Token = token SEMENTARA (persona=employee, tanpa tenant & tanpa role —
//     hanya bisa dipakai memanggil SelectTenant), NeedTenantSelection=true, Tenants=daftar pilihan.
type LoginResult struct {
	Token               string
	NeedTenantSelection bool
	Tenants             []TenantChoice
}

// TenantChoice adalah satu tenant yang boleh dimasuki person (untuk ditampilkan saat pemilihan).
type TenantChoice struct {
	TenantID     string
	IsHomeTenant bool
}

// tenantOption adalah pilihan tenant internal lengkap dengan status employment pemilik
// penugasan (dipakai untuk klaim employment_status & is_cross_tenant token final).
type tenantOption struct {
	TenantID     string
	IsHomeTenant bool
	EmpStatus    string // status employment yang memiliki penugasan ke tenant ini
}

func (o tenantOption) choice() TenantChoice {
	return TenantChoice{TenantID: o.TenantID, IsHomeTenant: o.IsHomeTenant}
}

// employeeTenantResolver mengumpulkan tenant yang berhak dimasuki person: lintas SEMUA
// employment aktif, hanya penugasan yang masih berlaku, dan hanya tenant yang masih aktif di
// registry. Dipakai bersama oleh LoginEmployee (bangun pilihan) & SelectTenant (validasi pilihan).
type employeeTenantResolver struct {
	employments domain.EmploymentRepository
	assigns     domain.TenantAssignmentRepository
	tenants     domain.TenantRegistry
	now         func() time.Time
}

// resolve mengembalikan opsi tenant aktif untuk person. hasActiveEmployment=false bila person
// tak punya employment aktif sama sekali (orang biasa tak bisa masuk internal) — caller menolak
// dengan pesan yang sesuai. Opsi di-dedup per tenantID (home didahulukan bila bertabrakan).
func (r employeeTenantResolver) resolve(ctx context.Context, personID uuid.UUID) (opts []tenantOption, hasActiveEmployment bool, err error) {
	emps, err := r.employments.ListByPerson(ctx, personID)
	if err != nil {
		return nil, false, err
	}
	now := r.now()
	byTenant := map[string]tenantOption{}
	for _, e := range emps {
		if !e.IsActiveAt(now) {
			continue
		}
		hasActiveEmployment = true

		assigns, err := r.assigns.ListByEmployment(ctx, e.ID)
		if err != nil {
			return nil, false, err
		}
		for _, a := range assigns {
			if !a.AppliesTo(now) {
				continue
			}
			t, err := r.tenants.FindByID(ctx, a.TenantID)
			if err != nil {
				var fe *core.FrameworkError
				if errors.As(err, &fe) && fe.Code == "NOT_FOUND" {
					continue // tenant tak dikenal lagi → bukan pilihan valid
				}
				return nil, false, err
			}
			if !t.IsActive {
				continue
			}
			opt := tenantOption{TenantID: a.TenantID, IsHomeTenant: a.IsHomeTenant, EmpStatus: string(e.Status)}
			// Dedup: bila tenant sama datang dari dua employment, utamakan home tenant.
			if prev, ok := byTenant[a.TenantID]; ok && prev.IsHomeTenant && !opt.IsHomeTenant {
				continue
			}
			byTenant[a.TenantID] = opt
		}
	}
	for _, o := range byTenant {
		opts = append(opts, o)
	}
	return opts, hasActiveEmployment, nil
}

// scopedTokenMinter menerbitkan token scoped final untuk satu tenant. Di sinilah invariant
// penyaringan role ditegakkan: hanya role yang berlaku untuk (person, tenant) yang dibakar.
type scopedTokenMinter struct {
	central     CentralRoleResolver
	tenantRoles TenantRoleResolver
	issuer      port.TokenIssuer
}

func (m scopedTokenMinter) mint(ctx context.Context, personID uuid.UUID, opt tenantOption) (string, error) {
	centralRoles, err := m.central.EffectiveRoles(ctx, personID, opt.TenantID)
	if err != nil {
		return "", err
	}
	tenantRoles, err := m.tenantRoles.EffectiveRoles(ctx, personID, opt.TenantID)
	if err != nil {
		return "", err
	}
	return m.issuer.Issue(ctx, port.Claims{
		PersonID:         personID,
		Persona:          domain.PersonaEmployee,
		EmploymentStatus: opt.EmpStatus,
		TenantID:         opt.TenantID,
		CentralRoles:     centralRoles,
		TenantRoles:      tenantRoles,
		// TenantScope sengaja kosong: token sudah di-scope ke satu tenant dan role sentral di
		// dalamnya SUDAH disaring untuk tenant itu saat login. Gateway tidak mengevaluasi ulang
		// scope dari tenant_scope (HasCentralRole scope-blind; otorisasi lewat RequirePermission).
		IsCrossTenant: !opt.IsHomeTenant,
	})
}

// errInvalidCredential adalah respons SERAGAM untuk semua kegagalan pra-tenant (credential tak
// ada, hash kosong, password salah, person non-aktif) agar tidak membocorkan bagian mana yang
// gagal ke penyerang.
func errInvalidCredential() error {
	return core.ErrUnauthorized("kredensial tidak valid")
}
