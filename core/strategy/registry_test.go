package strategy_test

import (
	"context"
	"errors"
	"testing"

	"github.com/huda-salam/pamong/core/strategy"
	"github.com/huda-salam/pamong/port"
)

// ===== Dummy decision point + dua varian (DoD) =====
//
// Decision point "keuangan.persediaan": metode penilaian persediaan. Dua varian sah.

// MetodePersediaan adalah interface decision-point yang ditulis modul keuangan.
type MetodePersediaan interface {
	Nilai(hargaMasuk []int) int
}

type fifo struct{}

func (fifo) Nilai(h []int) int {
	if len(h) == 0 {
		return 0
	}
	return h[0] // ambil yang pertama masuk
}

type average struct{}

func (average) Nilai(h []int) int {
	if len(h) == 0 {
		return 0
	}
	sum := 0
	for _, v := range h {
		sum += v
	}
	return sum / len(h)
}

const (
	pointPersediaan = "keuangan.persediaan"
	keyFIFO         = "keuangan.persediaan.fifo"
	keyAverage      = "keuangan.persediaan.average"
)

func newRegistry(t *testing.T) (*strategy.Registry, *strategy.MemorySelectionSource) {
	t.Helper()
	sel := strategy.NewMemorySelectionSource()
	reg := strategy.New(sel)
	if err := reg.Register(keyFIFO, fifo{}); err != nil {
		t.Fatalf("register fifo: %v", err)
	}
	if err := reg.Register(keyAverage, average{}); err != nil {
		t.Fatalf("register average: %v", err)
	}
	return reg, sel
}

// ===== DoD: dua strategy ter-register; use case memilih via key dari config =====

func TestRegistry_ResolveByTenantSelection(t *testing.T) {
	reg, sel := newRegistry(t)
	ctx := context.Background()

	// Tenant A memilih FIFO.
	sel.Set("pemkot-a", pointPersediaan, keyFIFO)
	implA, err := reg.Resolve(ctx, "pemkot-a", pointPersediaan)
	if err != nil {
		t.Fatalf("resolve A: %v", err)
	}
	if got := implA.(MetodePersediaan).Nilai([]int{100, 200, 300}); got != 100 {
		t.Errorf("FIFO harusnya 100, dapat %d", got)
	}

	// Tenant B memilih average — impl identik dipanggil, hasil beda karena pilihan beda.
	sel.Set("pemkot-b", pointPersediaan, keyAverage)
	implB, err := reg.Resolve(ctx, "pemkot-b", pointPersediaan)
	if err != nil {
		t.Fatalf("resolve B: %v", err)
	}
	if got := implB.(MetodePersediaan).Nilai([]int{100, 200, 300}); got != 200 {
		t.Errorf("average harusnya 200, dapat %d", got)
	}
}

func TestResolveAs_TypedHelper(t *testing.T) {
	reg, sel := newRegistry(t)
	sel.Set("pemkot-a", pointPersediaan, keyFIFO)

	m, err := strategy.ResolveAs[MetodePersediaan](context.Background(), reg, "pemkot-a", pointPersediaan)
	if err != nil {
		t.Fatalf("ResolveAs: %v", err)
	}
	if got := m.Nilai([]int{50, 99}); got != 50 {
		t.Errorf("FIFO via ResolveAs harusnya 50, dapat %d", got)
	}
}

func TestResolveAs_TipeTidakCocok(t *testing.T) {
	reg, sel := newRegistry(t)
	sel.Set("pemkot-a", pointPersediaan, keyFIFO)

	// Interface yang tidak dipenuhi impl fifo{}.
	type Lain interface{ Sesuatu() }
	if _, err := strategy.ResolveAs[Lain](context.Background(), reg, "pemkot-a", pointPersediaan); err == nil {
		t.Fatal("ResolveAs ke tipe tak cocok harus error")
	}
}

// ===== F2: validasi key =====

func TestRegistry_Resolve_KeyTakTerdaftarDitolak(t *testing.T) {
	reg, sel := newRegistry(t)
	// Pilihan tenant merujuk varian yang tidak didaftarkan (LIFO tak ada).
	sel.Set("pemkot-a", pointPersediaan, "keuangan.persediaan.lifo")

	_, err := reg.Resolve(context.Background(), "pemkot-a", pointPersediaan)
	if err == nil {
		t.Fatal("key tak terdaftar harus ditolak — tidak ada fallback diam-diam")
	}
}

func TestRegistry_Resolve_TanpaPilihanTanpaDefault(t *testing.T) {
	reg, _ := newRegistry(t)
	_, err := reg.Resolve(context.Background(), "pemkot-tanpa-pilihan", pointPersediaan)
	if err == nil {
		t.Fatal("decision point tanpa pilihan & tanpa default harus error")
	}
}

