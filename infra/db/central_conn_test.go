package db

import (
	"context"
	"errors"
	"testing"

	"github.com/huda-salam/pamong/core/config"
)

// CentralRoutingConn TIDAK ter-guard tenant (beda dari TenantRoutingConn yang error tanpa
// tenant di context): ia selalu menuju pool sentral. Test membuktikannya dengan opener palsu —
// query pada context TANPA tenant harus tetap mencoba membuka pool CENTRAL (bukan gagal dengan
// "tenant tak ada"). Jalur nyata (Postgres) diuji di integration.
func TestCentralRoutingConn_RouteKeCentral_TanpaTenant(t *testing.T) {
	sentinel := errors.New("central-open-dipanggil")
	mgr := NewTenantConnManager(nil, config.DBConfig{}, config.CentralDBConfig{})
	mgr.open = func(context.Context, connParams) (*Pool, error) { return nil, sentinel }
	c := NewCentralRoutingConn(mgr)

	// Query & Exec: context.Background() TANPA tenant → tetap menuju central (→ sentinel).
	if _, err := c.Query(context.Background(), "SELECT 1"); !errors.Is(err, sentinel) {
		t.Fatalf("Query central harus membuka pool central, dapat: %v", err)
	}
	if _, err := c.Exec(context.Background(), "SELECT 1"); !errors.Is(err, sentinel) {
		t.Fatalf("Exec central harus membuka pool central, dapat: %v", err)
	}
	// QueryRow menunda error ke Scan (pola pgx.Row).
	if err := c.QueryRow(context.Background(), "SELECT 1").Scan(new(int)); !errors.Is(err, sentinel) {
		t.Fatalf("QueryRow central: Scan harus mengembalikan error open central, dapat: %v", err)
	}
}
