package db

import (
	"context"
	"fmt"
	"time"

	"github.com/huda-salam/pamong/core/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// connParams adalah parameter koneksi yang sudah diratakan dari berbagai bentuk config
// (DBConfig/IdentityDBConfig/CentralDBConfig, atau hasil gabung registry+kredensial
// bersama). Builder pool tunggal bekerja di atas ini agar semua jalur koneksi konsisten.
type connParams struct {
	Host     string
	Port     int
	Name     string
	User     string
	Password string
	PoolMax  int
	PoolIdle int
}

// New membangun connection pool Postgres dari konfigurasi tenant DB (default/shared).
func New(ctx context.Context, cfg config.DBConfig) (*Pool, error) {
	return newPool(ctx, connParams{
		Host: cfg.Host, Port: cfg.Port, Name: cfg.Name, User: cfg.User,
		Password: cfg.Password, PoolMax: cfg.PoolMax, PoolIdle: cfg.PoolIdle,
	})
}

// NewIdentity membangun pool ke identity DB sentral (koneksi penuh dari config, ADR-004).
func NewIdentity(ctx context.Context, cfg config.IdentityDBConfig) (*Pool, error) {
	return newPool(ctx, connParams{
		Host: cfg.Host, Port: cfg.Port, Name: cfg.Name, User: cfg.User,
		Password: cfg.Password, PoolMax: cfg.PoolMax, PoolIdle: cfg.PoolIdle,
	})
}

// NewCentral membangun pool ke DB sentral data master/referensi (ADR-005).
func NewCentral(ctx context.Context, cfg config.CentralDBConfig) (*Pool, error) {
	return newPool(ctx, connParams{
		Host: cfg.Host, Port: cfg.Port, Name: cfg.Name, User: cfg.User,
		Password: cfg.Password, PoolMax: cfg.PoolMax, PoolIdle: cfg.PoolIdle,
	})
}

// newPool membuka pool dari connParams, menerapkan batas pool, lalu mem-ping untuk
// memastikan kredensial & jaringan valid sebelum dipakai.
func newPool(ctx context.Context, p connParams) (*Pool, error) {
	pcfg, err := pgxpool.ParseConfig(dsn(p))
	if err != nil {
		return nil, fmt.Errorf("parse konfigurasi pool: %w", err)
	}
	if p.PoolMax > 0 {
		pcfg.MaxConns = int32(p.PoolMax)
	}
	if p.PoolIdle > 0 {
		pcfg.MinConns = int32(p.PoolIdle)
	}

	pool, err := pgxpool.NewWithConfig(ctx, pcfg)
	if err != nil {
		return nil, fmt.Errorf("buka pool: %w", err)
	}

	pl := NewPool(pool)
	if err := pl.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pl, nil
}

// dsn merangkai connection string Postgres dari connParams.
func dsn(p connParams) string {
	return fmt.Sprintf(
		"host=%s port=%d dbname=%s user=%s password=%s sslmode=disable",
		p.Host, p.Port, p.Name, p.User, p.Password,
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
