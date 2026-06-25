package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/identity/usecase"
	"github.com/huda-salam/pamong/testkit"
)

// fakeRegistry in-memory untuk unit test tenant.
type fakeRegistry struct {
	byID map[string]*domain.Tenant
}

func newFakeRegistry() *fakeRegistry {
	return &fakeRegistry{byID: map[string]*domain.Tenant{}}
}

func (f *fakeRegistry) Save(_ context.Context, t *domain.Tenant) error {
	if _, dup := f.byID[t.TenantID]; dup {
		return core.ErrConflict("tenant sudah terdaftar: " + t.TenantID)
	}
	cp := *t
	f.byID[t.TenantID] = &cp
	return nil
}
func (f *fakeRegistry) FindByID(_ context.Context, id string) (*domain.Tenant, error) {
	if t, ok := f.byID[id]; ok {
		return t, nil
	}
	return nil, core.ErrNotFound("Tenant", id)
}
func (f *fakeRegistry) List(context.Context) ([]*domain.Tenant, error) {
	out := make([]*domain.Tenant, 0, len(f.byID))
	for _, t := range f.byID {
		out = append(out, t)
	}
	return out, nil
}
func (f *fakeRegistry) SetActive(_ context.Context, id string, active bool) error {
	t, ok := f.byID[id]
	if !ok {
		return core.ErrNotFound("Tenant", id)
	}
	t.IsActive = active
	return nil
}

func TestRegisterTenant_Success_DefaultTier(t *testing.T) {
	reg := newFakeRegistry()
	uc := usecase.NewRegisterTenant(reg)
	ctx := testkit.Ctx(t, testkit.WithPermission(domain.PermTenantDaftar))

	tenant, err := uc.Execute(ctx, usecase.RegisterTenantInput{
		TenantID: "pemkot-malang", Nama: "Pemkot Malang", DBHost: "db1", DBName: "gov_pemkot_malang",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if tenant.Tier != domain.TierShared || !tenant.IsActive {
		t.Fatalf("default tier/aktif salah: %+v", tenant)
	}
}

func TestRegisterTenant_PermissionDenied(t *testing.T) {
	uc := usecase.NewRegisterTenant(newFakeRegistry())
	ctx := testkit.Ctx(t)
	_, err := uc.Execute(ctx, usecase.RegisterTenantInput{TenantID: "pemkot-x", Nama: "X", DBHost: "h", DBName: "n"})
	var fe *core.FrameworkError
	if !errors.As(err, &fe) || fe.Code != "PERMISSION_DENIED" {
		t.Fatalf("harus PERMISSION_DENIED, dapat: %v", err)
	}
}

func TestRegisterTenant_Invalid(t *testing.T) {
	uc := usecase.NewRegisterTenant(newFakeRegistry())
	ctx := testkit.Ctx(t, testkit.WithPermission(domain.PermTenantDaftar))
	_, err := uc.Execute(ctx, usecase.RegisterTenantInput{TenantID: "X", Nama: "X", DBHost: "h", DBName: "n"})
	if !errors.Is(err, domain.ErrTenantIDInvalid) {
		t.Fatalf("harus ErrTenantIDInvalid, dapat: %v", err)
	}
}

func TestDeactivateTenant_Success(t *testing.T) {
	reg := newFakeRegistry()
	_ = reg.Save(context.Background(), &domain.Tenant{TenantID: "pemkot-malang", IsActive: true})
	uc := usecase.NewDeactivateTenant(reg)
	ctx := testkit.Ctx(t, testkit.WithPermission(domain.PermTenantNonaktif))

	if err := uc.Execute(ctx, "pemkot-malang"); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got, _ := reg.FindByID(context.Background(), "pemkot-malang")
	if got.IsActive {
		t.Fatal("tenant harus nonaktif setelah deactivate")
	}
}

func TestDeactivateTenant_NotFound(t *testing.T) {
	uc := usecase.NewDeactivateTenant(newFakeRegistry())
	ctx := testkit.Ctx(t, testkit.WithPermission(domain.PermTenantNonaktif))
	err := uc.Execute(ctx, "tidak-ada")
	var fe *core.FrameworkError
	if !errors.As(err, &fe) || fe.Code != "NOT_FOUND" {
		t.Fatalf("harus NOT_FOUND, dapat: %v", err)
	}
}

func TestListTenants_Success(t *testing.T) {
	reg := newFakeRegistry()
	_ = reg.Save(context.Background(), &domain.Tenant{TenantID: "pemkot-a", IsActive: true})
	_ = reg.Save(context.Background(), &domain.Tenant{TenantID: "pemkot-b", IsActive: true})
	uc := usecase.NewListTenants(reg)
	ctx := testkit.Ctx(t, testkit.WithPermission(domain.PermTenantBaca))

	list, err := uc.Execute(ctx)
	if err != nil || len(list) != 2 {
		t.Fatalf("list: %v / jumlah=%d", err, len(list))
	}
}

func TestListTenants_PermissionDenied(t *testing.T) {
	uc := usecase.NewListTenants(newFakeRegistry())
	_, err := uc.Execute(testkit.Ctx(t))
	var fe *core.FrameworkError
	if !errors.As(err, &fe) || fe.Code != "PERMISSION_DENIED" {
		t.Fatalf("harus PERMISSION_DENIED, dapat: %v", err)
	}
}
