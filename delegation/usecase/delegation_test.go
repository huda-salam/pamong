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

// --- Containment unit (PR-W3b, ADR-021) ---
//
// Delegasi adalah jalur MANDIRI di evaluator (tak tunduk strict-intersection role), jadi ia
// permukaan eskalasi paling langsung: pembuat delegasi se-tenant bisa melimpahkan wewenang ke akun
// mana pun tanpa menyentuh satu pun role.

// TestCreateDelegation_UnitDiluarJangkauan_Ditolak: unit sasaran di luar jangkauan pembuat → 403.
func TestCreateDelegation_UnitDiluarJangkauan_Ditolak(t *testing.T) {
	repo := &fakeDelegationRepo{}
	uc := usecase.NewCreateDelegation(repo, domain.NewNonDelegableSet())

	unitSaya, unitOrangLain := uuid.New(), uuid.New()
	ctx := testkit.Ctx(t,
		testkit.WithPermission(domain.PermDelegasiBuat),
		testkit.WithUnitAuthority(unitSaya),
	)

	if _, err := uc.Execute(ctx, usecase.CreateDelegationInput{
		FromUserID: uuid.New(), ToUserID: uuid.New(),
		Permissions: []string{"keuangan:spm:baca"},
		UnitKerjaID: &unitOrangLain,
		ValidUntil:  time.Now().Add(time.Hour),
	}); err == nil {
		t.Fatal("delegasi pada unit di luar jangkauan harus ditolak")
	}
	if repo.saved != nil {
		t.Fatalf("delegasi tersimpan padahal ditolak: %+v", repo.saved)
	}
}

// TestCreateDelegation_SeTenantTanpaWewenangSeTenant_Ditolak: eskalasi lewat field yang dibiarkan
// kosong — `unit_kerja_id` nil = SELURUH TENANT.
func TestCreateDelegation_SeTenantTanpaWewenangSeTenant_Ditolak(t *testing.T) {
	repo := &fakeDelegationRepo{}
	uc := usecase.NewCreateDelegation(repo, domain.NewNonDelegableSet())

	ctx := testkit.Ctx(t,
		testkit.WithPermission(domain.PermDelegasiBuat),
		testkit.WithUnitAuthority(uuid.New()), // satu unit, bukan se-tenant
	)

	if _, err := uc.Execute(ctx, usecase.CreateDelegationInput{
		FromUserID: uuid.New(), ToUserID: uuid.New(),
		Permissions: []string{"keuangan:spm:baca"},
		UnitKerjaID: nil,
		ValidUntil:  time.Now().Add(time.Hour),
	}); err == nil {
		t.Fatal("pembuat ber-scope satu unit tidak boleh membuat delegasi SE-TENANT " +
			"hanya dengan mengosongkan unit_kerja_id")
	}
	if repo.saved != nil {
		t.Fatalf("delegasi se-tenant tersimpan padahal ditolak: %+v", repo.saved)
	}
}

// TestCreateDelegation_DalamJangkauan_Lolos: kasus normal (PLT satu OPD) tak boleh ikut tertutup.
func TestCreateDelegation_DalamJangkauan_Lolos(t *testing.T) {
	repo := &fakeDelegationRepo{}
	uc := usecase.NewCreateDelegation(repo, domain.NewNonDelegableSet())

	unit := uuid.New()
	ctx := testkit.Ctx(t,
		testkit.WithPermission(domain.PermDelegasiBuat),
		testkit.WithUnitAuthority(unit),
	)

	if _, err := uc.Execute(ctx, usecase.CreateDelegationInput{
		FromUserID: uuid.New(), ToUserID: uuid.New(),
		Permissions: []string{"keuangan:spm:baca"},
		UnitKerjaID: &unit,
		ValidUntil:  time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("delegasi di unit sendiri harus lolos: %v", err)
	}
	if repo.saved == nil {
		t.Fatal("delegasi tak tersimpan")
	}
}

// TestCreateDelegation_SubtreeTanpaWewenangSubtree_Ditolak — alasan identik dengan padanannya di
// tenantrole, dan di sini lebih tajam: delegasi tak tunduk strict-intersection role, jadi jangkauan
// subtree yang terlanjur dilimpahkan berlaku penuh bagi delegatee.
func TestCreateDelegation_SubtreeTanpaWewenangSubtree_Ditolak(t *testing.T) {
	repo := &fakeDelegationRepo{}
	uc := usecase.NewCreateDelegation(repo, domain.NewNonDelegableSet())

	unit := uuid.New()
	ctx := testkit.Ctx(t,
		testkit.WithPermission(domain.PermDelegasiBuat),
		testkit.WithUnitAuthority(unit), // satu unit saja
	)

	if _, err := uc.Execute(ctx, usecase.CreateDelegationInput{
		FromUserID: uuid.New(), ToUserID: uuid.New(),
		Permissions: []string{"keuangan:spm:baca"},
		UnitKerjaID: &unit, IncludeSubtree: true,
		ValidUntil: time.Now().Add(time.Hour),
	}); err == nil {
		t.Fatal("pembuat ber-wewenang satu unit tidak boleh melimpahkan jangkauan SUBTREE")
	}
	if repo.saved != nil {
		t.Fatalf("delegasi subtree tersimpan padahal ditolak: %+v", repo.saved)
	}
}

func TestCreateDelegation_SubtreeDenganWewenangSubtree_Lolos(t *testing.T) {
	repo := &fakeDelegationRepo{}
	uc := usecase.NewCreateDelegation(repo, domain.NewNonDelegableSet())

	unit := uuid.New()
	ctx := testkit.Ctx(t,
		testkit.WithPermission(domain.PermDelegasiBuat),
		testkit.WithSubtreeAuthority(unit),
	)

	if _, err := uc.Execute(ctx, usecase.CreateDelegationInput{
		FromUserID: uuid.New(), ToUserID: uuid.New(),
		Permissions: []string{"keuangan:spm:baca"},
		UnitKerjaID: &unit, IncludeSubtree: true,
		ValidUntil: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("pemegang wewenang subtree harus bisa melimpahkan ber-subtree: %v", err)
	}
	if repo.saved == nil || !repo.saved.IncludeSubtree {
		t.Fatalf("delegasi subtree tak tersimpan sebagaimana mestinya: %+v", repo.saved)
	}
}
