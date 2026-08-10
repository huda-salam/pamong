package usecase_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/huda-salam/pamong/identity/adapter/auth"
	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/identity/usecase"
	"github.com/huda-salam/pamong/testkit"
)

// --- Fake credential store (PR-W2) ---

type storeCreds struct{ saved []*domain.Credential }

func (f *storeCreds) Save(_ context.Context, c *domain.Credential) error {
	// Meniru pintu tulis nyata: CredentialRepo.Save memvalidasi SEBELUM menyimpan, jadi fake
	// yang menerima apa saja akan membuat test lulus untuk nilai yang DB tolak.
	if err := c.Validate(); err != nil {
		return err
	}
	f.saved = append(f.saved, c)
	return nil
}
func (f *storeCreds) FindByTypeValue(context.Context, domain.CredType, string) (*domain.Credential, error) {
	return nil, nil
}
func (f *storeCreds) ListByPerson(context.Context, uuid.UUID) ([]*domain.Credential, error) {
	return nil, nil
}

// seedPersonUntukCred menyiapkan satu person yang sudah ada di repo.
func seedPersonUntukCred(t *testing.T) (*fakePersons, *domain.Person) {
	t.Helper()
	persons := newFakePersons()
	p := &domain.Person{ID: uuid.New(), NIK: "3578010101900021", NamaLengkap: "Sari", IsActive: true}
	if err := persons.Save(context.Background(), p); err != nil {
		t.Fatalf("seed person: %v", err)
	}
	return persons, p
}

