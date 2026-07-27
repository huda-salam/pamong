package db

import (
	"context"

	"github.com/huda-salam/pamong/port"
)

// CentralRoutingConn adalah port.DBConn yang SELALU merutekan query ke DB sentral (ADR-005):
// entity ber-Residency=ResidencyCentral (data master/referensi lintas-tenant) hidup di satu
// DB sentral, tidak per-tenant. Pasangan dari TenantRoutingConn (yang route per-tenant dari
// context); dipakai repo entity central lewat app.CentralDB().
//
// Ini menutup DEFERRED(Phase-5.1.2 #1): sebelumnya modul hanya menerima app.DB() ber-routing
// tenant, sehingga repo entity central diam-diam menulis/membaca DB tenant. Kini ada jalur
// eksplisit ke DB sentral tanpa mengubah port.DBConn (tak perlu membawa konteks entity di tiap
// query) — keputusan PR-5.1.2: app.CentralDB() terpisah, bukan DBConn sadar-residency.
//
// Lazy: pool sentral dibuka on-demand oleh TenantConnManager saat query pertama, jadi server
// tetap bisa boot tanpa DB sentral hidup. Tak bergantung tenant di context.
type CentralRoutingConn struct {
	mgr *TenantConnManager
}

var _ port.DBConn = (*CentralRoutingConn)(nil)

// NewCentralRoutingConn membungkus TenantConnManager sebagai DBConn yang selalu ke pool sentral.
func NewCentralRoutingConn(mgr *TenantConnManager) *CentralRoutingConn {
	return &CentralRoutingConn{mgr: mgr}
}

func (c *CentralRoutingConn) QueryRow(ctx context.Context, sql string, args ...any) port.Row {
	p, err := c.mgr.Central(ctx)
	if err != nil {
		// QueryRow tak bisa mengembalikan error langsung; tunda ke Scan (pola pgx.Row) —
		// errRow didefinisikan di routing_conn.go (paket sama).
		return errRow{err}
	}
	return p.QueryRow(ctx, sql, args...)
}

func (c *CentralRoutingConn) Query(ctx context.Context, sql string, args ...any) (port.Rows, error) {
	p, err := c.mgr.Central(ctx)
	if err != nil {
		return nil, err
	}
	return p.Query(ctx, sql, args...)
}

func (c *CentralRoutingConn) Exec(ctx context.Context, sql string, args ...any) (port.CommandTag, error) {
	p, err := c.mgr.Central(ctx)
	if err != nil {
		return nil, err
	}
	return p.Exec(ctx, sql, args...)
}
