package usecase

import (
	"context"

	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/port"
)

// citizenCredTypes membatasi jalur publik ke NIK / email / no_hp (CLAUDE.md "Credential & jalur
// login"). NIP (jalur internal) sengaja TIDAK termasuk: portal publik tak boleh dipakai sebagai
// pintu internal.
var citizenCredTypes = map[domain.CredType]bool{
	domain.CredNIK:   true,
	domain.CredEmail: true,
	domain.CredNoHP:  true,
}

// LoginCitizen adalah alur login persona citizen (portal publik, untuk SIAPA PUN termasuk ASN):
// resolve credential (NIK/email/no_hp) + verifikasi password → token persona=citizen.
//
// Berbeda tegas dari LoginEmployee: TIDAK mengecek employment dan TIDAK PERNAH memanggil resolver
// role (central/tenant). Ini struktural — token citizen mustahil membawa role internal, sehingga
// ASN yang login publik diperlakukan murni sebagai warga (cegah kebocoran wewenang internal).
type LoginCitizen struct {
	auth   passwordAuthenticator
	issuer port.TokenIssuer
}

// NewLoginCitizen merakit alur login citizen. Tidak menerima resolver role apa pun — disengaja.
// limiter+policy: proteksi brute-force yang sama persis dengan jalur employee (PR-W1); portal
// publik justru yang paling terekspos, jadi ia tak boleh dapat perlakuan lebih longgar.
func NewLoginCitizen(
	creds domain.CredentialRepository,
	persons domain.PersonRepository,
	passwords port.PasswordVerifier,
	issuer port.TokenIssuer,
	limiter port.RateLimiter,
	policy LoginPolicy,
) *LoginCitizen {
	return &LoginCitizen{
		auth: passwordAuthenticator{
			creds: creds, persons: persons, passwords: passwords, limiter: limiter, policy: policy,
		},
		issuer: issuer,
	}
}

// LoginCitizenInput DTO masuk dari portal publik.
type LoginCitizenInput struct {
	CredType  domain.CredType // nik | email | no_hp
	CredValue string
	Password  string
}

// Execute memverifikasi credential publik lalu menerbitkan token persona=citizen tanpa tenant
// dan tanpa role.
//
// Jalur OTP (no_hp/email tanpa password) hidup terpisah di RequestOTP/VerifyOTP (PR-2.4.4);
// di sini verifikasi via password (secret_hash bcrypt) dan credential OTP-only (secret_hash
// kosong) ditolak. Proteksi brute-force ada di passwordAuthenticator (PR-W1, REVIEW_BACKLOG A5).
func (uc *LoginCitizen) Execute(ctx context.Context, in LoginCitizenInput) (string, error) {
	if !citizenCredTypes[in.CredType] {
		return "", errInvalidCredential()
	}

	person, err := uc.auth.authenticate(ctx, in.CredType, in.CredValue, in.Password)
	if err != nil {
		return "", err
	}

	// Persona citizen: tanpa tenant, tanpa employment_status, tanpa role internal.
	return uc.issuer.Issue(ctx, port.Claims{
		PersonID: person.ID,
		Persona:  domain.PersonaCitizen,
	})
}
