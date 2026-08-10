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

// TestAssignCentralRole_GlobalSuccess: role GLOBAL berlaku di semua tenant sekaligus, jadi ia
// selalu melampaui wewenang aktor yang ter-scope satu tenant — menugaskannya menuntut pintu
// keluar eksplisit (ADR-019). Tanpa PermAuthorityEscalate kasus ini DITOLAK; lihat
// TestAssignCentralRole_EskalasiGlobalDitolak.
func TestAssignCentralRole_GlobalSuccess(t *testing.T) {
	roles := newFakeCentralRoles()
	role := seedRole(t, roles, domain.ScopeGlobal)
	assigns := &fakeCentralAssignments{}
	uc := usecase.NewAssignCentralRole(roles, assigns)
	ctx := testkit.Ctx(t, testkit.WithPersonID(uuid.New()), testkit.WithTenant("pemkot-surabaya"),
		testkit.WithPermission(domain.PermCentralRoleAssign),
		testkit.WithPermission(domain.PermAuthorityEscalate))

	_, err := uc.Execute(ctx, usecase.AssignCentralRoleInput{PersonID: uuid.New(), RoleID: role.ID})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(assigns.saved) != 1 {
		t.Fatalf("assignment harus tersimpan, dapat %d", len(assigns.saved))
	}
}

