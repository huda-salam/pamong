package main

import (
	"context"
	"sync"
	"time"

	"github.com/huda-salam/pamong/core/permission"
	"github.com/huda-salam/pamong/infra/db"
	"github.com/huda-salam/pamong/port"
	tenantroledb "github.com/huda-salam/pamong/tenantrole/adapter/db"
)

// evaluatorFactory adalah implementasi middleware.EvaluatorFactory di composition root:
// membangun port.PermissionEvaluator per-request dari Claims terverifikasi. Ia SENGAJA hidup
// di cmd/server, bukan di gateway/middleware — agar middleware tetap tak bergantung pada
// adapter konkret (core/permission, identity/adapter, tenantrole/adapter); composition root
// yang merakit concrete-nya dan menyuntik lewat interface middleware.EvaluatorFactory.
//
// Lapisan:
//   - central = snapshot proses (identity DB) — dibangun sekali saat boot, dishare semua tenant.
//   - tenant  = snapshot per-tenant (tenant DB) — dibangun lazy per tenant_id, lalu di-cache.
//
// Digabung CompositeCatalog (resolusi PER LAPIS ASAL sejak ADR-019: nama dari klaim tenant_roles
// dicari hanya di katalog tenant, dari central_roles hanya di katalog central) lalu
// dibungkus permission.Engine. Peran yang dipegang actor sudah di-resolve saat login dan dibawa
// token (Claims.CentralRoles/TenantRoles); catalog hanya memetakan NAMA role → (layer, grants).
// Karena definisi role jarang berubah (deploy / aksi admin), snapshot konsisten dengan pola
// CentralRoleCatalog/TenantRoleCatalog.
//
// Strict-permission set (PR-5.1.2c): dikumpulkan dari manifest modul saat boot
// (Registry.StrictPermissions, ADR-014) dan disuntik ke setiap Engine yang dibangun,
// sehingga resolusi INTERSECTION SoD berlaku untuk permission yang ditandai strict.
//
// DEFERRED(Phase-5.x — event-driven invalidation):
//   - Refresh-on-change catalog tenant kini TTL-based (lihat expired): entri cache
//     kedaluwarsa setelah TTL lalu dibangun ulang dari tenant DB. Cakupannya SPESIFIK:
//     catalog memetakan NAMA role → (layer, permission-set), jadi TTL hanya menyegarkan
//     perubahan DEFINISI role tenant (permission sebuah role diubah admin). Ia TIDAK
//     menyentuh: (a) role apa yang dipegang seorang user, dan (b) penonaktifan user —
//     keduanya hidup di klaim token (di-resolve saat login) dan berlaku lewat masa token
//     / revocation jti, BUKAN lewat refresh catalog ini. Invalidasi seketika untuk
//     perubahan definisi role menunggu event tenant-role-changed yang belum ada di repo
//     — ditunda sebagai PR tersendiri di tenantrole/.
type evaluatorFactory struct {
	central permission.RoleCatalog
	build   tenantCatalogBuilder
	strict  []permission.Permission
	ttl     time.Duration // 0 = tak pernah kedaluwarsa (cache selama umur proses)
	now     func() time.Time

	mu    sync.RWMutex
	cache map[string]tenantCatalogEntry // tenant_id → catalog tenant (snapshot + waktu build)
}

// tenantCatalogEntry membungkus snapshot catalog tenant dengan waktu build-nya untuk
// penegakan TTL.
type tenantCatalogEntry struct {
	catalog permission.RoleCatalog
	builtAt time.Time
}

// tenantCatalogBuilder membangun catalog role untuk satu tenant. Disuntik agar factory tak
// terikat langsung ke TenantConnManager konkret — jalur composite+cache dapat diuji tanpa DB.
type tenantCatalogBuilder func(ctx context.Context, tenantID string) (permission.RoleCatalog, error)

