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
	creds     domain.CredentialRepository
	persons   domain.PersonRepository
	passwords port.PasswordVerifier
	issuer    port.TokenIssuer
}

// NewLoginCitizen merakit alur login citizen. Tidak menerima resolver role apa pun — disengaja.
func NewLoginCitizen(
	creds domain.CredentialRepository,
	persons domain.PersonRepository,
	passwords port.PasswordVerifier,
	issuer port.TokenIssuer,
) *LoginCitizen {
	return &LoginCitizen{creds: creds, persons: persons, passwords: passwords, issuer: issuer}
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
// DEFERRED(Phase-2.4/PR-2.4.x): jalur OTP (no_hp/email tanpa password) + rate-limit & proteksi
// brute-force belum dibangun di sini — lihat REVIEW_BACKLOG A5. Saat ini verifikasi via password
// (secret_hash bcrypt); credential OTP-only (secret_hash kosong) ditolak.
func (uc *LoginCitizen) Execute(ctx context.Context, in LoginCitizenInput) (string, error) {
	if !citizenCredTypes[in.CredType] {
		return "", errInvalidCredential()
	}

	cred, err := uc.creds.FindByTypeValue(ctx, in.CredType, in.CredValue)
	if err != nil {
		return "", errInvalidCredential()
	}
	if cred.SecretHash == "" {
		return "", errInvalidCredential()
	}
	if err := uc.passwords.Verify(cred.SecretHash, in.Password); err != nil {
		return "", errInvalidCredential()
	}
	person, err := uc.persons.FindByID(ctx, cred.PersonID)
	if err != nil || !person.IsActive {
		return "", errInvalidCredential()
	}

	// Persona citizen: tanpa tenant, tanpa employment_status, tanpa role internal.
	return uc.issuer.Issue(ctx, port.Claims{
		PersonID: person.ID,
		Persona:  domain.PersonaCitizen,
	})
}
