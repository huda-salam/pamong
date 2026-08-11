package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/tenantrole/domain"
	"github.com/huda-salam/pamong/tenantrole/usecase"
	"github.com/huda-salam/pamong/testkit"
)

// fakeTenantRoleRepo adalah fake lokal (precedent identity: tanpa mock testkit untuk port baru).
type fakeTenantRoleRepo struct {
	saved   *domain.TenantRole
	saveErr error
}

func (f *fakeTenantRoleRepo) Save(_ context.Context, r *domain.TenantRole) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.saved = r
	return nil
}
func (f *fakeTenantRoleRepo) FindByID(context.Context, uuid.UUID) (*domain.TenantRole, error) {
	return nil, nil
}
func (f *fakeTenantRoleRepo) FindByName(context.Context, string) (*domain.TenantRole, error) {
	return nil, nil
}
func (f *fakeTenantRoleRepo) List(context.Context) ([]*domain.TenantRole, error) { return nil, nil }

type fakeAssignmentRepo struct{ saved *domain.TenantRoleAssignment }

func (f *fakeAssignmentRepo) Save(_ context.Context, a *domain.TenantRoleAssignment) error {
	f.saved = a
	return nil
}
func (f *fakeAssignmentRepo) ListByUser(context.Context, uuid.UUID) ([]*domain.TenantRoleAssignment, error) {
	return nil, nil
}

