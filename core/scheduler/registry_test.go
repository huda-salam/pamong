package scheduler_test

import (
	"context"
	"testing"

	"github.com/huda-salam/pamong/core/scheduler"
)

func noop(context.Context, []byte) error { return nil }

func TestRegistry_RegisterAndGet(t *testing.T) {
	r := scheduler.NewRegistry()
	if err := r.Register("laporan.harian", noop); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := r.Get("laporan.harian"); err != nil {
		t.Errorf("Get: %v", err)
	}
}

func TestRegistry_RejectsDuplicate(t *testing.T) {
	r := scheduler.NewRegistry()
	_ = r.Register("x", noop)
	if err := r.Register("x", noop); err == nil {
		t.Error("key ganda harus ditolak")
	}
}

func TestRegistry_RejectsNil(t *testing.T) {
	r := scheduler.NewRegistry()
	if err := r.Register("x", nil); err == nil {
		t.Error("handler nil harus ditolak")
	}
}

func TestRegistry_GetUnknownFails(t *testing.T) {
	r := scheduler.NewRegistry()
	if _, err := r.Get("tidak.ada"); err == nil {
		t.Error("key tak terdaftar harus error")
	}
}
