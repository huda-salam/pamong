package config_test

import (
	"testing"

	"github.com/huda-salam/pamong/core/config"
)

func TestCentralDBResolved_FallbackKeIdentity(t *testing.T) {
	cfg := config.AppConfig{
		IdentityDB: config.IdentityDBConfig{
			Host: "id-host", Port: 5432, Name: "gov_identity", User: "u", Password: "p",
			PoolMax: 10, PoolIdle: 2,
		},
		// CentralDB sengaja kosong → fallback ke identity DB (ADR-005).
	}
	got := cfg.CentralDBResolved()
	if got.Host != "id-host" || got.Name != "gov_identity" || got.User != "u" || got.PoolMax != 10 {
		t.Fatalf("central kosong harus fallback ke identity DB, dapat %+v", got)
	}
}

func TestCentralDBResolved_PakaiCentralBilaDiset(t *testing.T) {
	cfg := config.AppConfig{
		IdentityDB: config.IdentityDBConfig{Host: "id-host", Name: "gov_identity"},
		CentralDB:  config.CentralDBConfig{Host: "central-host", Name: "gov_central", User: "cu"},
	}
	got := cfg.CentralDBResolved()
	if got.Host != "central-host" || got.Name != "gov_central" || got.User != "cu" {
		t.Fatalf("central diset harus dipakai, dapat %+v", got)
	}
}
