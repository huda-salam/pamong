package db

import (
	"context"
	"fmt"
	"time"

	"github.com/huda-salam/pamong/core/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// New membangun connection pool Postgres dari konfigurasi tenant DB.
// Pool dikonfigurasi dengan batas maksimum & minimum koneksi dari config,
// lalu di-ping untuk memastikan kredensial & jaringan valid sebelum dipakai.
func New(ctx context.Context, cfg config.DBConfig) (*Pool, error) {
	pcfg, err := pgxpool.ParseConfig(dsn(cfg))
	if err != nil {
		return nil, fmt.Errorf("parse konfigurasi pool: %w", err)
	}
	if cfg.PoolMax > 0 {
		pcfg.MaxConns = int32(cfg.PoolMax)
	}
	if cfg.PoolIdle > 0 {
		pcfg.MinConns = int32(cfg.PoolIdle)
	}

	pool, err := pgxpool.NewWithConfig(ctx, pcfg)
	if err != nil {
		return nil, fmt.Errorf("buka pool: %w", err)
	}

	p := NewPool(pool)
	if err := p.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return p, nil
}

// dsn merangkai connection string Postgres dari DBConfig.
func dsn(cfg config.DBConfig) string {
	return fmt.Sprintf(
		"host=%s port=%d dbname=%s user=%s password=%s sslmode=disable",
		cfg.Host, cfg.Port, cfg.Name, cfg.User, cfg.Password,
	)
}

// Ping memverifikasi koneksi pool dengan timeout singkat. Dipakai juga oleh
// health check gateway untuk melaporkan kesiapan database.
func (p *Pool) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return p.pool.Ping(ctx)
}

// Close menutup seluruh koneksi dalam pool.
func (p *Pool) Close() { p.pool.Close() }
