package db

import (
	"context"
	"testing"
)

// TenantRoutingConn tanpa tenant di context harus GAGAL sebelum menyentuh TenantConnManager
// (mgr nil membuktikannya tak dipanggil) — cegah query jatuh ke DB yang salah. Jalur
// with-tenant (butuh registry + Postgres) diuji di integration.

func TestTenantRoutingConn_Query_TanpaTenant_Error(t *testing.T) {
	c := NewTenantRoutingConn(nil) // mgr nil: tak boleh sampai dipanggil
	if _, err := c.Query(context.Background(), "SELECT 1"); err == nil {
		t.Fatal("Query tanpa tenant di context harus error")
	}
}

func TestTenantRoutingConn_Exec_TanpaTenant_Error(t *testing.T) {
	c := NewTenantRoutingConn(nil)
	if _, err := c.Exec(context.Background(), "DELETE FROM x"); err == nil {
		t.Fatal("Exec tanpa tenant di context harus error")
	}
}

func TestTenantRoutingConn_QueryRow_TanpaTenant_ErrorSaatScan(t *testing.T) {
	c := NewTenantRoutingConn(nil)
	row := c.QueryRow(context.Background(), "SELECT 1")
	var v int
	if err := row.Scan(&v); err == nil {
		t.Fatal("QueryRow tanpa tenant: Scan harus mengembalikan error routing")
	}
}
