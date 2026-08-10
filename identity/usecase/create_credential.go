package usecase

import (
	"context"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/port"
)

// Batas panjang password pada jalur pembuatan kredensial. Batas atas = batas bcrypt (72 byte):
// di atas itu bcrypt memotong diam-diam, sehingga dua password berbeda dengan 72 byte awal
// yang sama akan dianggap cocok. Batas bawah adalah kebijakan — dan tempatnya memang di sini,
// karena INI satu-satunya jalur tulis password di seluruh sistem.
const (
	minPasswordLen = 12 // rune: "12 karakter" bagi pengguna, bukan 12 byte yang bisa dicapai 3 emoji
	maxPasswordLen = 72 // byte: yang dipotong bcrypt adalah byte, bukan rune
)

// CreateCredential membuat kredensial login untuk person yang sudah ada — satu-satunya jalur
// tulis password di sistem, dan pasangan tulis dari `passwordAuthenticator` yang membacanya.
//
// Ia dibutuhkan untuk seed admin pertama (ROADMAP PR-W2): tanpa penulis kredensial, satu-satunya
// cara memberi seseorang cara masuk adalah menyisipkan baris + hash bcrypt langsung ke DB, yaitu
// persis jalur tanpa permission, tanpa audit, dan tanpa validasi bentuk pengenal.
//
// Hash TIDAK dihitung di sini melainkan diminta ke port.PasswordVerifier — cermin dari sisi
// verifikasi. Cost bcrypt karena itu punya satu sumber; menuliskannya lagi di sisi tulis akan
// membuat kredensial baru tertinggal cost-nya begitu sisi verifikasi dinaikkan.
type CreateCredential struct {
	persons   domain.PersonRepository
	creds     domain.CredentialRepository
	passwords port.PasswordVerifier
	gate      *VerifyGate
}

// NewCreateCredential merakit use case. passwords & gate WAJIB non-nil, dan keduanya ditegakkan
// di sini karena keduanya kontrol yang menunggu pemanggil ingat memasangnya bukan kontrol:
// tanpa passwords satu-satunya jalur tersisa adalah menyimpan kredensial tanpa secret (tampak
// berdiri, tak pernah bisa dipakai login); tanpa gate, hashing di sini lolos dari batas
// concurrency bcrypt yang dijaga seluruh proses.
//
// gate SENGAJA gerbang yang SAMA dengan yang dipakai LoginEmployee/LoginCitizen — satu instance
// dirakit di composition root lalu diteruskan ke ketiganya.
func NewCreateCredential(
	persons domain.PersonRepository,
	creds domain.CredentialRepository,
	passwords port.PasswordVerifier,
	gate *VerifyGate,
) *CreateCredential {
	switch {
	case passwords == nil:
		panic("identity/usecase: CreateCredential butuh port.PasswordVerifier")
	case gate == nil:
		panic("identity/usecase: CreateCredential butuh *VerifyGate (batas concurrency bcrypt)")
	}
	return &CreateCredential{persons: persons, creds: creds, passwords: passwords, gate: gate}
}

// CreateCredentialInput DTO masuk. Password KOSONG sah dan berarti kredensial tanpa secret:
// jalur email/no_hp yang masuk lewat OTP (ADR-008) memang tak punya password, dan
// passwordAuthenticator sudah memperlakukan secret_hash kosong sebagai kegagalan seragam.
type CreateCredentialInput struct {
	PersonID  uuid.UUID
	CredType  domain.CredType
	CredValue string
	Password  string
	IsPrimary bool
}

// Execute: permission -> pastikan person ada -> validasi password -> hash -> persist.
//
// Tidak ada event yang diterbitkan, dan itu disengaja: kredensial tak pernah ikut ke clone
// tenant (gov.user_profiles sengaja tanpa kolom kredensial), jadi event-nya tak punya
// consumer — hanya menambah satu payload berisi koordinat kredensial ke stream ber-retensi
// (ADR-018). Jejak "siapa membuat kredensial ini" ditulis lapis audit, bukan lapis event.
func (uc *CreateCredential) Execute(ctx port.AuthContext, in CreateCredentialInput) (*domain.Credential, error) {
	if err := ctx.RequirePermission(domain.PermCredentialBuat); err != nil {
		return nil, err
	}

	// Person harus ada — kredensial menggantung pada person nyata (FK + kejelasan error),
	// sama seperti AttachEmployment.
	if _, err := uc.persons.FindByID(ctx, in.PersonID); err != nil {
		return nil, err
	}

	c := &domain.Credential{
		ID:        uuid.New(),
		PersonID:  in.PersonID,
		CredType:  in.CredType,
		CredValue: in.CredValue,
		IsPrimary: in.IsPrimary,
	}
	// Validate mendahului hashing, dan urutannya bukan selera: bcrypt adalah operasi termahal di
	// jalur ini (~60-100 ms CPU per panggilan), jadi masukan yang jelas-jelas ditolak aturan tak
	// boleh membelinya lebih dulu. Ia dipanggil lagi di repo (pintu tulis) — di situlah
	// penegakan yang sebenarnya; yang di sini hemat biaya.
	if err := c.Validate(); err != nil {
		return nil, err
	}

	hash, err := uc.hashPassword(ctx, in.Password)
	if err != nil {
		return nil, err
	}
	c.SecretHash = hash

	if err := uc.creds.Save(ctx, c); err != nil {
		return nil, err
	}
	return c, nil
}

// hashPassword memvalidasi batas panjang lalu mendelegasikan hashing ke port. Password kosong
// menghasilkan hash kosong (kredensial OTP-only), bukan hash dari string kosong — yang terakhir
// akan membuat "" menjadi password yang sah.
//
// Hashing berjalan di balik VerifyGate yang SAMA dengan jalur login. bcrypt terikat CPU, dan
// gerbang terpisah per permukaan melipatgandakan batas yang justru ingin ditegakkan (lihat
// NewVerifyGate). Rute ini memang menuntut token + permission, tapi rate limit gateway
// per-principal bawaan ada di orde ratusan rps: satu admin sah yang membanjirinya cukup untuk
// menjenuhkan seluruh core proses dan menjatuhkan jalur login bersamanya.
func (uc *CreateCredential) hashPassword(ctx context.Context, plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	// Dua satuan yang berbeda, dan itu disengaja — masing-masing mengukur hal yang berbeda:
	// batas bawah adalah janji kepada pengguna ("12 karakter"), batas atas adalah batas mesin.
	switch {
	case utf8.RuneCountInString(plain) < minPasswordLen:
		return "", domain.ErrPasswordTerlaluPendek
	case len(plain) > maxPasswordLen:
		return "", domain.ErrPasswordTerlaluPanjang
	}
	release, err := uc.gate.acquire(ctx)
	if err != nil {
		return "", err
	}
	defer release()
	return uc.passwords.Hash(plain)
}