func TestCreateCredential_Success(t *testing.T) {
	persons, person := seedPersonUntukCred(t)
	creds := &storeCreds{}
	verifier := auth.NewBcryptVerifier()
	uc := usecase.NewCreateCredential(persons, creds, verifier, usecase.NewVerifyGate(0, 0))
	ctx := testkit.Ctx(t, testkit.WithPermission(domain.PermCredentialBuat))

	const password = "kata-sandi-panjang"
	c, err := uc.Execute(ctx, usecase.CreateCredentialInput{
		PersonID: person.ID, CredType: domain.CredNIP,
		CredValue: "199001012015011021", Password: password, IsPrimary: true,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(creds.saved) != 1 {
		t.Fatalf("credential harus tersimpan, dapat %d", len(creds.saved))
	}
	// Hash, bukan plaintext — dan hash yang benar-benar bisa diverifikasi jalur login.
	if c.SecretHash == "" || strings.Contains(c.SecretHash, password) {
		t.Fatalf("secret_hash bukan hash: %q", c.SecretHash)
	}
	if err := verifier.Verify(c.SecretHash, password); err != nil {
		t.Fatalf("hash tak bisa diverifikasi jalur login: %v", err)
	}
	// Hash yang dipakai HARUS berasal dari port yang sama dengan sisi verifikasi (bukan
	// konstanta/kripto lokal): password lain tak boleh cocok.
	if err := verifier.Verify(c.SecretHash, "kata-sandi-lainnya"); err == nil {
		t.Fatal("password berbeda tak boleh cocok dengan hash yang sama")
	}
}

func TestCreateCredential_PermissionDenied(t *testing.T) {
	persons, person := seedPersonUntukCred(t)
	uc := usecase.NewCreateCredential(persons, &storeCreds{}, auth.NewBcryptVerifier(), usecase.NewVerifyGate(0, 0))
	ctx := testkit.Ctx(t) // tanpa permission

	_, err := uc.Execute(ctx, usecase.CreateCredentialInput{
		PersonID: person.ID, CredType: domain.CredNIP,
		CredValue: "199001012015011021", Password: "kata-sandi-panjang",
	})
	if !testkit.IsPermissionDenied(err) {
		t.Fatalf("harus PERMISSION_DENIED, dapat: %v", err)
	}
}

// Permission person:buat TIDAK memberi hak membuat kredensial: mencatat seseorang ada berbeda
// dari memberinya cara masuk. Test ini yang menahan keduanya tetap terpisah.
func TestCreateCredential_PermissionPersonTakCukup(t *testing.T) {
	persons, person := seedPersonUntukCred(t)
	uc := usecase.NewCreateCredential(persons, &storeCreds{}, auth.NewBcryptVerifier(), usecase.NewVerifyGate(0, 0))
	ctx := testkit.Ctx(t, testkit.WithPermission(domain.PermPersonBuat))

	_, err := uc.Execute(ctx, usecase.CreateCredentialInput{
		PersonID: person.ID, CredType: domain.CredNIP,
		CredValue: "199001012015011021", Password: "kata-sandi-panjang",
	})
	if !testkit.IsPermissionDenied(err) {
		t.Fatalf("harus PERMISSION_DENIED, dapat: %v", err)
	}
}

func TestCreateCredential_PersonTakAda(t *testing.T) {
	uc := usecase.NewCreateCredential(newFakePersons(), &storeCreds{}, auth.NewBcryptVerifier(),
		usecase.NewVerifyGate(0, 0))
	ctx := testkit.Ctx(t, testkit.WithPermission(domain.PermCredentialBuat))

	_, err := uc.Execute(ctx, usecase.CreateCredentialInput{
		PersonID: uuid.New(), CredType: domain.CredNIP,
		CredValue: "199001012015011021", Password: "kata-sandi-panjang",
	})
	if err == nil {
		t.Fatal("kredensial untuk person yang tak ada harus ditolak")
	}
}

// Field wajib: cred_value kosong & cred_type tak dikenal ditolak domain, bukan diserahkan ke DB.
func TestCreateCredential_ValidasiFieldWajib(t *testing.T) {
	kasus := []struct {
		nama string
		in   usecase.CreateCredentialInput
		mau  error
	}{
		{"cred_value kosong", usecase.CreateCredentialInput{CredType: domain.CredNIP}, domain.ErrCredValueKosong},
		{"cred_type tak dikenal", usecase.CreateCredentialInput{CredType: "sidik_jari", CredValue: "x"}, domain.ErrCredTypeInvalid},
		{
			"cred_value ber-spasi tepi",
			usecase.CreateCredentialInput{CredType: domain.CredEmail, CredValue: " sari@example.test"},
			domain.ErrCredValueFormat,
		},
	}
	for _, k := range kasus {
		t.Run(k.nama, func(t *testing.T) {
			persons, person := seedPersonUntukCred(t)
			uc := usecase.NewCreateCredential(persons, &storeCreds{}, auth.NewBcryptVerifier(), usecase.NewVerifyGate(0, 0))
			ctx := testkit.Ctx(t, testkit.WithPermission(domain.PermCredentialBuat))

			in := k.in
			in.PersonID = person.ID
			if _, err := uc.Execute(ctx, in); !errors.Is(err, k.mau) {
				t.Fatalf("mau %v, dapat %v", k.mau, err)
			}
		})
	}
}

// Batas panjang password adalah VALIDASI (400), bukan kegagalan internal. Batas atas ada karena
// bcrypt memotong di 72 byte: dua password dengan 72 byte awal sama akan dianggap cocok.
func TestCreateCredential_BatasPanjangPassword(t *testing.T) {
	kasus := []struct {
		nama     string
		password string
		mau      error
	}{
		{"terlalu pendek", "pendek", domain.ErrPasswordTerlaluPendek},
		{"melewati batas bcrypt", strings.Repeat("a", 73), domain.ErrPasswordTerlaluPanjang},
	}
	for _, k := range kasus {
		t.Run(k.nama, func(t *testing.T) {
			persons, person := seedPersonUntukCred(t)
			creds := &storeCreds{}
			uc := usecase.NewCreateCredential(persons, creds, auth.NewBcryptVerifier(), usecase.NewVerifyGate(0, 0))
			ctx := testkit.Ctx(t, testkit.WithPermission(domain.PermCredentialBuat))

			_, err := uc.Execute(ctx, usecase.CreateCredentialInput{
				PersonID: person.ID, CredType: domain.CredNIP,
				CredValue: "199001012015011021", Password: k.password,
			})
			if !errors.Is(err, k.mau) {
				t.Fatalf("mau %v, dapat %v", k.mau, err)
			}
			if len(creds.saved) != 0 {
				t.Fatal("kredensial tak boleh tersimpan saat password ditolak")
			}
		})
	}
}

// Minimum dihitung per RUNE, bukan per byte: 12 byte bisa dicapai 3 emoji, dan janji ke
// pengguna berbunyi "12 karakter".
func TestCreateCredential_MinimumDihitungPerRune(t *testing.T) {
	persons, person := seedPersonUntukCred(t)
	uc := usecase.NewCreateCredential(persons, &storeCreds{}, auth.NewBcryptVerifier(), usecase.NewVerifyGate(0, 0))
	ctx := testkit.Ctx(t, testkit.WithPermission(domain.PermCredentialBuat))

	_, err := uc.Execute(ctx, usecase.CreateCredentialInput{
		PersonID: person.ID, CredType: domain.CredNIP,
		CredValue: "199001012015011021",
		Password:  strings.Repeat("é", 11), // 22 byte, tapi hanya 11 karakter
	})
	if !errors.Is(err, domain.ErrPasswordTerlaluPendek) {
		t.Fatalf("11 rune (22 byte) harus ditolak sebagai terlalu pendek, dapat: %v", err)
	}
}

// Password kosong = kredensial OTP-only (email/no_hp, ADR-008): sah, dan secret_hash-nya harus
// KOSONG — bukan hash dari string kosong, yang akan menjadikan "" password yang sah.
func TestCreateCredential_TanpaPassword_SecretHashKosong(t *testing.T) {
	persons, person := seedPersonUntukCred(t)
	creds := &storeCreds{}
	uc := usecase.NewCreateCredential(persons, creds, auth.NewBcryptVerifier(), usecase.NewVerifyGate(0, 0))
	ctx := testkit.Ctx(t, testkit.WithPermission(domain.PermCredentialBuat))

	c, err := uc.Execute(ctx, usecase.CreateCredentialInput{
		PersonID: person.ID, CredType: domain.CredEmail, CredValue: "sari@example.test",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if c.SecretHash != "" {
		t.Fatalf("kredensial tanpa password harus ber-secret_hash kosong, dapat %q", c.SecretHash)
	}
	if err := auth.NewBcryptVerifier().Verify(c.SecretHash, ""); err == nil {
		t.Fatal(`string kosong tak boleh menjadi password yang sah untuk kredensial OTP-only`)
	}
}

// Kedua kontrol WAJIB ditegakkan saat PERAKITAN, bukan saat request pertama: verifier nil
// menghasilkan kredensial tanpa secret, gate nil melepaskan hashing dari batas concurrency
// bcrypt yang dijaga seluruh proses. Keduanya tak bergejala sampai terlambat.
func TestNewCreateCredential_MenolakDependensiNil(t *testing.T) {
	kasus := map[string]func(){
		"verifier nil": func() {
			usecase.NewCreateCredential(newFakePersons(), &storeCreds{}, nil, usecase.NewVerifyGate(0, 0))
		},
		"gate nil": func() {
			usecase.NewCreateCredential(newFakePersons(), &storeCreds{}, auth.NewBcryptVerifier(), nil)
		},
	}
	for nama, rakit := range kasus {
		t.Run(nama, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("perakitan dengan dependensi nil harus panic")
				}
			}()
			rakit()
		})
	}
}
