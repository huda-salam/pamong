package port_test

import (
	"context"
	"testing"

	"github.com/huda-salam/pamong/port"
	"github.com/huda-salam/pamong/testkit"
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

// TestTenantFrom_FallbackAuthContext memastikan bila context adalah AuthContext (mis.
// gateway.Context yang dibawa handler), tenant diambil dari TenantID()-nya — sehingga routing
// DB tetap benar pada jalur handler meski WithTenant belum disisipkan eksplisit.
func TestTenantFrom_FallbackAuthContext(t *testing.T) {
	ctx := testkit.Ctx(t, testkit.WithTenant("pemkot-malang"))
	if got := port.TenantFrom(ctx); got != "pemkot-malang" {
		t.Fatalf("TenantFrom (fallback AuthContext) = %q, mau pemkot-malang", got)
	}
}

// TestTenantFrom_ExplicitMenangAtasFallback memastikan nilai WithTenant menang atas TenantID()
// AuthContext bila keduanya ada (WithTenant lebih spesifik/disengaja).
func TestTenantFrom_ExplicitMenangAtasFallback(t *testing.T) {
	base := testkit.Ctx(t, testkit.WithTenant("dari-authcontext"))
	ctx := port.WithTenant(base, "dari-withtenant")
	if got := port.TenantFrom(ctx); got != "dari-withtenant" {
		t.Fatalf("TenantFrom = %q, mau dari-withtenant (WithTenant menang)", got)
	}
}
