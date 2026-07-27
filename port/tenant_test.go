package port_test

import (
	"context"
	"testing"

	"github.com/huda-salam/pamong/port"
)

func TestTenantFrom_ExplicitValue(t *testing.T) {
	ctx := port.WithTenant(context.Background(), "pemkot-surabaya")
	if got := port.TenantFrom(ctx); got != "pemkot-surabaya" {
		t.Fatalf("TenantFrom = %q, mau pemkot-surabaya", got)
	}
}

func TestTenantFrom_KosongBilaTakAda(t *testing.T) {
	if got := port.TenantFrom(context.Background()); got != "" {
		t.Fatalf("TenantFrom = %q, mau kosong", got)
	}
}

// TestTenantFrom_BertahanSaatDibungkus memastikan nilai WithTenant tetap ter-resolve setelah
// context dibungkus (WithValue/WithCancel) di rantai handler→usecase→repo — properti yang
// menjadi alasan menghapus fallback ctx.(AuthContext) yang rapuh (DEFERRED Phase-5.1.2 #2).
func TestTenantFrom_BertahanSaatDibungkus(t *testing.T) {
	base := port.WithTenant(context.Background(), "pemkot-malang")
	wrapped := context.WithValue(base, struct{ k int }{1}, "x")
	child, cancel := context.WithCancel(wrapped)
	defer cancel()
	if got := port.TenantFrom(child); got != "pemkot-malang" {
		t.Fatalf("TenantFrom setelah dibungkus = %q, mau pemkot-malang", got)
	}
}

// TestTenantFrom_TerdalamMenang memastikan WithTenant yang lebih dalam menutupi yang lebih luar
// (paling spesifik menang) — perilaku standar context.Value.
func TestTenantFrom_TerdalamMenang(t *testing.T) {
	outer := port.WithTenant(context.Background(), "luar")
	inner := port.WithTenant(outer, "dalam")
	if got := port.TenantFrom(inner); got != "dalam" {
		t.Fatalf("TenantFrom = %q, mau dalam (terdalam menang)", got)
	}
}