// newEvaluatorFactory merakit factory produksi: catalog tenant dibangun dari pool tenant
// (TenantConnManager) → TenantRoleRepo → TenantRoleCatalog (snapshot). strict = permission
// SoD dari manifest (Registry.StrictPermissions); ttl = umur cache catalog tenant (0 = tak
// pernah kedaluwarsa).
func newEvaluatorFactory(central permission.RoleCatalog, connMgr *db.TenantConnManager, strict []permission.Permission, ttl time.Duration) *evaluatorFactory {
	return newEvaluatorFactoryWith(central, func(ctx context.Context, tenantID string) (permission.RoleCatalog, error) {
		pool, err := connMgr.Tenant(ctx, tenantID)
		if err != nil {
			return nil, err
		}
		return tenantroledb.NewTenantRoleCatalog(ctx, tenantroledb.NewTenantRoleRepo(pool))
	}, strict, ttl)
}

// newEvaluatorFactoryWith merakit factory dengan builder catalog tenant kustom (dipakai test).
func newEvaluatorFactoryWith(central permission.RoleCatalog, build tenantCatalogBuilder, strict []permission.Permission, ttl time.Duration) *evaluatorFactory {
	return &evaluatorFactory{
		central: central,
		build:   build,
		strict:  strict,
		ttl:     ttl,
		now:     time.Now,
		cache:   make(map[string]tenantCatalogEntry),
	}
}

// Build mengembalikan evaluator RBAC untuk request ini. Citizen (TenantID kosong) mendapat
// Engine central-only (tak ada lapis tenant untuknya); employee mendapat Engine di atas
// composite central+tenant.
func (f *evaluatorFactory) Build(ctx context.Context, claims *port.Claims) (port.PermissionEvaluator, error) {
	if claims.TenantID == "" {
		// Citizen: hanya lapis central. Composite dengan tenant nil — BUKAN katalog central
		// telanjang — supaya ref ber-origin tenant (yang tak semestinya ada di token citizen)
		// tetap tak ditemukan alih-alih ikut dicari di katalog central (ADR-019).
		return permission.NewEngine(permission.NewCompositeCatalog(f.central, nil), f.strict...), nil
	}
	tenantCat, err := f.tenantCatalog(ctx, claims.TenantID)
	if err != nil {
		return nil, err
	}
	return permission.NewEngine(permission.NewCompositeCatalog(f.central, tenantCat), f.strict...), nil
}

// tenantCatalog mengembalikan (get-or-build) catalog role tenant. Build dilakukan DI LUAR lock
// agar cold-start satu tenant tak memblokir tenant lain; double-check saat menyimpan menjaga
// satu entri per tenant (build ganda yang jarang untuk tenant sama tetap benar — keduanya
// snapshot sah). Kegagalan (mis. DB tenant tak terjangkau) TIDAK di-cache: percobaan berikut
// mencoba ulang.
//
// Entri kedaluwarsa setelah f.ttl (bila > 0): catalog dibangun ulang dari tenant DB sehingga
// perubahan definisi role tenant terlihat dalam jeda ≤ TTL tanpa restart. TTL 0 = cache selama
// umur proses (perilaku lama). Lihat catatan DEFERRED event-driven di doc tipe.
func (f *evaluatorFactory) tenantCatalog(ctx context.Context, tenantID string) (permission.RoleCatalog, error) {
	f.mu.RLock()
	entry, ok := f.cache[tenantID]
	f.mu.RUnlock()
	if ok && !f.expired(entry) {
		return entry.catalog, nil
	}

	built, err := f.build(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	f.mu.Lock()
	// Double-check: entri segar yang ditulis goroutine lain saat kita membangun tetap dipakai
	// (hindari menimpa snapshot yang lebih baru); entri kedaluwarsa/absen diganti hasil build.
	if existing, ok := f.cache[tenantID]; ok && !f.expired(existing) {
		f.mu.Unlock()
		return existing.catalog, nil
	}
	f.cache[tenantID] = tenantCatalogEntry{catalog: built, builtAt: f.now()}
	f.mu.Unlock()
	return built, nil
}

// expired melaporkan apakah entri cache sudah melewati TTL. TTL 0 (atau negatif) berarti tak
// pernah kedaluwarsa.
func (f *evaluatorFactory) expired(e tenantCatalogEntry) bool {
	if f.ttl <= 0 {
		return false
	}
	return f.now().Sub(e.builtAt) >= f.ttl
}
