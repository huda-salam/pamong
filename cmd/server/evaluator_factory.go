package main

import (
	"context"
	"sync"

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
// Digabung CompositeCatalog (central DIDAHULUKAN agar tenant tak men-shadow role global) lalu
// dibungkus permission.Engine. Peran yang dipegang actor sudah di-resolve saat login dan dibawa
// token (Claims.CentralRoles/TenantRoles); catalog hanya memetakan NAMA role → (layer, grants).
// Karena definisi role jarang berubah (deploy / aksi admin), snapshot konsisten dengan pola
// CentralRoleCatalog/TenantRoleCatalog.
//
// DEFERRED(Phase-5.1.2b):
//   - Refresh-on-change catalog tenant: kini di-cache selama umur proses; perubahan definisi
//     role tenant baru terlihat setelah restart (keputusan user: cache + DEFERRED refresh).
//   - Strict-permission set kosong: belum ada permission strict yang dideklarasikan manifest,
//     jadi Engine berjalan union murni + global-precedence. Wiring strict dari manifest menyusul.
type evaluatorFactory struct {
	central permission.RoleCatalog
	build   tenantCatalogBuilder

	mu    sync.RWMutex
	cache map[string]permission.RoleCatalog // tenant_id → catalog tenant (snapshot)
}

// tenantCatalogBuilder membangun catalog role untuk satu tenant. Disuntik agar factory tak
// terikat langsung ke TenantConnManager konkret — jalur composite+cache dapat diuji tanpa DB.
type tenantCatalogBuilder func(ctx context.Context, tenantID string) (permission.RoleCatalog, error)

// newEvaluatorFactory merakit factory produksi: catalog tenant dibangun dari pool tenant
// (TenantConnManager) → TenantRoleRepo → TenantRoleCatalog (snapshot).
func newEvaluatorFactory(central permission.RoleCatalog, connMgr *db.TenantConnManager) *evaluatorFactory {
	return newEvaluatorFactoryWith(central, func(ctx context.Context, tenantID string) (permission.RoleCatalog, error) {
		pool, err := connMgr.Tenant(ctx, tenantID)
		if err != nil {
			return nil, err
		}
		return tenantroledb.NewTenantRoleCatalog(ctx, tenantroledb.NewTenantRoleRepo(pool))
	})
}

// newEvaluatorFactoryWith merakit factory dengan builder catalog tenant kustom (dipakai test).
func newEvaluatorFactoryWith(central permission.RoleCatalog, build tenantCatalogBuilder) *evaluatorFactory {
	return &evaluatorFactory{
		central: central,
		build:   build,
		cache:   make(map[string]permission.RoleCatalog),
	}
}

// Build mengembalikan evaluator RBAC untuk request ini. Citizen (TenantID kosong) mendapat
// Engine central-only (tak ada lapis tenant untuknya); employee mendapat Engine di atas
// composite central+tenant.
func (f *evaluatorFactory) Build(ctx context.Context, claims *port.Claims) (port.PermissionEvaluator, error) {
	if claims.TenantID == "" {
		return permission.NewEngine(f.central), nil
	}
	tenantCat, err := f.tenantCatalog(ctx, claims.TenantID)
	if err != nil {
		return nil, err
	}
	return permission.NewEngine(permission.NewCompositeCatalog(f.central, tenantCat)), nil
}

// tenantCatalog mengembalikan (get-or-build) catalog role tenant. Build dilakukan DI LUAR lock
// agar cold-start satu tenant tak memblokir tenant lain; double-check saat menyimpan menjaga
// satu entri per tenant (build ganda yang jarang untuk tenant sama tetap benar — keduanya
// snapshot sah). Kegagalan (mis. DB tenant tak terjangkau) TIDAK di-cache: percobaan berikut
// mencoba ulang.
func (f *evaluatorFactory) tenantCatalog(ctx context.Context, tenantID string) (permission.RoleCatalog, error) {
	f.mu.RLock()
	cat, ok := f.cache[tenantID]
	f.mu.RUnlock()
	if ok {
		return cat, nil
	}

	built, err := f.build(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	f.mu.Lock()
	if existing, ok := f.cache[tenantID]; ok {
		f.mu.Unlock()
		return existing, nil
	}
	f.cache[tenantID] = built
	f.mu.Unlock()
	return built, nil
}