func TestCreateTenantRole_Success(t *testing.T) {
	repo := &fakeTenantRoleRepo{}
	ctx := testkit.Ctx(t, testkit.WithPermission(domain.PermTenantRoleBuat))

	role, err := usecase.NewCreateTenantRole(repo).Execute(ctx, usecase.CreateTenantRoleInput{
		Name: "bendahara_pengeluaran", Label: "Bendahara Pengeluaran",
		Permissions: []string{"keuangan:spm:terbitkan", "keuangan:spm:terbitkan"}, // duplikat
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if repo.saved == nil || repo.saved.ID != role.ID {
		t.Fatal("role harus tersimpan")
	}
	if len(role.Permissions) != 1 {
		t.Fatalf("permission duplikat harus di-dedup, dapat: %v", role.Permissions)
	}
}

func TestCreateTenantRole_PermissionDenied(t *testing.T) {
	repo := &fakeTenantRoleRepo{}
	ctx := testkit.Ctx(t) // tanpa PermTenantRoleBuat

	_, err := usecase.NewCreateTenantRole(repo).Execute(ctx, usecase.CreateTenantRoleInput{
		Name: "bendahara_pengeluaran", Label: "x",
	})
	if err == nil {
		t.Fatal("tanpa permission harus ditolak")
	}
	if repo.saved != nil {
		t.Fatal("role tidak boleh tersimpan saat permission ditolak")
	}
}

func TestCreateTenantRole_NameInvalid(t *testing.T) {
	repo := &fakeTenantRoleRepo{}
	ctx := testkit.Ctx(t, testkit.WithPermission(domain.PermTenantRoleBuat))

	_, err := usecase.NewCreateTenantRole(repo).Execute(ctx, usecase.CreateTenantRoleInput{
		Name: "Bendahara Pengeluaran", Label: "x", // bukan snake_case
	})
	if err != domain.ErrTenantRoleNameInvalid {
		t.Fatalf("err = %v, mau ErrTenantRoleNameInvalid", err)
	}
}

// TestCreateTenantRole_PermissionIAMTakDipegang_Ditolak — containment ISI (ADR-021 Keputusan 6).
// Containment jangkauan menjaga DI MANA wewenang diberikan; ia tak menjawab APA yang diberikan.
// Pemegang `iam:tenant_role:buat` di sini TIDAK memegang `iam:delegasi:buat`, jadi ia tak boleh
// mencetak role yang memuatnya — kalau boleh, ia tinggal menugaskan role itu ke dirinya sendiri di
// unitnya sendiri (lolos containment) dan memanen permission yang tak pernah diberikan kepadanya.
func TestCreateTenantRole_PermissionIAMTakDipegang_Ditolak(t *testing.T) {
	repo := &fakeTenantRoleRepo{}
	ctx := testkit.Ctx(t, testkit.WithPermission(domain.PermTenantRoleBuat))

	_, err := usecase.NewCreateTenantRole(repo).Execute(ctx, usecase.CreateTenantRoleInput{
		Name: "wakil_admin", Label: "Wakil Admin",
		Permissions: []string{"iam:delegasi:buat"},
	})
	if err == nil {
		t.Fatal("role berisi permission iam: yang tak dipegang pembuatnya harus ditolak — " +
			"kalau tidak, pembuat role bisa mencetak wewenang untuk dirinya sendiri")
	}
	var fe *core.FrameworkError
	if !errors.As(err, &fe) || fe.Code != "PERMISSION_DENIED" {
		t.Fatalf("err = %v (%T), mau PERMISSION_DENIED", err, err)
	}
	if repo.saved != nil {
		t.Fatalf("role tidak boleh tersimpan: %+v", repo.saved)
	}
}

// TestCreateTenantRole_PermissionIAMDipegang_Lolos — sisi lain aturan itu: admin tenant yang MEMANG
// memegang permission IAM tetap bisa menyusun role untuk wakilnya. Tanpa ini, satu-satunya jalan
// membentuk admin IAM tenant adalah lewat role sentral (admin platform), yang terlalu kaku.
func TestCreateTenantRole_PermissionIAMDipegang_Lolos(t *testing.T) {
	repo := &fakeTenantRoleRepo{}
	ctx := testkit.Ctx(t,
		testkit.WithPermission(domain.PermTenantRoleBuat),
		testkit.WithPermission(domain.PermTenantRoleAssign),
	)

	if _, err := usecase.NewCreateTenantRole(repo).Execute(ctx, usecase.CreateTenantRoleInput{
		Name: "wakil_admin", Label: "Wakil Admin",
		Permissions: []string{domain.PermTenantRoleAssign},
	}); err != nil {
		t.Fatalf("pembuat yang memegang permission itu harus bisa menaruhnya di role: %v", err)
	}
	if repo.saved == nil {
		t.Fatal("role harus tersimpan")
	}
}

// TestCreateTenantRole_PermissionBisnis_TakButuhDipegang — batas aturan, dinyatakan eksplisit agar
// tak "diperbaiki" tanpa sadar: permission BISNIS tetap bebas ditaruh tanpa pembuat memegangnya.
// Mewajibkannya berarti admin IAM harus lebih dulu menjadi bendahara sebelum bisa membuat role
// bendahara. Konsekuensinya (pasangan buat+assign = wewenang setingkat admin tenant) dicatat di
// ADR-021, bukan ditutup di sini.
func TestCreateTenantRole_PermissionBisnis_TakButuhDipegang(t *testing.T) {
	repo := &fakeTenantRoleRepo{}
	ctx := testkit.Ctx(t, testkit.WithPermission(domain.PermTenantRoleBuat))

	if _, err := usecase.NewCreateTenantRole(repo).Execute(ctx, usecase.CreateTenantRoleInput{
		Name: "bendahara_pengeluaran", Label: "Bendahara",
		Permissions: []string{"keuangan:spm:terbitkan"},
	}); err != nil {
		t.Fatalf("permission bisnis tak boleh menuntut pembuat memegangnya: %v", err)
	}
}

func TestAssignTenantRole_PermissionDenied(t *testing.T) {
	repo := &fakeAssignmentRepo{}
	ctx := testkit.Ctx(t) // tanpa PermTenantRoleAssign

	_, err := usecase.NewAssignTenantRole(repo).Execute(ctx, usecase.AssignTenantRoleInput{
		UserID: uuid.New(), RoleID: uuid.New(),
	})
	if err == nil {
		t.Fatal("tanpa permission harus ditolak")
	}
	if repo.saved != nil {
		t.Fatal("assignment tidak boleh tersimpan saat permission ditolak")
	}
}

func TestAssignTenantRole_Success(t *testing.T) {
	repo := &fakeAssignmentRepo{}
	actor := uuid.New()
	ctx := testkit.Ctx(t, testkit.WithPersonID(actor), testkit.WithPermission(domain.PermTenantRoleAssign))

	user, roleID := uuid.New(), uuid.New()
	a, err := usecase.NewAssignTenantRole(repo).Execute(ctx, usecase.AssignTenantRoleInput{
		UserID: user, RoleID: roleID,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if repo.saved == nil || a.AssignedBy != actor || a.ValidFrom.IsZero() {
		t.Fatalf("assignment harus tersimpan dgn assigned_by=actor & valid_from terisi: %+v", a)
	}
}

// --- Containment unit (PR-W3b, ADR-021) ---
//
// Memegang `iam:tenant_role:assign` menjawab "boleh menugaskan", BUKAN "boleh menugaskan di mana
// pun". Tanpa lapis kedua, admin yang wewenangnya dibatasi ke satu OPD bisa menugaskan role di OPD
// lain — lalu memanen wewenang itu lewat akun yang ia kendalikan.

// TestAssignTenantRole_UnitDiluarJangkauan_Ditolak: unit sasaran di luar jangkauan actor → 403,
// dan tak ada baris yang tersimpan.
func TestAssignTenantRole_UnitDiluarJangkauan_Ditolak(t *testing.T) {
	repo := &fakeAssignmentRepo{}
	uc := usecase.NewAssignTenantRole(repo)

	unitSaya, unitOrangLain := uuid.New(), uuid.New()
	ctx := testkit.Ctx(t,
		testkit.WithPermission(domain.PermTenantRoleAssign),
		testkit.WithUnitAuthority(unitSaya),
	)

	_, err := uc.Execute(ctx, usecase.AssignTenantRoleInput{
		UserID: uuid.New(), RoleID: uuid.New(), UnitKerjaID: &unitOrangLain,
	})
	if err == nil {
		t.Fatal("menugaskan role pada unit di luar jangkauan harus ditolak")
	}
	var fe *core.FrameworkError
	if !errors.As(err, &fe) || fe.Code != "PERMISSION_DENIED" {
		t.Fatalf("mau PERMISSION_DENIED, dapat %v", err)
	}
	if repo.saved != nil {
		t.Fatalf("assignment tersimpan padahal ditolak: %+v", repo.saved)
	}
}

// TestAssignTenantRole_UnitDalamJangkauan_Lolos: kasus normal tak boleh ikut tertutup.
func TestAssignTenantRole_UnitDalamJangkauan_Lolos(t *testing.T) {
	repo := &fakeAssignmentRepo{}
	uc := usecase.NewAssignTenantRole(repo)

	unit := uuid.New()
	ctx := testkit.Ctx(t,
		testkit.WithPermission(domain.PermTenantRoleAssign),
		testkit.WithUnitAuthority(unit),
	)

	got, err := uc.Execute(ctx, usecase.AssignTenantRoleInput{
		UserID: uuid.New(), RoleID: uuid.New(), UnitKerjaID: &unit,
	})
	if err != nil {
		t.Fatalf("menugaskan role di unit sendiri harus lolos: %v", err)
	}
	if repo.saved == nil || got == nil || *repo.saved.UnitKerjaID != unit {
		t.Fatalf("assignment tak tersimpan dengan unit yang benar: %+v", repo.saved)
	}
}

// TestAssignTenantRole_SeTenantTanpaWewenangSeTenant_Ditolak adalah inti ADR-021: eskalasi lewat
// field yang DIBIARKAN KOSONG. `unit_kerja_id` nil berarti "seluruh tenant" — jangkauan TERLUAS —
// jadi admin ber-scope satu OPD tak boleh bisa membuatnya hanya dengan tidak mengisi field itu.
func TestAssignTenantRole_SeTenantTanpaWewenangSeTenant_Ditolak(t *testing.T) {
	repo := &fakeAssignmentRepo{}
	uc := usecase.NewAssignTenantRole(repo)

	ctx := testkit.Ctx(t,
		testkit.WithPermission(domain.PermTenantRoleAssign),
		testkit.WithUnitAuthority(uuid.New()), // berwenang di SATU unit, bukan se-tenant
	)

	if _, err := uc.Execute(ctx, usecase.AssignTenantRoleInput{
		UserID: uuid.New(), RoleID: uuid.New(), UnitKerjaID: nil,
	}); err == nil {
		t.Fatal("admin ber-scope satu unit tidak boleh membuat penugasan SE-TENANT " +
			"hanya dengan mengosongkan unit_kerja_id")
	}
	if repo.saved != nil {
		t.Fatalf("assignment se-tenant tersimpan padahal ditolak: %+v", repo.saved)
	}
}

// TestAssignTenantRole_SeTenantDenganWewenangSeTenant_Lolos: sisi lain dari aturan itu — pemegang
// wewenang se-tenant (grant TenantWide, dinyatakan sebagai uuid.Nil) tetap bisa bekerja.
func TestAssignTenantRole_SeTenantDenganWewenangSeTenant_Lolos(t *testing.T) {
	repo := &fakeAssignmentRepo{}
	uc := usecase.NewAssignTenantRole(repo)

	ctx := testkit.Ctx(t,
		testkit.WithPermission(domain.PermTenantRoleAssign),
		testkit.WithUnitAuthority(uuid.Nil), // uuid.Nil = wewenang se-tenant
	)

	if _, err := uc.Execute(ctx, usecase.AssignTenantRoleInput{
		UserID: uuid.New(), RoleID: uuid.New(), UnitKerjaID: nil,
	}); err != nil {
		t.Fatalf("pemegang wewenang se-tenant harus bisa menugaskan se-tenant: %v", err)
	}
	if repo.saved == nil || repo.saved.UnitKerjaID != nil {
		t.Fatalf("assignment se-tenant tak tersimpan sebagaimana mestinya: %+v", repo.saved)
	}
}

// TestAssignTenantRole_SubtreeTanpaWewenangSubtree_Ditolak — eskalasi lewat BOOLEAN, bukan lewat
// field kosong. `include_subtree` memperluas jangkauan penugasan ke SELURUH keturunan unit, jadi
// pemegang wewenang atas satu unit saja tak boleh bisa membagikannya (ADR-021).
func TestAssignTenantRole_SubtreeTanpaWewenangSubtree_Ditolak(t *testing.T) {
	repo := &fakeAssignmentRepo{}
	uc := usecase.NewAssignTenantRole(repo)

	unit := uuid.New()
	ctx := testkit.Ctx(t,
		testkit.WithPermission(domain.PermTenantRoleAssign),
		testkit.WithUnitAuthority(unit), // wewenang atas unit itu SAJA — tanpa keturunan
	)

	if _, err := uc.Execute(ctx, usecase.AssignTenantRoleInput{
		UserID: uuid.New(), RoleID: uuid.New(), UnitKerjaID: &unit, IncludeSubtree: true,
	}); err == nil {
		t.Fatal("pemegang wewenang satu unit tidak boleh menerbitkan penugasan ber-include_subtree " +
			"— itu membagikan jangkauan atas keturunan yang ia sendiri tak punya")
	}
	if repo.saved != nil {
		t.Fatalf("assignment subtree tersimpan padahal ditolak: %+v", repo.saved)
	}
}

// TestAssignTenantRole_SubtreeDenganWewenangSubtree_Lolos: sisi lain aturan itu — kepala OPD yang
// memang berwenang atas subtree-nya tetap bisa bekerja. Tanpa ini, satu-satunya jalan yang tersisa
// bagi mereka adalah penugasan SE-TENANT, yang justru lebih longgar.
func TestAssignTenantRole_SubtreeDenganWewenangSubtree_Lolos(t *testing.T) {
	repo := &fakeAssignmentRepo{}
	uc := usecase.NewAssignTenantRole(repo)

	unit := uuid.New()
	ctx := testkit.Ctx(t,
		testkit.WithPermission(domain.PermTenantRoleAssign),
		testkit.WithSubtreeAuthority(unit),
	)

	if _, err := uc.Execute(ctx, usecase.AssignTenantRoleInput{
		UserID: uuid.New(), RoleID: uuid.New(), UnitKerjaID: &unit, IncludeSubtree: true,
	}); err != nil {
		t.Fatalf("pemegang wewenang subtree harus bisa menugaskan ber-subtree: %v", err)
	}
	if repo.saved == nil || !repo.saved.IncludeSubtree {
		t.Fatalf("assignment subtree tak tersimpan sebagaimana mestinya: %+v", repo.saved)
	}
}
