package db

import (
	"context"

	"github.com/huda-salam/pamong/port"
	"github.com/jackc/pgx/v5"
)

// Tx membungkus pgx.Tx agar memenuhi port.DBConn sekaligus menyediakan kontrol
// commit/rollback. Dipakai migration runner agar tiap migrasi atomik.
type Tx struct {
	tx pgx.Tx
}

var _ port.DBConn = (*Tx)(nil)

func (t *Tx) QueryRow(ctx context.Context, sql string, args ...any) port.Row {
	return pgxRow{t.tx.QueryRow(ctx, sql, args...)}
}

func (t *Tx) Query(ctx context.Context, sql string, args ...any) (port.Rows, error) {
	rows, err := t.tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgxRows{rows}, nil
}

func (t *Tx) Exec(ctx context.Context, sql string, args ...any) (port.CommandTag, error) {
	tag, err := t.tx.Exec(ctx, sql, args...)
	return pgxCommandTag{tag}, err
}

func (t *Tx) Commit(ctx context.Context) error   { return t.tx.Commit(ctx) }
func (t *Tx) Rollback(ctx context.Context) error { return t.tx.Rollback(ctx) }

// Begin memulai transaksi baru pada pool.
func (p *Pool) Begin(ctx context.Context) (*Tx, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &Tx{tx: tx}, nil
}
