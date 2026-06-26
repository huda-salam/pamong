package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/identity/usecase"
	"github.com/huda-salam/pamong/testkit"
)

// --- Fakes role sentral ---

type fakeCentralRoles struct {
	byID map[uuid.UUID]*domain.CentralRole
}

func newFakeCentralRoles() *fakeCentralRoles {
	return &fakeCentralRoles{byID: map[uuid.UUID]*domain.CentralRole{}}
}
func (f *fakeCentralRoles) Save(_ context.Context, r *domain.CentralRole) error {
	f.byID[r.ID] = r
	return nil
}
func (f *fakeCentralRoles) FindByID(_ context.Context, id uuid.UUID) (*domain.CentralRole, error) {
	if r, ok := f.byID[id]; ok {
		return r, nil
	}
	return nil, core.ErrNotFound("CentralRole", id.String())
}
func (f *fakeCentralRoles) FindByName(_ context.Context, name string) (*domain.CentralRole, error) {
	for _, r := range f.byID {
		if r.Name == name {
			return r, nil
		}
	}
	return nil, core.ErrNotFound("CentralRole", name)
}
func (f *fakeCentralRoles) List(context.Context) ([]*domain.CentralRole, error) {
	out := make([]*domain.CentralRole, 0, len(f.byID))
	for _, r := range f.byID {
		out = append(out, r)
	}
	return out, nil
}

type fakeCentralAssignments struct {
	saved []*domain.CentralRoleAssignment
}

func (f *fakeCentralAssignments) Save(_ context.Context, a *domain.CentralRoleAssignment) error {
	f.saved = append(f.saved, a)
	return nil
}
func (f *fakeCentralAssignments) ListByPerson(_ context.Context, personID uuid.UUID) ([]*domain.CentralRoleAssignment, error) {
	var out []*domain.CentralRoleAssignment
	for _, a := range f.saved {
		if a.PersonID == personID {
			out = append(out, a)
		}
	}
	return out, nil
}

// --- CreateCentralRole ---

