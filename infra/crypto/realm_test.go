package crypto

import (
	"context"
	"errors"
	"testing"
)

// stubCustody mencatat apakah ia pernah ditanya. Realm sentral WAJIB dijawab tanpa
// menyentuhnya sama sekali (ADR-017 §3) — bukan sekadar "kebetulan menghasilkan platform".
type stubCustody struct {
	called bool
	ret    Custody
	err    error
}

func (s *stubCustody) Custody(context.Context, string) (Custody, error) {
	s.called = true
	return s.ret, s.err
}

func TestWithCentralRealm_RealmSentralTakMenyentuhRegistry(t *testing.T) {
	// Resolver di baliknya SELALU gagal — meniru DBCustodyResolver yang fail-closed untuk
	// identitas yang tak ada di id.tenant_registry, yaitu persis kondisi realm sentral.
	inner := &stubCustody{err: errors.New("tenant tidak ada di registry")}
	r := WithCentralRealm(inner)

	got, err := r.Custody(context.Background(), RealmCentral)
	if err != nil {
		t.Fatalf("realm sentral harus punya custody tanpa registry: %v", err)
	}
	if got != CustodyPlatform {
		t.Fatalf("custody realm sentral wajib %q, dapat %q", CustodyPlatform, got)
	}
	if inner.called {
		t.Fatal("realm sentral tak boleh menanyakan custody ke registry")
	}
}

func TestWithCentralRealm_TenantTetapLewatResolverDiBaliknya(t *testing.T) {
	inner := &stubCustody{ret: CustodyTenant}
	r := WithCentralRealm(inner)

	got, err := r.Custody(context.Background(), "pemkot-surabaya")
	if err != nil {
		t.Fatalf("custody tenant: %v", err)
	}
	if !inner.called {
		t.Fatal("custody tenant WAJIB tetap dibaca dari registry (kebijakan per-tenant, ADR-010 §3)")
	}
	if got != CustodyTenant {
		t.Fatalf("jawaban resolver harus diteruskan apa adanya, dapat %q", got)
	}
}

// TestWithCentralRealm_ErrorTenantDiteruskan menjaga fail-closed tetap fail-closed: dekorator
// ini hanya menambah satu jalur, bukan melunakkan yang lain.
func TestWithCentralRealm_ErrorTenantDiteruskan(t *testing.T) {
	sentinel := errors.New("registry down")
	r := WithCentralRealm(&stubCustody{err: sentinel})

	if _, err := r.Custody(context.Background(), "pemkot-malang"); !errors.Is(err, sentinel) {
		t.Fatalf("error resolver harus diteruskan, dapat: %v", err)
	}
}

func TestIsCentralRealm(t *testing.T) {
	for realm, want := range map[string]bool{
		RealmCentral:      true,
		"central":         false, // nama tenant yang SAH — justru itu sebabnya ia ditolak
		"_Central":        false,
		"pemkot-surabaya": false,
		"":                false,
	} {
		if got := IsCentralRealm(realm); got != want {
			t.Errorf("IsCentralRealm(%q) = %v, mau %v", realm, got, want)
		}
	}
}
