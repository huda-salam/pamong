package domain_test

import (
	"errors"
	"testing"

	"github.com/huda-salam/pamong/core/domain"
	"github.com/huda-salam/pamong/port"
	"github.com/huda-salam/pamong/testkit"
)

func TestHooks_UrutanDeterministik(t *testing.T) {
	var calls []string
	hs := domain.HookSet{
		BeforeSave: []domain.HookFunc{
			func(port.AuthContext, *domain.Entity) error { calls = append(calls, "a"); return nil },
			func(port.AuthContext, *domain.Entity) error { calls = append(calls, "b"); return nil },
			func(port.AuthContext, *domain.Entity) error { calls = append(calls, "c"); return nil },
		},
	}
	if err := hs.RunBeforeSave(testkit.NewContext(t), domain.NewEntity("X")); err != nil {
		t.Fatal(err)
	}
	if got := calls; len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Errorf("urutan hook = %v, mau [a b c]", got)
	}
}

func TestHooks_BeforeMembatalkan(t *testing.T) {
	boom := errors.New("validasi gagal")
	called := false
	hs := domain.HookSet{
		BeforeSave: []domain.HookFunc{
			func(port.AuthContext, *domain.Entity) error { return boom },
			func(port.AuthContext, *domain.Entity) error { called = true; return nil }, // tak boleh terpanggil
		},
	}
	err := hs.RunBeforeSave(testkit.NewContext(t), domain.NewEntity("X"))
	if !errors.Is(err, boom) {
		t.Fatalf("error before-hook harus diteruskan, dapat: %v", err)
	}
	if called {
		t.Error("hook setelah error tidak boleh dijalankan (operasi dibatalkan)")
	}
}

func TestHooks_AfterLaporTanpaMembatalkan(t *testing.T) {
	boom := errors.New("notifikasi gagal")
	ran := 0
	hs := domain.HookSet{
		AfterSave: []domain.HookFunc{
			func(port.AuthContext, *domain.Entity) error { ran++; return boom },
			func(port.AuthContext, *domain.Entity) error { ran++; return nil }, // tetap jalan
		},
	}
	err := hs.RunAfterSave(testkit.NewContext(t), domain.NewEntity("X"))
	if !errors.Is(err, boom) {
		t.Fatalf("error after-hook harus dilaporkan, dapat: %v", err)
	}
	if ran != 2 {
		t.Errorf("semua after-hook harus jalan meski ada yang error; ran=%d mau 2", ran)
	}
}

func TestEntity_GetSet(t *testing.T) {
	e := domain.NewEntity("SPM")
	e.Set("nomor", "001/2025")
	if e.Get("nomor") != "001/2025" {
		t.Errorf("Get/Set tidak konsisten: %v", e.Get("nomor"))
	}
}
