package usecase_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/delegation/domain"
	"github.com/huda-salam/pamong/delegation/usecase"
	"github.com/huda-salam/pamong/testkit"
)

// fakeDelegationRepo merekam delegasi yang disimpan (tanpa DB).
type fakeDelegationRepo struct{ saved *domain.Delegation }

func (r *fakeDelegationRepo) Save(_ context.Context, d *domain.Delegation) error {
	r.saved = d
	return nil
}

func (r *fakeDelegationRepo) ListActiveByDelegatee(context.Context, uuid.UUID, time.Time) ([]*domain.Delegation, error) {
	return nil, nil
}

func TestCreateDelegation_Success(t *testing.T) {
	repo := &fakeDelegationRepo{}
	uc := usecase.NewCreateDelegation(repo, domain.NewNonDelegableSet())
	ctx := testkit.Ctx(t, testkit.WithPermission(domain.PermDelegasiBuat))

	out, err := uc.Execute(ctx, usecase.CreateDelegationInput{
		FromUserID:  uuid.New(),
		ToUserID:    uuid.New(),
		Permissions: []string{"keuangan:spm:baca", "keuangan:spm:baca"}, // duplikat → di-dedup
		ValidUntil:  time.Now().Add(48 * time.Hour),
	})
	if err != nil {
		t.Fatalf("create delegasi gagal: %v", err)
	}
	if repo.saved == nil || repo.saved.ID != out.ID {
		t.Fatal("delegasi tidak tersimpan")
	}
	if len(out.Permissions) != 1 {
		t.Errorf("permission harus ter-dedup jadi 1, dapat %d", len(out.Permissions))
	}
	if out.AssignedBy != ctx.PersonID() {
		t.Error("assigned_by harus = actor")
	}
}

func TestCreateDelegation_PermissionDenied(t *testing.T) {
	uc := usecase.NewCreateDelegation(&fakeDelegationRepo{}, domain.NewNonDelegableSet())
	ctx := testkit.Ctx(t) // tanpa PermDelegasiBuat

	_, err := uc.Execute(ctx, usecase.CreateDelegationInput{
		FromUserID:  uuid.New(),
		ToUserID:    uuid.New(),
		Permissions: []string{"keuangan:spm:baca"},
		ValidUntil:  time.Now().Add(time.Hour),
	})
	if err == nil {
		t.Fatal("tanpa permission seharusnya ditolak")
	}
}

func TestCreateDelegation_NonDelegableRejected(t *testing.T) {
	const ttd = "keuangan:sp2d:tandatangan"
	repo := &fakeDelegationRepo{}
	uc := usecase.NewCreateDelegation(repo, domain.NewNonDelegableSet(ttd))
	ctx := testkit.Ctx(t, testkit.WithPermission(domain.PermDelegasiBuat))

	_, err := uc.Execute(ctx, usecase.CreateDelegationInput{
		FromUserID:  uuid.New(),
		ToUserID:    uuid.New(),
		Permissions: []string{"keuangan:spm:baca", ttd},
		ValidUntil:  time.Now().Add(time.Hour),
	})
	if err != domain.ErrPermNonDelegable {
		t.Fatalf("permission non-delegable harus ditolak, dapat: %v", err)
	}
	if repo.saved != nil {
		t.Error("tidak boleh tersimpan saat ada permission non-delegable")
	}
}

func TestCreateDelegation_InvalidPeriod(t *testing.T) {
	uc := usecase.NewCreateDelegation(&fakeDelegationRepo{}, domain.NewNonDelegableSet())
	ctx := testkit.Ctx(t, testkit.WithPermission(domain.PermDelegasiBuat))

	// ValidUntil kosong → delegasi tak berbatas → ditolak Validate.
	_, err := uc.Execute(ctx, usecase.CreateDelegationInput{
		FromUserID:  uuid.New(),
		ToUserID:    uuid.New(),
		Permissions: []string{"keuangan:spm:baca"},
	})
	if err != domain.ErrValidUntilWajib {
		t.Fatalf("delegasi tanpa valid_until harus ditolak dgn ErrValidUntilWajib, dapat: %v", err)
	}
}