// TestAssignCentralRole_ScopedSuccess: role scoped ke tenant AKTOR sendiri, dan role itu tak
// memberi permission apa pun — keduanya dalam wewenang aktor, jadi tak butuh escalate.
func TestAssignCentralRole_ScopedSuccess(t *testing.T) {
	roles := newFakeCentralRoles()
	role := seedRole(t, roles, domain.ScopeScoped)
	assigns := &fakeCentralAssignments{}
	uc := usecase.NewAssignCentralRole(roles, assigns)
	ctx := testkit.Ctx(t, testkit.WithPersonID(uuid.New()), testkit.WithTenant("pemkot-surabaya"),
		testkit.WithPermission(domain.PermCentralRoleAssign))

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

// --- Containment aktor→target (ADR-019 / REVIEW_BACKLOG B7) ---

// TestAssignCentralRole_EskalasiGlobalDitolak adalah B7 butir (c): tanpa gerbang containment,
// "boleh menugaskan role" efektif setara dengan "boleh menjadi apa pun" — pemegang
// identity:central_role:assign dapat menugaskan super_admin, termasuk kepada dirinya sendiri.
func TestAssignCentralRole_EskalasiGlobalDitolak(t *testing.T) {
	roles := newFakeCentralRoles()
	role := seedRole(t, roles, domain.ScopeGlobal)
	assigns := &fakeCentralAssignments{}
	uc := usecase.NewAssignCentralRole(roles, assigns)
	// Aktor punya izin menugaskan, TIDAK punya pintu keluar.
	ctx := testkit.Ctx(t, testkit.WithPersonID(uuid.New()), testkit.WithTenant("pemkot-surabaya"),
		testkit.WithPermission(domain.PermCentralRoleAssign))

	_, err := uc.Execute(ctx, usecase.AssignCentralRoleInput{PersonID: uuid.New(), RoleID: role.ID})
	if !testkit.IsPermissionDenied(err) {
		t.Fatalf("menugaskan role GLOBAL tanpa escalate harus ditolak, dapat: %v", err)
	}
	if len(assigns.saved) != 0 {
		t.Fatal("penolakan wewenang tidak boleh menyisakan assignment tersimpan")
	}
}

// TestAssignCentralRole_ScopeLuarTenantAktorDitolak adalah B7 butir (b) pada jalur role:
// helpdesk regional Jatim tak boleh men-scope-kan role ke tenant di luar wewenangnya.
func TestAssignCentralRole_ScopeLuarTenantAktorDitolak(t *testing.T) {
	roles := newFakeCentralRoles()
	role := seedRole(t, roles, domain.ScopeScoped)
	assigns := &fakeCentralAssignments{}
	uc := usecase.NewAssignCentralRole(roles, assigns)
	ctx := testkit.Ctx(t, testkit.WithPersonID(uuid.New()), testkit.WithTenant("pemkot-surabaya"),
		testkit.WithPermission(domain.PermCentralRoleAssign))

	_, err := uc.Execute(ctx, usecase.AssignCentralRoleInput{
		PersonID: uuid.New(), RoleID: role.ID, TenantScope: []string{"pemkot-malang"},
	})
	if !testkit.IsPermissionDenied(err) {
		t.Fatalf("scope ke tenant lain tanpa escalate harus ditolak, dapat: %v", err)
	}
	if len(assigns.saved) != 0 {
		t.Fatal("penolakan wewenang tidak boleh menyisakan assignment tersimpan")
	}
}

// TestAssignCentralRole_PermissionTakDipegangDitolak adalah aturan Kubernetes apa adanya:
// seseorang hanya boleh memberikan permission yang ia sendiri pegang. Role di sini memberi
// identity:tenant:nonaktif, aktor tidak memegangnya.
func TestAssignCentralRole_PermissionTakDipegangDitolak(t *testing.T) {
	roles := newFakeCentralRoles()
	role := &domain.CentralRole{
		ID: uuid.New(), Name: "regional_helpdesk", Label: "Helpdesk",
		ScopeType:   domain.ScopeScoped,
		Permissions: []string{domain.PermTenantNonaktif},
	}
	_ = roles.Save(context.Background(), role)
	assigns := &fakeCentralAssignments{}
	uc := usecase.NewAssignCentralRole(roles, assigns)
	ctx := testkit.Ctx(t, testkit.WithPersonID(uuid.New()), testkit.WithTenant("pemkot-surabaya"),
		testkit.WithPermission(domain.PermCentralRoleAssign))

	_, err := uc.Execute(ctx, usecase.AssignCentralRoleInput{
		PersonID: uuid.New(), RoleID: role.ID, TenantScope: []string{"pemkot-surabaya"},
	})
	if !testkit.IsPermissionDenied(err) {
		t.Fatalf("memberikan permission yang tak dipegang harus ditolak, dapat: %v", err)
	}

	// Aktor yang MEMANG memegang permission itu boleh memberikannya.
	ctxOK := testkit.Ctx(t, testkit.WithPersonID(uuid.New()), testkit.WithTenant("pemkot-surabaya"),
		testkit.WithPermission(domain.PermCentralRoleAssign),
		testkit.WithPermission(domain.PermTenantNonaktif))
	if _, err := uc.Execute(ctxOK, usecase.AssignCentralRoleInput{
		PersonID: uuid.New(), RoleID: role.ID, TenantScope: []string{"pemkot-surabaya"},
	}); err != nil {
		t.Fatalf("aktor yang memegang permission harus boleh memberikannya, dapat: %v", err)
	}
}

// TestAssignCentralRole_EskalasiDenganPintuKeluarDiizinkan mengunci sisi lain gerbang: pintu
// keluar itu benar-benar membuka, bukan hanya ada di dokumen.
func TestAssignCentralRole_EskalasiDenganPintuKeluarDiizinkan(t *testing.T) {
	roles := newFakeCentralRoles()
	role := &domain.CentralRole{
		ID: uuid.New(), Name: "super_admin", Label: "Super Admin",
		ScopeType:   domain.ScopeGlobal,
		Permissions: []string{domain.PermTenantNonaktif},
	}
	_ = roles.Save(context.Background(), role)
	assigns := &fakeCentralAssignments{}
	uc := usecase.NewAssignCentralRole(roles, assigns)
	ctx := testkit.Ctx(t, testkit.WithPersonID(uuid.New()), testkit.WithTenant("pemkot-surabaya"),
		testkit.WithPermission(domain.PermCentralRoleAssign),
		testkit.WithPermission(domain.PermAuthorityEscalate))

	if _, err := uc.Execute(ctx, usecase.AssignCentralRoleInput{
		PersonID: uuid.New(), RoleID: role.ID,
	}); err != nil {
		t.Fatalf("pemegang escalate harus boleh menugaskan role global, dapat: %v", err)
	}
	if len(assigns.saved) != 1 {
		t.Fatalf("assignment harus tersimpan, dapat %d", len(assigns.saved))
	}
}

// TestAssignCentralRole_AktorTanpaTenantDitolak: pasangan requireTenantBound pada jalur role.
// Konteks tanpa tenant tak punya evaluator, dan RequirePermission default permisif — pintu
// keluar escalate karena itu tak boleh menjadi satu-satunya penjaga.
func TestAssignCentralRole_AktorTanpaTenantDitolak(t *testing.T) {
	roles := newFakeCentralRoles()
	role := seedRole(t, roles, domain.ScopeScoped)
	assigns := &fakeCentralAssignments{}
	uc := usecase.NewAssignCentralRole(roles, assigns)
	ctx := testkit.Ctx(t, testkit.WithPersonID(uuid.New()), // tanpa WithTenant
		testkit.WithPermission(domain.PermCentralRoleAssign),
		testkit.WithPermission(domain.PermAuthorityEscalate))

	_, err := uc.Execute(ctx, usecase.AssignCentralRoleInput{
		PersonID: uuid.New(), RoleID: role.ID, TenantScope: []string{"pemkot-surabaya"},
	})
	if !testkit.IsPermissionDenied(err) {
		t.Fatalf("aktor tanpa tenant harus ditolak walau memegang escalate, dapat: %v", err)
	}
	if len(assigns.saved) != 0 {
		t.Fatal("penolakan wewenang tidak boleh menyisakan assignment tersimpan")
	}
}