func TestCreateCentralRole_Success(t *testing.T) {
	roles := newFakeCentralRoles()
	uc := usecase.NewCreateCentralRole(roles)
	ctx := testkit.Ctx(t, testkit.WithPermission(domain.PermCentralRoleBuat))

	r, err := uc.Execute(ctx, usecase.CreateCentralRoleInput{
		Name: "platform_auditor", Label: "Auditor Platform", ScopeType: domain.ScopeGlobal,
		Permissions: []string{"identity:tenant:baca"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if r.ID == uuid.Nil || len(roles.byID) != 1 {
		t.Fatalf("role harus tersimpan: %+v", roles.byID)
	}
}

func TestCreateCentralRole_DedupPermissions(t *testing.T) {
	roles := newFakeCentralRoles()
	uc := usecase.NewCreateCentralRole(roles)
	ctx := testkit.Ctx(t, testkit.WithPermission(domain.PermCentralRoleBuat))

	// Permission duplikat (mis. UI gabung beberapa group) tidak boleh menggandakan grant.
	r, err := uc.Execute(ctx, usecase.CreateCentralRoleInput{
		Name: "platform_auditor", Label: "Auditor", ScopeType: domain.ScopeGlobal,
		Permissions: []string{"identity:tenant:baca", "identity:tenant:baca", "identity:tenant:nonaktif"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(r.Permissions) != 2 {
		t.Fatalf("permission harus ter-dedup jadi 2, dapat %v", r.Permissions)
	}
}

func TestCreateCentralRole_PermissionDenied(t *testing.T) {
	uc := usecase.NewCreateCentralRole(newFakeCentralRoles())
	ctx := testkit.Ctx(t) // tanpa permission
	_, err := uc.Execute(ctx, usecase.CreateCentralRoleInput{
		Name: "platform_auditor", Label: "Auditor", ScopeType: domain.ScopeGlobal,
	})
	if !testkit.IsPermissionDenied(err) {
		t.Fatalf("harus PERMISSION_DENIED, dapat: %v", err)
	}
}

func TestCreateCentralRole_NameInvalid(t *testing.T) {
	uc := usecase.NewCreateCentralRole(newFakeCentralRoles())
	ctx := testkit.Ctx(t, testkit.WithPermission(domain.PermCentralRoleBuat))
	_, err := uc.Execute(ctx, usecase.CreateCentralRoleInput{
		Name: "Platform Auditor", Label: "x", ScopeType: domain.ScopeGlobal, // spasi + huruf besar
	})
	if !errors.Is(err, domain.ErrCentralRoleNameInvalid) {
		t.Fatalf("nama invalid harus ditolak, dapat: %v", err)
	}
}

// --- AssignCentralRole ---

func seedRole(t *testing.T, roles *fakeCentralRoles, scope domain.ScopeType) *domain.CentralRole {
	t.Helper()
	r := &domain.CentralRole{ID: uuid.New(), Name: "regional_helpdesk", Label: "Helpdesk", ScopeType: scope}
	_ = roles.Save(context.Background(), r)
	return r
}

func TestAssignCentralRole_GlobalSuccess(t *testing.T) {
	roles := newFakeCentralRoles()
	role := seedRole(t, roles, domain.ScopeGlobal)
	assigns := &fakeCentralAssignments{}
	uc := usecase.NewAssignCentralRole(roles, assigns)
	ctx := testkit.Ctx(t, testkit.WithPersonID(uuid.New()), testkit.WithPermission(domain.PermCentralRoleAssign))

	_, err := uc.Execute(ctx, usecase.AssignCentralRoleInput{PersonID: uuid.New(), RoleID: role.ID})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(assigns.saved) != 1 {
		t.Fatalf("assignment harus tersimpan, dapat %d", len(assigns.saved))
	}
}

func TestAssignCentralRole_ScopedSuccess(t *testing.T) {
	roles := newFakeCentralRoles()
	role := seedRole(t, roles, domain.ScopeScoped)
	assigns := &fakeCentralAssignments{}
	uc := usecase.NewAssignCentralRole(roles, assigns)
	ctx := testkit.Ctx(t, testkit.WithPersonID(uuid.New()), testkit.WithPermission(domain.PermCentralRoleAssign))

	a, err := uc.Execute(ctx, usecase.AssignCentralRoleInput{
		PersonID: uuid.New(), RoleID: role.ID, TenantScope: []string{"pemkot-surabaya"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(a.TenantScope) != 1 || a.TenantScope[0] != "pemkot-surabaya" {
		t.Fatalf("scope tidak tersimpan: %+v", a.TenantScope)
	}
}

func TestAssignCentralRole_ScopedTanpaScopeDitolak(t *testing.T) {
	roles := newFakeCentralRoles()
	role := seedRole(t, roles, domain.ScopeScoped)
	uc := usecase.NewAssignCentralRole(roles, &fakeCentralAssignments{})
	ctx := testkit.Ctx(t, testkit.WithPersonID(uuid.New()), testkit.WithPermission(domain.PermCentralRoleAssign))

	_, err := uc.Execute(ctx, usecase.AssignCentralRoleInput{PersonID: uuid.New(), RoleID: role.ID})
	if !errors.Is(err, domain.ErrScopeWajibUntukScoped) {
		t.Fatalf("scoped tanpa scope harus ditolak, dapat: %v", err)
	}
}

func TestAssignCentralRole_GlobalDenganScopeDitolak(t *testing.T) {
	roles := newFakeCentralRoles()
	role := seedRole(t, roles, domain.ScopeGlobal)
	uc := usecase.NewAssignCentralRole(roles, &fakeCentralAssignments{})
	ctx := testkit.Ctx(t, testkit.WithPersonID(uuid.New()), testkit.WithPermission(domain.PermCentralRoleAssign))

	_, err := uc.Execute(ctx, usecase.AssignCentralRoleInput{
		PersonID: uuid.New(), RoleID: role.ID, TenantScope: []string{"pemkot-surabaya"},
	})
	if !errors.Is(err, domain.ErrScopeDilarangGlobal) {
		t.Fatalf("global dengan scope harus ditolak, dapat: %v", err)
	}
}

func TestAssignCentralRole_PermissionDenied(t *testing.T) {
	roles := newFakeCentralRoles()
	role := seedRole(t, roles, domain.ScopeGlobal)
	uc := usecase.NewAssignCentralRole(roles, &fakeCentralAssignments{})
	ctx := testkit.Ctx(t) // tanpa permission
	_, err := uc.Execute(ctx, usecase.AssignCentralRoleInput{PersonID: uuid.New(), RoleID: role.ID})
	if !testkit.IsPermissionDenied(err) {
		t.Fatalf("harus PERMISSION_DENIED, dapat: %v", err)
	}
}

func TestAssignCentralRole_RoleTidakAda(t *testing.T) {
	uc := usecase.NewAssignCentralRole(newFakeCentralRoles(), &fakeCentralAssignments{})
	ctx := testkit.Ctx(t, testkit.WithPersonID(uuid.New()), testkit.WithPermission(domain.PermCentralRoleAssign))
	_, err := uc.Execute(ctx, usecase.AssignCentralRoleInput{PersonID: uuid.New(), RoleID: uuid.New()})
	var fe *core.FrameworkError
	if !errors.As(err, &fe) || fe.Code != "NOT_FOUND" {
		t.Fatalf("role tak ada harus NOT_FOUND, dapat: %v", err)
	}
}
