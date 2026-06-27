package usecase

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/port"
)

// employeeCredTypes membatasi jalur masuk internal ke NIP (ASN) & NIK (non-ASN). Email/no_hp
// adalah jalur citizen — ditolak di sini agar persona ditentukan portal, bukan tebakan use case.
var employeeCredTypes = map[domain.CredType]bool{
	domain.CredNIP: true,
	domain.CredNIK: true,
}

// LoginEmployee adalah alur login persona employee (portal internal, sentral & daerah):
// verifikasi credential (NIP/NIK) + password → person → employment aktif → tenant assignment.
// Tenant tunggal langsung menerbitkan token scoped final; >1 tenant mengembalikan daftar pilihan
// + token sementara (pemilihan dilanjutkan SelectTenant). Ditolak bila tak ada employment aktif.
type LoginEmployee struct {
	creds     domain.CredentialRepository
	persons   domain.PersonRepository
	passwords port.PasswordVerifier
	resolver  employeeTenantResolver
	minter    scopedTokenMinter
	issuer    port.TokenIssuer
}

// NewLoginEmployee merakit alur login employee. central/tenantRoles disaring per-tenant saat
// penerbitan token (invariant scope).
func NewLoginEmployee(
	creds domain.CredentialRepository,
	persons domain.PersonRepository,
	employments domain.EmploymentRepository,
	assigns domain.TenantAssignmentRepository,
	tenants domain.TenantRegistry,
	passwords port.PasswordVerifier,
	central CentralRoleResolver,
	tenantRoles TenantRoleResolver,
	issuer port.TokenIssuer,
) *LoginEmployee {
	return &LoginEmployee{
		creds:     creds,
		persons:   persons,
		passwords: passwords,
		resolver:  employeeTenantResolver{employments: employments, assigns: assigns, tenants: tenants, now: time.Now},
		minter:    scopedTokenMinter{central: central, tenantRoles: tenantRoles, issuer: issuer},
		issuer:    issuer,
	}
}

// LoginEmployeeInput DTO masuk dari handler.
type LoginEmployeeInput struct {
	CredType  domain.CredType // nip | nik
	CredValue string
	Password  string
}

// Execute memverifikasi credential lalu menerbitkan token sesuai jumlah tenant.
func (uc *LoginEmployee) Execute(ctx context.Context, in LoginEmployeeInput) (*LoginResult, error) {
	if !employeeCredTypes[in.CredType] {
		// Jalur internal hanya NIP/NIK. Seragamkan dengan kegagalan kredensial (tak membocorkan).
		return nil, errInvalidCredential()
	}

	personID, err := uc.authenticate(ctx, in.CredType, in.CredValue, in.Password)
	if err != nil {
		return nil, err
	}

	opts, hasActiveEmployment, err := uc.resolver.resolve(ctx, personID)
	if err != nil {
		return nil, err
	}
	if !hasActiveEmployment {
		// Orang biasa (tanpa kepegawaian aktif) tak bisa masuk portal internal.
		return nil, core.ErrUnauthorized("tidak ada kepegawaian aktif")
	}
	if len(opts) == 0 {
		// Pegawai aktif tapi tak punya penugasan tenant aktif → tak ada tenant untuk dimasuki.
		return nil, core.ErrUnauthorized("tidak ada penugasan tenant aktif")
	}

	if len(opts) == 1 {
		token, err := uc.minter.mint(ctx, personID, opts[0])
		if err != nil {
			return nil, err
		}
		return &LoginResult{Token: token, Tenants: []TenantChoice{opts[0].choice()}}, nil
	}

	// >1 tenant: terbitkan token SEMENTARA (tanpa tenant & tanpa role) + daftar pilihan.
	temp, err := uc.issuer.Issue(ctx, port.Claims{PersonID: personID, Persona: domain.PersonaEmployee})
	if err != nil {
		return nil, err
	}
	choices := make([]TenantChoice, 0, len(opts))
	for _, o := range opts {
		choices = append(choices, o.choice())
	}
	return &LoginResult{Token: temp, NeedTenantSelection: true, Tenants: choices}, nil
}

// authenticate memverifikasi credential + password dan mengembalikan person yang aktif. Semua
// kegagalan dipetakan ke respons seragam (errInvalidCredential) agar tak membocorkan sebab.
func (uc *LoginEmployee) authenticate(ctx context.Context, t domain.CredType, value, password string) (uuid.UUID, error) {
	cred, err := uc.creds.FindByTypeValue(ctx, t, value)
	if err != nil {
		return uuid.Nil, errInvalidCredential()
	}
	if cred.SecretHash == "" {
		// Credential tanpa password (SSO/OTP-only) tak bisa login lewat jalur password.
		return uuid.Nil, errInvalidCredential()
	}
	if err := uc.passwords.Verify(cred.SecretHash, password); err != nil {
		return uuid.Nil, errInvalidCredential()
	}
	person, err := uc.persons.FindByID(ctx, cred.PersonID)
	if err != nil || !person.IsActive {
		return uuid.Nil, errInvalidCredential()
	}
	return cred.PersonID, nil
}

// SelectTenant menerbitkan token scoped final setelah person dengan banyak tenant memilih satu.
// Person sudah terotentikasi lewat token sementara (gateway memverifikasinya → AuthContext), jadi
// person_id diambil dari klaim tersigning, BUKAN input — mencegah pemilihan atas nama orang lain.
type SelectTenant struct {
	resolver employeeTenantResolver
	minter   scopedTokenMinter
}

// NewSelectTenant merakit use case pemilihan tenant.
func NewSelectTenant(
	employments domain.EmploymentRepository,
	assigns domain.TenantAssignmentRepository,
	tenants domain.TenantRegistry,
	central CentralRoleResolver,
	tenantRoles TenantRoleResolver,
	issuer port.TokenIssuer,
) *SelectTenant {
	return &SelectTenant{
		resolver: employeeTenantResolver{employments: employments, assigns: assigns, tenants: tenants, now: time.Now},
		minter:   scopedTokenMinter{central: central, tenantRoles: tenantRoles, issuer: issuer},
	}
}

// Execute memvalidasi bahwa tenantID berada di antara penugasan aktif person (token sementara),
// lalu menerbitkan token scoped final. Tenant yang tak berhak/non-aktif ditolak.
func (uc *SelectTenant) Execute(ctx port.AuthContext, tenantID string) (string, error) {
	if ctx.Persona() != domain.PersonaEmployee {
		return "", core.ErrUnauthorized("pemilihan tenant hanya untuk persona employee")
	}
	personID := ctx.PersonID()

	opts, _, err := uc.resolver.resolve(ctx, personID)
	if err != nil {
		return "", err
	}
	for _, o := range opts {
		if o.TenantID == tenantID {
			return uc.minter.mint(ctx, personID, o)
		}
	}
	// Tenant tak ada di daftar penugasan aktif person → tak berhak.
	return "", core.ErrUnauthorized("tenant tidak berada dalam penugasan aktif")
}
