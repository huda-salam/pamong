package db

import (
	"context"
	"sync"

	"github.com/huda-salam/pamong/core/config"
	"github.com/huda-salam/pamong/core/domain"
	"github.com/huda-salam/pamong/port"
)

// TenantConnManager merutekan koneksi DB sesuai data residency entity (ADR-005) dan
// model koneksi multi-tenant (ADR-004). Inilah routing yang melengkapi tenant resolver
// gateway (PRD gateway F5) yang ditunda dari PR-2.2.2.
//
//   - Entity ResidencyCentral → satu central pool (CentralDBResolved; default jatuh ke
//     identity DB selama gov_central belum dipisah).
//   - Entity ResidencyTenant  → pool tenant: lokasi (host, dbname) dari id.tenant_registry
//     digabung kredensial+pool BERSAMA dari config (GOV_DB_*). Pool di-cache per
//     (host, dbname) — banyak tenant pada satu host Tier 1 berbagi parameter, beda DB.
//
// Lookup registry lewat port.TenantResolver agar manager tidak import identity/.
//
// Model penguncian (lihat juga infra/db/CLAUDE.md): mutex manajer (mu) hanya dipegang
// SINGKAT untuk mengakses map entry, TIDAK selama membuka pool. Pembukaan pool (dial
// jaringan) berlangsung di bawah lock per-entry — sehingga pembuatan pool untuk key
// BERBEDA berjalan paralel (tidak ada head-of-line blocking antar tenant saat cold start),
// sementara key SAMA tetap diserialisasi (hanya satu pool yang dibuat).
type TenantConnManager struct {
	resolver port.TenantResolver
	shared   config.DBConfig        // kredensial+pool bersama + host default Tier 1
	central  config.CentralDBConfig // koneksi central ter-resolve (ADR-005)
	open     opener                 // injectable untuk test; default openPool

	mu            sync.Mutex            // melindungi map entry & centralEntry; dipegang singkat
	tenantEntries map[string]*poolEntry // key: host|dbname
	centralEntry  *poolEntry
}

// poolEntry adalah slot cache untuk satu pool. Lock per-entry memungkinkan pembukaan
// pool key berbeda berjalan paralel tanpa saling memblokir.
type poolEntry struct {
	mu   sync.Mutex
	pool *Pool
}

// opener membuka satu pool dari connParams. Default openPool memakai newPool nyata;
// test menyuntik fake agar routing & caching teruji tanpa Postgres.
type opener func(ctx context.Context, p connParams) (*Pool, error)

func openPool(ctx context.Context, p connParams) (*Pool, error) { return newPool(ctx, p) }

// NewTenantConnManager membuat manager. `shared` = config.DBConfig (kredensial bersama);
// `central` = AppConfig.CentralDBResolved().
func NewTenantConnManager(resolver port.TenantResolver, shared config.DBConfig, central config.CentralDBConfig) *TenantConnManager {
	return &TenantConnManager{
		resolver:      resolver,
		shared:        shared,
		central:       central,
		open:          openPool,
		tenantEntries: make(map[string]*poolEntry),
	}
}

// For mengembalikan pool yang tepat untuk entity pada tenant tertentu, berdasarkan
// residency-nya (ADR-005). Entity central mengabaikan tenantID.
func (m *TenantConnManager) For(ctx context.Context, tenantID string, e domain.EntityDef) (*Pool, error) {
	if e.IsCentral() {
		return m.Central(ctx)
	}
	return m.Tenant(ctx, tenantID)
}

// Tenant mengembalikan (get-or-create) pool ke DB milik tenant. Lokasi DB dari registry;
// kredensial bersama dari config. Pool di-cache per (host, dbname).
func (m *TenantConnManager) Tenant(ctx context.Context, tenantID string) (*Pool, error) {
	info, err := m.resolver.Resolve(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	host := info.DBHost
	if host == "" {
		host = m.shared.Host // fallback ke host default Tier 1 (shared)
	}
	key := host + "|" + info.DBName

	return m.entry(key).get(ctx, m.open, connParams{
		Host: host, Port: m.shared.Port, Name: info.DBName,
		User: m.shared.User, Password: m.shared.Password,
		PoolMax: m.shared.PoolMax, PoolIdle: m.shared.PoolIdle,
	})
}

// Central mengembalikan (get-or-create) pool tunggal ke DB sentral data master/referensi.
func (m *TenantConnManager) Central(ctx context.Context) (*Pool, error) {
	return m.centralPoolEntry().get(ctx, m.open, connParams{
		Host: m.central.Host, Port: m.central.Port, Name: m.central.Name,
		User: m.central.User, Password: m.central.Password,
		PoolMax: m.central.PoolMax, PoolIdle: m.central.PoolIdle,
	})
}

// entry mengembalikan poolEntry untuk key tenant, membuatnya bila belum ada. mu dipegang
// singkat (hanya akses map), tidak selama open().
func (m *TenantConnManager) entry(key string) *poolEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.tenantEntries[key]
	if e == nil {
		e = &poolEntry{}
		m.tenantEntries[key] = e
	}
	return e
}

// centralPoolEntry mengembalikan entry central, membuatnya bila belum ada.
func (m *TenantConnManager) centralPoolEntry() *poolEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.centralEntry == nil {
		m.centralEntry = &poolEntry{}
	}
	return m.centralEntry
}

// get mengembalikan pool ter-cache pada entry atau membukanya sekali. Open dilakukan di
// bawah lock per-entry: key berbeda (entry berbeda) paralel, key sama diserialisasi.
// Kegagalan TIDAK di-cache — entry dibiarkan kosong agar pemanggilan berikutnya mencoba
// ulang (mis. DB sempat tak terjangkau saat first use). Entry kosong tetap di map (jumlah
// terbatas oleh jumlah tenant), jadi tidak pernah ada dua pool dibuat untuk key sama.
func (e *poolEntry) get(ctx context.Context, open opener, p connParams) (*Pool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.pool != nil {
		return e.pool, nil
	}
	pool, err := open(ctx, p)
	if err != nil {
		return nil, err
	}
	e.pool = pool
	return pool, nil
}

// Close menutup seluruh pool yang pernah dibuka (tenant & central). Ordering kunci
// konsisten (mu → entry.mu) sehingga tidak deadlock dengan get() yang hanya memegang
// entry.mu; Close menunggu pembuatan pool yang sedang berjalan selesai.
func (m *TenantConnManager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, e := range m.tenantEntries {
		e.mu.Lock()
		if e.pool != nil {
			e.pool.Close()
			e.pool = nil
		}
		e.mu.Unlock()
		delete(m.tenantEntries, k)
	}
	if m.centralEntry != nil {
		m.centralEntry.mu.Lock()
		if m.centralEntry.pool != nil {
			m.centralEntry.pool.Close()
			m.centralEntry.pool = nil
		}
		m.centralEntry.mu.Unlock()
		m.centralEntry = nil
	}
}
