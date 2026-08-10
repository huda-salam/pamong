package usecase_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/huda-salam/pamong/identity/adapter/auth"
	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/identity/usecase"
	"github.com/huda-salam/pamong/port"
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

// newCredUC merakit CreateCredential dengan repo containment KOSONG: target yang tak memegang
// role sentral apa pun berada dalam wewenang siapa saja yang punya identity:credential:buat,
// jadi test-test di bawah menguji sisi lain (hash, validasi, batas panjang) tanpa terganggu
// gerbang ADR-019. Test containment mengisi repo-nya sendiri.
func newCredUC(
	persons domain.PersonRepository, creds domain.CredentialRepository,
	pw port.PasswordVerifier, gate *usecase.VerifyGate,
) *usecase.CreateCredential {
	return usecase.NewCreateCredential(persons, creds, pw, gate,
		newFakeCentralRoles(), &fakeCentralAssignments{})
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
	uc := newCredUC(persons, creds, verifier, usecase.NewVerifyGate(0, 0))
	ctx := testkit.Ctx(t, testkit.WithTenant("pemkot-surabaya"),
		testkit.WithPermission(domain.PermCredentialBuat))

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
	uc := newCredUC(persons, &storeCreds{}, auth.NewBcryptVerifier(), usecase.NewVerifyGate(0, 0))
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
	uc := newCredUC(persons, &storeCreds{}, auth.NewBcryptVerifier(), usecase.NewVerifyGate(0, 0))
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
	uc := newCredUC(newFakePersons(), &storeCreds{}, auth.NewBcryptVerifier(),
		usecase.NewVerifyGate(0, 0))
	ctx := testkit.Ctx(t, testkit.WithTenant("pemkot-surabaya"),
		testkit.WithPermission(domain.PermCredentialBuat))

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
			uc := newCredUC(persons, &storeCreds{}, auth.NewBcryptVerifier(), usecase.NewVerifyGate(0, 0))
			ctx := testkit.Ctx(t, testkit.WithTenant("pemkot-surabaya"),
				testkit.WithPermission(domain.PermCredentialBuat))

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
			uc := newCredUC(persons, creds, auth.NewBcryptVerifier(), usecase.NewVerifyGate(0, 0))
			ctx := testkit.Ctx(t, testkit.WithTenant("pemkot-surabaya"),
				testkit.WithPermission(domain.PermCredentialBuat))

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
	uc := newCredUC(persons, &storeCreds{}, auth.NewBcryptVerifier(), usecase.NewVerifyGate(0, 0))
	ctx := testkit.Ctx(t, testkit.WithTenant("pemkot-surabaya"),
		testkit.WithPermission(domain.PermCredentialBuat))

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
	uc := newCredUC(persons, creds, auth.NewBcryptVerifier(), usecase.NewVerifyGate(0, 0))
	ctx := testkit.Ctx(t, testkit.WithTenant("pemkot-surabaya"),
		testkit.WithPermission(domain.PermCredentialBuat))

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

// Keempat kontrol WAJIB ditegakkan saat PERAKITAN, bukan saat request pertama: verifier nil
// menghasilkan kredensial tanpa secret, gate nil melepaskan hashing dari batas concurrency
// bcrypt yang dijaga seluruh proses, dan repo containment nil membuat gerbang ADR-019 tak punya
// cara mengetahui wewenang target. Semuanya tak bergejala sampai terlambat.
func TestNewCreateCredential_MenolakDependensiNil(t *testing.T) {
	kasus := map[string]func(){
		"verifier nil": func() {
			usecase.NewCreateCredential(newFakePersons(), &storeCreds{}, nil,
				usecase.NewVerifyGate(0, 0), newFakeCentralRoles(), &fakeCentralAssignments{})
		},
		"gate nil": func() {
			usecase.NewCreateCredential(newFakePersons(), &storeCreds{}, auth.NewBcryptVerifier(),
				nil, newFakeCentralRoles(), &fakeCentralAssignments{})
		},
		"repo role sentral nil": func() {
			usecase.NewCreateCredential(newFakePersons(), &storeCreds{}, auth.NewBcryptVerifier(),
				usecase.NewVerifyGate(0, 0), nil, &fakeCentralAssignments{})
		},
		"repo assignment nil": func() {
			usecase.NewCreateCredential(newFakePersons(), &storeCreds{}, auth.NewBcryptVerifier(),
				usecase.NewVerifyGate(0, 0), newFakeCentralRoles(), nil)
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

// --- Containment aktor→target (ADR-019 / REVIEW_BACKLOG B7 butir a — varian TERKUAT) ---

// seedTargetBerRoleSentral menyiapkan target yang MEMEGANG satu role sentral aktif, beserta
// repo containment yang akan dibaca use case.
func seedTargetBerRoleSentral(
	t *testing.T, scope domain.ScopeType, tenantScope []string, perms ...string,
) (*fakeCentralRoles, *fakeCentralAssignments, uuid.UUID) {
	t.Helper()
	roles := newFakeCentralRoles()
	role := &domain.CentralRole{
		ID: uuid.New(), Name: "platform_admin", Label: "Admin Platform",
		ScopeType: scope, Permissions: perms,
	}
	if err := roles.Save(context.Background(), role); err != nil {
		t.Fatalf("seed role: %v", err)
	}
	targetID := uuid.New()
	assigns := &fakeCentralAssignments{}
	if err := assigns.Save(context.Background(), &domain.CentralRoleAssignment{
		ID: uuid.New(), PersonID: targetID, RoleID: role.ID, TenantScope: tenantScope,
		AssignedBy: uuid.New(), ValidFrom: time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatalf("seed assignment: %v", err)
	}
	return roles, assigns, targetID
}

// TestCreateCredential_TargetDiLuarWewenangDitolak adalah skenario B7(a) utuh: aktor ber-scope
// tenant A memegang identity:credential:buat dan tahu id person admin platform (ia ter-clone ke
// gov.user_profiles tenantnya sendiri). Sebelum ADR-019, menerbitkan kredensial untuk id itu
// berhasil — dan karena login me-resolve murni lewat (cred_type, cred_value) → person_id, ia
// lalu login SEBAGAI admin platform.
func TestCreateCredential_TargetDiLuarWewenangDitolak(t *testing.T) {
	persons := newFakePersons()
	roles, assigns, targetID := seedTargetBerRoleSentral(
		t, domain.ScopeGlobal, nil, domain.PermCredentialBuat, domain.PermCentralRoleAssign)
	target := &domain.Person{
		ID: targetID, NIK: "3578010101900099", NamaLengkap: "Admin Platform", IsActive: true,
	}
	if err := persons.Save(context.Background(), target); err != nil {
		t.Fatalf("seed target: %v", err)
	}
	creds := &storeCreds{}
	uc := usecase.NewCreateCredential(
		persons, creds, auth.NewBcryptVerifier(), usecase.NewVerifyGate(0, 0), roles, assigns)
	ctx := testkit.Ctx(t, testkit.WithTenant("pemkot-surabaya"),
		testkit.WithPermission(domain.PermCredentialBuat))

	_, err := uc.Execute(ctx, usecase.CreateCredentialInput{
		PersonID: targetID, CredType: domain.CredNIP,
		CredValue: "199001012015011099", Password: "kata-sandi-panjang",
	})
	if !testkit.IsPermissionDenied(err) {
		t.Fatalf("kredensial untuk target di luar wewenang harus ditolak, dapat: %v", err)
	}
	if len(creds.saved) != 0 {
		t.Fatal("penolakan wewenang tidak boleh menyisakan kredensial tersimpan")
	}
}

// TestCreateCredential_TargetDalamWewenangDiizinkan: target yang role sentralnya scoped ke
// tenant aktor DAN permission-nya dipegang aktor tetap boleh diberi kredensial — gerbang ini
// membatasi eskalasi, bukan mematikan administrasi identitas sehari-hari.
func TestCreateCredential_TargetDalamWewenangDiizinkan(t *testing.T) {
	persons := newFakePersons()
	roles, assigns, targetID := seedTargetBerRoleSentral(
		t, domain.ScopeScoped, []string{"pemkot-surabaya"}, domain.PermTenantBaca)
	target := &domain.Person{
		ID: targetID, NIK: "3578010101900098", NamaLengkap: "Operator", IsActive: true,
	}
	if err := persons.Save(context.Background(), target); err != nil {
		t.Fatalf("seed target: %v", err)
	}
	creds := &storeCreds{}
	uc := usecase.NewCreateCredential(
		persons, creds, auth.NewBcryptVerifier(), usecase.NewVerifyGate(0, 0), roles, assigns)
	ctx := testkit.Ctx(t, testkit.WithTenant("pemkot-surabaya"),
		testkit.WithPermission(domain.PermCredentialBuat),
		testkit.WithPermission(domain.PermTenantBaca))

	if _, err := uc.Execute(ctx, usecase.CreateCredentialInput{
		PersonID: targetID, CredType: domain.CredNIP,
		CredValue: "199001012015011098", Password: "kata-sandi-panjang",
	}); err != nil {
		t.Fatalf("target dalam wewenang harus diizinkan, dapat: %v", err)
	}
	if len(creds.saved) != 1 {
		t.Fatalf("kredensial harus tersimpan, dapat %d", len(creds.saved))
	}
}

// TestCreateCredential_TargetKedaluwarsaTakMenghalangi: assignment role sentral yang sudah
// lewat masa berlakunya tidak memberi wewenang apa pun kepada target, jadi ia tak boleh
// membekukan penerbitan kredensial untuk orang itu selamanya.
func TestCreateCredential_TargetKedaluwarsaTakMenghalangi(t *testing.T) {
	persons := newFakePersons()
	roles := newFakeCentralRoles()
	role := &domain.CentralRole{
		ID: uuid.New(), Name: "platform_admin", Label: "Admin", ScopeType: domain.ScopeGlobal,
		Permissions: []string{domain.PermCentralRoleAssign},
	}
	_ = roles.Save(context.Background(), role)
	targetID := uuid.New()
	expired := time.Now().Add(-time.Hour)
	assigns := &fakeCentralAssignments{}
	_ = assigns.Save(context.Background(), &domain.CentralRoleAssignment{
		ID: uuid.New(), PersonID: targetID, RoleID: role.ID, AssignedBy: uuid.New(),
		ValidFrom: time.Now().Add(-48 * time.Hour), ValidUntil: &expired,
	})
	if err := persons.Save(context.Background(), &domain.Person{
		ID: targetID, NIK: "3578010101900097", NamaLengkap: "Mantan Admin", IsActive: true,
	}); err != nil {
		t.Fatalf("seed target: %v", err)
	}
	uc := usecase.NewCreateCredential(persons, &storeCreds{}, auth.NewBcryptVerifier(),
		usecase.NewVerifyGate(0, 0), roles, assigns)
	ctx := testkit.Ctx(t, testkit.WithTenant("pemkot-surabaya"),
		testkit.WithPermission(domain.PermCredentialBuat))

	if _, err := uc.Execute(ctx, usecase.CreateCredentialInput{
		PersonID: targetID, CredType: domain.CredNIP,
		CredValue: "199001012015011097", Password: "kata-sandi-panjang",
	}); err != nil {
		t.Fatalf("assignment kedaluwarsa tak boleh memblokir, dapat: %v", err)
	}
}

// TestCreateCredential_EskalasiDenganPintuKeluarDiizinkan: admin platform (pemegang escalate)
// tetap bisa menerbitkan kredensial untuk sesama admin platform — jalur pemulihan yang WAJIB
// tetap ada, dan yang sekarang terlihat di audit sebagai pemakaian escalate.
func TestCreateCredential_EskalasiDenganPintuKeluarDiizinkan(t *testing.T) {
	persons := newFakePersons()
	roles, assigns, targetID := seedTargetBerRoleSentral(
		t, domain.ScopeGlobal, nil, domain.PermCredentialBuat)
	if err := persons.Save(context.Background(), &domain.Person{
		ID: targetID, NIK: "3578010101900096", NamaLengkap: "Admin Lain", IsActive: true,
	}); err != nil {
		t.Fatalf("seed target: %v", err)
	}
	creds := &storeCreds{}
	uc := usecase.NewCreateCredential(persons, creds, auth.NewBcryptVerifier(),
		usecase.NewVerifyGate(0, 0), roles, assigns)
	ctx := testkit.Ctx(t, testkit.WithTenant("pemkot-surabaya"),
		testkit.WithPermission(domain.PermCredentialBuat),
		testkit.WithPermission(domain.PermAuthorityEscalate))

	if _, err := uc.Execute(ctx, usecase.CreateCredentialInput{
		PersonID: targetID, CredType: domain.CredNIP,
		CredValue: "199001012015011096", Password: "kata-sandi-panjang",
	}); err != nil {
		t.Fatalf("pemegang escalate harus boleh, dapat: %v", err)
	}
	if len(creds.saved) != 1 {
		t.Fatalf("kredensial harus tersimpan, dapat %d", len(creds.saved))
	}
}

// TestCreateCredential_TargetBerRoleScopedTenantLainDitolak menutup lubang yang paling mudah
// terlewat pada aturan 3: kredensial TIDAK terikat tenant. Aktor di tenant X yang menerbitkan
// kredensial bagi orang ber-role scoped di tenant Y bisa login sebagai orang itu lalu memilih
// tenant Y (POST /auth/select-tenant) dan memakai wewenangnya di sana. Karena itu penyaringan
// assignment target sengaja tenant-agnostik (ActiveAt, bukan AppliesTo) — versi yang menyaring
// dengan tenant aktor akan menganggap role di tenant lain tak ada.
func TestCreateCredential_TargetBerRoleScopedTenantLainDitolak(t *testing.T) {
	persons := newFakePersons()
	roles, assigns, targetID := seedTargetBerRoleSentral(
		t, domain.ScopeScoped, []string{"pemkot-malang"}, domain.PermTenantNonaktif)
	if err := persons.Save(context.Background(), &domain.Person{
		ID: targetID, NIK: "3578010101900095", NamaLengkap: "Helpdesk Malang", IsActive: true,
	}); err != nil {
		t.Fatalf("seed target: %v", err)
	}
	creds := &storeCreds{}
	uc := usecase.NewCreateCredential(persons, creds, auth.NewBcryptVerifier(),
		usecase.NewVerifyGate(0, 0), roles, assigns)
	// Aktor bahkan MEMEGANG permission yang diberikan role target — yang kurang hanyalah
	// jangkauan tenantnya. Itu cukup untuk menolak.
	ctx := testkit.Ctx(t, testkit.WithTenant("pemkot-surabaya"),
		testkit.WithPermission(domain.PermCredentialBuat),
		testkit.WithPermission(domain.PermTenantNonaktif))

	_, err := uc.Execute(ctx, usecase.CreateCredentialInput{
		PersonID: targetID, CredType: domain.CredNIP,
		CredValue: "199001012015011095", Password: "kata-sandi-panjang",
	})
	if !testkit.IsPermissionDenied(err) {
		t.Fatalf("kredensial untuk target ber-role scoped di tenant LAIN harus ditolak, dapat: %v", err)
	}
	if len(creds.saved) != 0 {
		t.Fatal("penolakan wewenang tidak boleh menyisakan kredensial tersimpan")
	}
}

// TestCreateCredential_TargetBerRoleTerjadwalDitolak menutup asimetri waktu pada aturan 3:
// kredensial berumur PERMANEN, sedangkan "aktif sekarang" hanya memotret satu titik. valid_from
// datang dari klien (POST /admin/identity/central-role-assignments), jadi assignment global yang
// dijadwalkan mulai pekan depan tampak tak aktif hari ini — aktor menerbitkan kredensialnya
// sekarang, lalu memanen wewenangnya pekan depan. Hanya ValidUntil yang sudah lewat yang aman
// diabaikan (lihat TestCreateCredential_TargetKedaluwarsaTakMenghalangi).
func TestCreateCredential_TargetBerRoleTerjadwalDitolak(t *testing.T) {
	persons := newFakePersons()
	roles := newFakeCentralRoles()
	role := &domain.CentralRole{
		ID: uuid.New(), Name: "platform_admin", Label: "Admin Platform",
		ScopeType: domain.ScopeGlobal, Permissions: []string{domain.PermCentralRoleAssign},
	}
	_ = roles.Save(context.Background(), role)
	targetID := uuid.New()
	assigns := &fakeCentralAssignments{}
	_ = assigns.Save(context.Background(), &domain.CentralRoleAssignment{
		ID: uuid.New(), PersonID: targetID, RoleID: role.ID, AssignedBy: uuid.New(),
		ValidFrom: time.Now().Add(7 * 24 * time.Hour), // baru berlaku pekan depan
	})
	if err := persons.Save(context.Background(), &domain.Person{
		ID: targetID, NIK: "3578010101900094", NamaLengkap: "Calon Admin", IsActive: true,
	}); err != nil {
		t.Fatalf("seed target: %v", err)
	}
	creds := &storeCreds{}
	uc := usecase.NewCreateCredential(persons, creds, auth.NewBcryptVerifier(),
		usecase.NewVerifyGate(0, 0), roles, assigns)
	ctx := testkit.Ctx(t, testkit.WithTenant("pemkot-surabaya"),
		testkit.WithPermission(domain.PermCredentialBuat))

	_, err := uc.Execute(ctx, usecase.CreateCredentialInput{
		PersonID: targetID, CredType: domain.CredNIP,
		CredValue: "199001012015011094", Password: "kata-sandi-panjang",
	})
	if !testkit.IsPermissionDenied(err) {
		t.Fatalf("kredensial untuk target ber-role TERJADWAL harus ditolak, dapat: %v", err)
	}
	if len(creds.saved) != 0 {
		t.Fatal("penolakan wewenang tidak boleh menyisakan kredensial tersimpan")
	}
}

// TestCreateCredential_AktorTanpaTenantDitolak mengunci arah gagal gerbang saat konteks tak
// terikat tenant (anonim, atau job/CLI yang belum merakit stack auth). Konteks semacam itu tak
// punya evaluator, dan RequirePermission default PERMISIF — tanpa requireTenantBound, pintu
// keluar escalate akan terbaca "boleh" dan seluruh containment lolos diam-diam.
func TestCreateCredential_AktorTanpaTenantDitolak(t *testing.T) {
	persons := newFakePersons()
	roles, assigns, targetID := seedTargetBerRoleSentral(
		t, domain.ScopeScoped, []string{"pemkot-surabaya"}, domain.PermTenantBaca)
	if err := persons.Save(context.Background(), &domain.Person{
		ID: targetID, NIK: "3578010101900093", NamaLengkap: "Operator", IsActive: true,
	}); err != nil {
		t.Fatalf("seed target: %v", err)
	}
	creds := &storeCreds{}
	uc := usecase.NewCreateCredential(persons, creds, auth.NewBcryptVerifier(),
		usecase.NewVerifyGate(0, 0), roles, assigns)
	// Semua permission diberikan — TERMASUK pintu keluar — tapi konteks tak ber-tenant.
	ctx := testkit.Ctx(t,
		testkit.WithPermission(domain.PermCredentialBuat),
		testkit.WithPermission(domain.PermAuthorityEscalate))

	_, err := uc.Execute(ctx, usecase.CreateCredentialInput{
		PersonID: targetID, CredType: domain.CredNIP,
		CredValue: "199001012015011093", Password: "kata-sandi-panjang",
	})
	if !testkit.IsPermissionDenied(err) {
		t.Fatalf("aktor tanpa tenant harus ditolak walau memegang escalate, dapat: %v", err)
	}
	if len(creds.saved) != 0 {
		t.Fatal("penolakan wewenang tidak boleh menyisakan kredensial tersimpan")
	}
}
