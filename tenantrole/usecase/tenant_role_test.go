package usecase_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
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
