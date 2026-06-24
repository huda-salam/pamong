package db

import (
	"context"

	"github.com/huda-salam/pamong/port"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool membungkus *pgxpool.Pool agar memenuhi port.DBConn.
// pgxpool.Pool mengembalikan tipe konkret (pgx.Row, pgconn.CommandTag) yang tidak
// secara otomatis memenuhi interface port — wrapper ini menjembataninya.
type Pool struct {
	pool *pgxpool.Pool
}

// NewPool membuat Pool dari *pgxpool.Pool yang sudah terhubung.
func NewPool(p *pgxpool.Pool) *Pool { return &Pool{pool: p} }

var _ port.DBConn = (*Pool)(nil)

func (p *Pool) QueryRow(ctx context.Context, sql string, args ...any) port.Row {
	return pgxRow{p.pool.QueryRow(ctx, sql, args...)}
}

func (p *Pool) Query(ctx context.Context, sql string, args ...any) (port.Rows, error) {
	rows, err := p.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgxRows{rows}, nil
}

func (p *Pool) Exec(ctx context.Context, sql string, args ...any) (port.CommandTag, error) {
	tag, err := p.pool.Exec(ctx, sql, args...)
	return pgxCommandTag{tag}, err
}

// pgxRow membungkus pgx.Row agar memenuhi port.Row.
type pgxRow struct{ r pgx.Row }

func (r pgxRow) Scan(dest ...any) error { return r.r.Scan(dest...) }

// pgxRows membungkus pgx.Rows agar memenuhi port.Rows.
type pgxRows struct{ r pgx.Rows }

func (r pgxRows) Next() bool             { return r.r.Next() }
func (r pgxRows) Scan(dest ...any) error { return r.r.Scan(dest...) }
func (r pgxRows) Err() error             { return r.r.Err() }
func (r pgxRows) Close()                 { r.r.Close() }

// pgxCommandTag membungkus pgconn.CommandTag agar memenuhi port.CommandTag.
type pgxCommandTag struct{ t pgconn.CommandTag }

func (t pgxCommandTag) RowsAffected() int64 { return t.t.RowsAffected() }