func TestRegistry_Resolve_DecisionPointTakDikenal(t *testing.T) {
	reg, sel := newRegistry(t)
	sel.Set("pemkot-a", "keuangan.tidak_ada", "keuangan.tidak_ada.x")
	_, err := reg.Resolve(context.Background(), "pemkot-a", "keuangan.tidak_ada")
	if err == nil {
		t.Fatal("decision point tanpa varian terdaftar harus error")
	}
}

// ===== Default developer =====

func TestRegistry_Resolve_DefaultDipakaiSaatTakMemilih(t *testing.T) {
	reg, _ := newRegistry(t)
	if err := reg.SetDefault(keyFIFO); err != nil {
		t.Fatalf("SetDefault: %v", err)
	}
	// Tenant belum memilih → pakai default (FIFO).
	impl, err := reg.Resolve(context.Background(), "pemkot-baru", pointPersediaan)
	if err != nil {
		t.Fatalf("resolve default: %v", err)
	}
	if got := impl.(MetodePersediaan).Nilai([]int{7, 8}); got != 7 {
		t.Errorf("default FIFO harusnya 7, dapat %d", got)
	}
}

func TestRegistry_Resolve_PilihanTenantMenangAtasDefault(t *testing.T) {
	reg, sel := newRegistry(t)
	_ = reg.SetDefault(keyFIFO)
	sel.Set("pemkot-a", pointPersediaan, keyAverage)

	impl, err := reg.Resolve(context.Background(), "pemkot-a", pointPersediaan)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got := impl.(MetodePersediaan).Nilai([]int{100, 300}); got != 200 {
		t.Errorf("pilihan tenant (average=200) harus menang atas default, dapat %d", got)
	}
}

func TestRegistry_SetDefault_KeyTakTerdaftar(t *testing.T) {
	reg, _ := newRegistry(t)
	if err := reg.SetDefault("keuangan.persediaan.lifo"); err == nil {
		t.Fatal("SetDefault ke key tak terdaftar harus error")
	}
}

// ===== Register: format & duplikat =====

func TestRegistry_Register_FormatKeyInvalid(t *testing.T) {
	reg := strategy.New(strategy.NewMemorySelectionSource())
	cases := []string{
		"",                     // kosong
		"fifo",                 // 1 segmen
		"persediaan.fifo",      // 2 segmen (butuh {modul}.{titik}.{varian})
		"keuangan..fifo",       // segmen kosong
		"keuangan.persediaan.", // varian kosong
	}
	for _, key := range cases {
		if err := reg.Register(key, fifo{}); err == nil {
			t.Errorf("Register(%q) harusnya menolak format invalid", key)
		}
	}
}

func TestRegistry_Register_NilImpl(t *testing.T) {
	reg := strategy.New(strategy.NewMemorySelectionSource())
	if err := reg.Register(keyFIFO, nil); err == nil {
		t.Fatal("Register impl nil harus error")
	}
}

func TestRegistry_Register_Duplikat(t *testing.T) {
	reg := strategy.New(strategy.NewMemorySelectionSource())
	if err := reg.Register(keyFIFO, fifo{}); err != nil {
		t.Fatalf("register pertama: %v", err)
	}
	if err := reg.Register(keyFIFO, average{}); err == nil {
		t.Fatal("Register key duplikat harus error")
	}
}

// ===== AvailableOptions =====

func TestRegistry_AvailableOptions_TerurutSemuaVarian(t *testing.T) {
	reg, _ := newRegistry(t)
	opts, err := reg.AvailableOptions(context.Background(), "pemkot-a", pointPersediaan)
	if err != nil {
		t.Fatalf("AvailableOptions: %v", err)
	}
	// Terurut: average sebelum fifo.
	want := []string{keyAverage, keyFIFO}
	if len(opts) != len(want) {
		t.Fatalf("jumlah opsi = %d, mau %d (%v)", len(opts), len(want), opts)
	}
	for i := range want {
		if opts[i] != want[i] {
			t.Errorf("opts[%d] = %q, mau %q", i, opts[i], want[i])
		}
	}
}

func TestRegistry_AvailableOptions_DecisionPointTakDikenal(t *testing.T) {
	reg, _ := newRegistry(t)
	if _, err := reg.AvailableOptions(context.Background(), "pemkot-a", "modul.tak_ada"); err == nil {
		t.Fatal("AvailableOptions untuk decision point tak dikenal harus error")
	}
}

// ===== Kontrak port =====

func TestRegistry_MemenuhiPortInterface(t *testing.T) {
	var _ port.StrategyRegistry = strategy.New(strategy.NewMemorySelectionSource())
}

// ===== SelectionSource error dipropagasi =====

type failingSelection struct{}

func (failingSelection) SelectedKey(_ context.Context, _, _ string) (string, bool, error) {
	return "", false, errors.New("db down")
}

func TestRegistry_Resolve_SelectionSourceError(t *testing.T) {
	reg := strategy.New(failingSelection{})
	_ = reg.Register(keyFIFO, fifo{})
	if _, err := reg.Resolve(context.Background(), "pemkot-a", pointPersediaan); err == nil {
		t.Fatal("error dari SelectionSource harus dipropagasi")
	}
}
