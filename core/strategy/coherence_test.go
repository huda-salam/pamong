package strategy_test

import (
	"errors"
	"testing"

	"github.com/huda-salam/pamong/core/strategy"
)

// bebanTakBolehFIFO menolak kombinasi pendekatan "beban" dengan metode persediaan "fifo"
// (contoh aturan koherensi akuntansi dari PRD F5).
func bebanTakBolehFIFO(choices map[string]string) error {
	if choices["aset.pendekatan"] == "aset.pendekatan.beban" &&
		choices["keuangan.persediaan"] == "keuangan.persediaan.fifo" {
		return errors.New("pendekatan beban tidak boleh dengan metode fifo")
	}
	return nil
}

func TestCoherence_RejectsIncoherentCombination(t *testing.T) {
	reg := strategy.NewCoherenceRegistry()
	if err := reg.Register("beban_vs_fifo", bebanTakBolehFIFO); err != nil {
		t.Fatal(err)
	}

	// DoD: kombinasi tak koheren yang didaftarkan → ditolak.
	err := reg.Validate(map[string]string{
		"aset.pendekatan":     "aset.pendekatan.beban",
		"keuangan.persediaan": "keuangan.persediaan.fifo",
	})
	if err == nil {
		t.Fatal("kombinasi tak koheren harus ditolak")
	}

	// Kombinasi koheren → lolos.
	if err := reg.Validate(map[string]string{
		"aset.pendekatan":     "aset.pendekatan.aset",
		"keuangan.persediaan": "keuangan.persediaan.fifo",
	}); err != nil {
		t.Fatalf("kombinasi koheren harus lolos: %v", err)
	}
}

func TestCoherence_NoValidators_AlwaysCoherent(t *testing.T) {
	reg := strategy.NewCoherenceRegistry()
	if err := reg.Validate(map[string]string{"x": "y"}); err != nil {
		t.Fatalf("tanpa validator harus selalu koheren: %v", err)
	}
}

func TestCoherence_RegisterRejectsDuplicateAndNil(t *testing.T) {
	reg := strategy.NewCoherenceRegistry()
	if err := reg.Register("v", bebanTakBolehFIFO); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register("v", bebanTakBolehFIFO); err == nil {
		t.Error("nama ganda harus ditolak")
	}
	if err := reg.Register("nil_v", nil); err == nil {
		t.Error("validator nil harus ditolak")
	}
	if err := reg.Register("", bebanTakBolehFIFO); err == nil {
		t.Error("nama kosong harus ditolak")
	}
}

// Semua validator dijalankan; salah satu menolak → Validate menolak.
func TestCoherence_RunsAllValidators(t *testing.T) {
	reg := strategy.NewCoherenceRegistry()
	called := map[string]bool{}
	_ = reg.Register("a_selalu_lolos", func(map[string]string) error { called["a"] = true; return nil })
	_ = reg.Register("b_menolak", func(map[string]string) error {
		called["b"] = true
		return errors.New("b menolak")
	})

	if err := reg.Validate(map[string]string{}); err == nil {
		t.Fatal("harus menolak karena validator b")
	}
	// Urutan deterministik (nama terurut): a jalan sebelum b.
	if !called["a"] || !called["b"] {
		t.Fatalf("kedua validator harus dijalankan, got %v", called)
	}
}
