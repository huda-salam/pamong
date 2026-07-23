package config_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core/config"
	"github.com/huda-salam/pamong/port"
	"github.com/huda-salam/pamong/testkit"
)

// fiscalStub mengembalikan status tetap untuk semua tanggal — cukup untuk menguji gerbang.
type fiscalStub struct {
	status port.FiscalPeriodStatus
	err    error
}

func (f fiscalStub) CheckPeriod(_ context.Context, _ string, _ time.Time) (port.FiscalPeriodStatus, error) {
	return f.status, f.err
}

func TestSetChoice_RecordsActorAndVersions(t *testing.T) {
	store := config.NewMemoryTenantConfigStore()
	mgr := config.NewChoiceManager(store, nil) // fiscal nil → gerbang dilewati
	r := config.NewResolver(store)

	actor := uuid.New()
	ctx := testkit.Ctx(t, testkit.WithTenant(tenant), testkit.WithPersonID(actor))
	scope := config.ConfigScope{TenantID: tenant}

	if err := mgr.SetChoice(ctx, scope, "keuangan.persediaan", "fifo", time.Time{}); err != nil {
		t.Fatalf("SetChoice: %v", err)
	}

	cands, _ := store.Candidates(context.Background(), tenant, "keuangan.persediaan")
	if len(cands) != 1 {
		t.Fatalf("want 1 versi, got %d", len(cands))
	}
	if cands[0].SetBy == nil || *cands[0].SetBy != actor {
		t.Errorf("SetBy: want %v, got %v", actor, cands[0].SetBy)
	}
	if cands[0].EffectiveFrom.IsZero() {
		t.Error("EffectiveFrom harus terisi (default sekarang)")
	}
	if v, ok := mustResolve(t, r, scope, "keuangan.persediaan"); !ok || v != "fifo" {
		t.Errorf("resolve: want fifo, got %q/%v", v, ok)
	}
}

// DoD non-retroaktif (write gate): SetChoice pada periode hard-closed ditolak.
func TestSetChoice_RejectsLockedPeriod(t *testing.T) {
	store := config.NewMemoryTenantConfigStore()
	mgr := config.NewChoiceManager(store, fiscalStub{status: port.FiscalHardClosed})

	ctx := testkit.Ctx(t, testkit.WithTenant(tenant), testkit.WithPersonID(uuid.New()))
	scope := config.ConfigScope{TenantID: tenant}

	err := mgr.SetChoice(ctx, scope, "keuangan.persediaan", "average",
		time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatal("SetChoice pada periode terkunci harus ditolak")
	}
	// Tidak ada versi tertulis saat ditolak.
	cands, _ := store.Candidates(context.Background(), tenant, "keuangan.persediaan")
	if len(cands) != 0 {
		t.Fatalf("periode terkunci: tidak boleh ada versi tertulis, got %d", len(cands))
	}
}

func TestSetChoice_AllowsOpenPeriod(t *testing.T) {
	store := config.NewMemoryTenantConfigStore()
	mgr := config.NewChoiceManager(store, fiscalStub{status: port.FiscalOpen})

	ctx := testkit.Ctx(t, testkit.WithTenant(tenant), testkit.WithPersonID(uuid.New()))
	scope := config.ConfigScope{TenantID: tenant}

	if err := mgr.SetChoice(ctx, scope, "keuangan.persediaan", "average",
		time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("periode open harus diterima: %v", err)
	}
}
