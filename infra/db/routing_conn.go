package db

import (
	"context"
	"fmt"

	"github.com/huda-salam/pamong/port"
)

// TenantRoutingConn adalah port.DBConn yang merutekan tiap query ke DB tenant yang benar
// PER-REQUEST: tenant_id dibaca dari context (port.TenantFrom) lalu pool diambil dari
// TenantConnManager. Inilah jembatan antara "modul menerima satu app.DB()" dan realita
// DB-per-tenant (ADR-004): repo dibangun SEKALI saat Bootstrap, routing terjadi tiap query.
//
// Context tanpa tenant_id → error eksplisit (bukan diam-diam pakai DB salah). Konstruksi
// bersifat lazy: tak ada koneksi dibuka sampai query pertama, sehingga server tetap bisa boot
// tanpa Postgres hidup (koneksi dibuka on-demand oleh TenantConnManager).
//
// DEFERRED(Phase-5.1.2): routing SELALU ke pool tenant — entity ber-Residency=ResidencyCentral
// (ADR-005) TIDAK dirutekan ke DB sentral di sini. port.DBConn tak membawa konteks entity, jadi
// TenantConnManager.For(ctx, tenantID, entityDef) yang menghormati residency tak bisa dipakai
// lewat app.DB() tunggal. Aman untuk saat ini (modul yang ter-wire semua ResidencyTenant), TAPI
// repo entity central yang dibangun di atas app.DB() akan diam-diam menulis/membaca DB tenant.
// Perbaikan menyeluruh (mis. DBConn sadar-residency atau app.CentralDB() terpisah) diputuskan
// bersama tenant resolver PR-5.1.2 — lihat ROADMAP Phase-5.1.
type TenantRoutingConn struct {
	mgr *TenantConnManager
}

var (
	_ port.DBConn = (*TenantRoutingConn)(nil)
	_ TxConn      = (*TenantRoutingConn)(nil)
)

// NewTenantRoutingConn membungkus TenantConnManager sebagai DBConn ber-routing per-tenant.
func NewTenantRoutingConn(mgr *TenantConnManager) *TenantRoutingConn {
	return &TenantRoutingConn{mgr: mgr}
}

// pool memilih pool tenant untuk context ini. tenant_id kosong = tak bisa dirutekan.
func (c *TenantRoutingConn) pool(ctx context.Context) (*Pool, error) {
	tenantID := port.TenantFrom(ctx)
	if tenantID == "" {
		return nil, fmt.Errorf("db: tenant_id tak ada di context — tak bisa memilih tenant DB (butuh middleware tenant / WithTenant)")
	}
	return c.mgr.Tenant(ctx, tenantID)
}

// Begin membuka transaksi pada pool tenant untuk context ini — routing yang sama dengan query
// tunggal, hanya pada level transaksi. Ini yang membuat repo ber-transaksi (mis. TenantRoleRepo:
// role + grant permission atomik) bisa dipakai di jalur request multi-tenant.
//
// Transaksi TIDAK melintasi tenant: ia lahir dari satu pool, dan pool itu dipilih dari tenant di
// context. Tak ada jalan di sini untuk sebuah transaksi menyentuh dua DB tenant.
func (c *TenantRoutingConn) Begin(ctx context.Context) (*Tx, error) {
	p, err := c.pool(ctx)
	if err != nil {
		return nil, err
	}
	return p.Begin(ctx)
}

func (c *TenantRoutingConn) QueryRow(ctx context.Context, sql string, args ...any) port.Row {
	p, err := c.pool(ctx)
	if err != nil {
		// QueryRow tak bisa mengembalikan error langsung; tunda ke Scan (pola pgx.Row).
		return errRow{err}
	}
	return p.QueryRow(ctx, sql, args...)
}

func (c *TenantRoutingConn) Query(ctx context.Context, sql string, args ...any) (port.Rows, error) {
	p, err := c.pool(ctx)
	if err != nil {
		return nil, err
	}
	return p.Query(ctx, sql, args...)
}

func (c *TenantRoutingConn) Exec(ctx context.Context, sql string, args ...any) (port.CommandTag, error) {
	p, err := c.pool(ctx)
	if err != nil {
		return nil, err
	}
	return p.Exec(ctx, sql, args...)
}

// errRow membawa error routing agar muncul saat Scan (port.Row tak punya jalur error lain).
type errRow struct{ err error }

func (r errRow) Scan(_ ...any) error { return r.err }
