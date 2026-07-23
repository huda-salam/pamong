package strategy_test

import (
	"context"
	"testing"

	"github.com/huda-salam/pamong/core/config"
	"github.com/huda-salam/pamong/core/strategy"
)

// TestConfigSelectionSource memverifikasi Registry bekerja dengan sumber pilihan produksi
// (resolver tenant config), bukan hanya MemorySelectionSource.
func TestConfigSelectionSource_ResolveViaConfig(t *testing.T) {
	store := config.NewMemoryTenantConfigStore()
	if err := store.Set(context.Background(), config.ConfigEntry{
		Scope: config.ConfigScope{TenantID: "pemkot-surabaya"},
		Key:   "keuangan.persediaan", Value: "keuangan.persediaan.fifo",
	}); err != nil {
		t.Fatal(err)
	}

	reg := strategy.New(strategy.NewConfigSelectionSource(config.NewResolver(store)))
	if err := reg.Register("keuangan.persediaan.fifo", "impl-fifo"); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register("keuangan.persediaan.average", "impl-average"); err != nil {
		t.Fatal(err)
	}

	impl, err := reg.Resolve(context.Background(), "pemkot-surabaya", "keuangan.persediaan")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if impl != "impl-fifo" {
		t.Fatalf("want impl-fifo, got %v", impl)
	}
}

func TestConfigSelectionSource_NoChoiceFallsBackToDefault(t *testing.T) {
	reg := strategy.New(strategy.NewConfigSelectionSource(
		config.NewResolver(config.NewMemoryTenantConfigStore())))
	if err := reg.Register("keuangan.persediaan.fifo", "impl-fifo"); err != nil {
		t.Fatal(err)
	}
	if err := reg.SetDefault("keuangan.persediaan.fifo"); err != nil {
		t.Fatal(err)
	}

	// Tenant belum memilih apapun → registry pakai default developer.
	impl, err := reg.Resolve(context.Background(), "tenant-tanpa-config", "keuangan.persediaan")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if impl != "impl-fifo" {
		t.Fatalf("want default impl-fifo, got %v", impl)
	}
}
