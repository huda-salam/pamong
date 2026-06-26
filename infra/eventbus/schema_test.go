package eventbus_test

import (
	"errors"
	"testing"

	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/infra/eventbus"
)

func TestSchemaRegistry_RegisterUlangTipeBerbedaDitolak(t *testing.T) {
	r := eventbus.NewSchemaRegistry()
	if err := r.Register(eventSuratDiterima, suratDiterima{}); err != nil {
		t.Fatalf("register pertama: %v", err)
	}
	err := r.Register(eventSuratDiterima, lainPayload{})
	if err == nil {
		t.Fatal("register ulang dengan tipe berbeda harus ditolak")
	}
	var fe *core.FrameworkError
	if !errors.As(err, &fe) || fe.Code != "CONFLICT" {
		t.Errorf("mau CONFLICT, dapat: %v", err)
	}
}

func TestSchemaRegistry_RegisterUlangTipeSamaIdempoten(t *testing.T) {
	r := eventbus.NewSchemaRegistry()
	_ = r.Register(eventSuratDiterima, suratDiterima{})
	if err := r.Register(eventSuratDiterima, suratDiterima{}); err != nil {
		t.Errorf("register ulang tipe sama harus lolos, dapat: %v", err)
	}
}

func TestSchemaRegistry_RegisterInputKosongDitolak(t *testing.T) {
	r := eventbus.NewSchemaRegistry()
	if err := r.Register("", suratDiterima{}); err == nil {
		t.Error("nama event kosong harus ditolak")
	}
	if err := r.Register(eventSuratDiterima, nil); err == nil {
		t.Error("schema nil harus ditolak")
	}
}

func TestSchemaRegistry_ValidateEventTakTerdaftar(t *testing.T) {
	r := eventbus.NewSchemaRegistry()
	if err := r.Validate(eventSuratDiterima, suratDiterima{}); err == nil {
		t.Error("validate event tak terdaftar harus gagal")
	}
}
