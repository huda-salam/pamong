package main

import (
	"context"
	"sync"

	"github.com/google/uuid"

	"github.com/huda-salam/pamong/core/permission"
	delegationdb "github.com/huda-salam/pamong/delegation/adapter/db"
	"github.com/huda-salam/pamong/infra/db"
	"github.com/huda-salam/pamong/port"
	tenantroledb "github.com/huda-salam/pamong/tenantrole/adapter/db"
)

// scopedDeps adalah bahan data-level (ABAC) untuk SATU tenant: jangkauan unit dari assignment
// role, dari delegasi aktif, dan tree OPD untuk menjawab subtree. Ketiganya hidup di tenant DB —
// isolasi per-tenant bersifat struktural (tak ada parameter tenantID di kontraknya).
type scopedDeps struct {
	roleGrants permission.GrantResolver
	delegated  permission.GrantResolver
	tree       permission.Hierarchy
}

// scopedDepsBuilder membangun bahan itu untuk satu tenant. Disuntik agar factory tak terikat
// TenantConnManager konkret — jalur Authority dapat diuji tanpa DB.
type scopedDepsBuilder func(ctx context.Context, tenantID string) (scopedDeps, error)

// newScopedDepsBuilder merakit builder produksi di atas pool tenant. Repo delegasi dipakai
// TANPA dekorator audit: ini jalur BACA per-request, dan audit hanya mencatat mutasi (ADR-003).
//
// Bahan di-CACHE per tenant, dan itu bukan mikro-optimasi: tiap adapter memegang memo
// "skema sudah dipastikan" di INSTANCE-nya (db.SchemaMemo — sengaja bukan variabel paket, agar
// test yang menghapus tabelnya tetap jujur). Membangun instance baru tiap request berarti memo
// selalu kosong, dan setiap pemeriksaan wewenang membayar ulang 3 blok DDL ensure-on-write —
// tepat di jalur yang file ini dibuat untuk membuatnya murah. Menahan instance-nya hidup
// memindahkan biaya itu ke pemeriksaan PERTAMA per tenant per proses.
//
// Isinya stateless selain memo (pool datang dari TenantConnManager yang punya cache sendiri),
// jadi berbagi antar-request aman; besarnya terbatas jumlah tenant yang benar-benar dilayani.
func newScopedDepsBuilder(connMgr *db.TenantConnManager) scopedDepsBuilder {
	var mu sync.Mutex
	cache := make(map[string]scopedDeps)

	return func(ctx context.Context, tenantID string) (scopedDeps, error) {
		mu.Lock()
		defer mu.Unlock()
		if deps, ok := cache[tenantID]; ok {
			return deps, nil
		}
		// connMgr.Tenant di bawah lock: ia bisa membuka pool baru (lazy), tapi hanya sekali per
		// tenant — menyerialisasi kejadian sekali-per-tenant lebih murah daripada menerima dua
		// instance dengan dua memo terpisah, yang justru mengulang DDL yang mau dihindari.
		pool, err := connMgr.Tenant(ctx, tenantID)
		if err != nil {
			return scopedDeps{}, err
		}
		deps := scopedDeps{
			roleGrants: tenantroledb.NewTenantScopedGrantResolver(pool),
			delegated:  delegationdb.NewDelegationScopedGrantResolver(delegationdb.NewDelegationRepo(pool)),
			tree:       tenantroledb.NewOrgUnitHierarchy(pool),
		}
		cache[tenantID] = deps
		return deps, nil
	}
}

// lazyScopedEvaluator adalah port.ScopedEvaluator yang menunda perakitan Authority sampai
// pengecekan data-level PERTAMA pada request ini.
//
// Kenapa lazy, bukan dirakit di middleware bersama evaluator RBAC: Authority menuntut DUA query
// tenant DB (assignment role + delegasi aktif), sementara mayoritas request tak pernah memanggil
// RequirePermissionInUnit sama sekali (permission tanpa dimensi unit). Merakit eager berarti
// membebani SETIAP request untuk kemampuan yang hanya dipakai sebagian — dan biaya itu jatuh
// tepat di jalur terpanas aplikasi. Kontrak `AllowsInUnit` sudah mengembalikan error, jadi
// menunda tak menambah bentuk kegagalan baru.
//
// Hasil (termasuk ERROR) di-memo untuk satu request: dua pengecekan dalam satu handler tak boleh
// menghasilkan dua pasang query, dan tak boleh berbeda jawabannya di tengah request. sync.Once
// dipakai karena handler boleh melakukan fan-out ke goroutine.
type lazyScopedEvaluator struct {
	build  func(ctx context.Context) (port.ScopedEvaluator, error)
	once   sync.Once
	inner  port.ScopedEvaluator
	buildE error
}

var _ port.ScopedEvaluator = (*lazyScopedEvaluator)(nil)

// resolve merakit (sekali) lalu mengembalikan evaluator sesungguhnya. Fail-closed: tanpa Authority
// tak ada dasar mengizinkan, dan yang dikembalikan ERROR (bukan false) supaya pemanggil membedakan
// "tidak berwenang" dari "tak bisa memastikan".
func (l *lazyScopedEvaluator) resolve(ctx context.Context) (port.ScopedEvaluator, error) {
	l.once.Do(func() { l.inner, l.buildE = l.build(ctx) })
	return l.inner, l.buildE
}

func (l *lazyScopedEvaluator) AllowsInUnit(ctx context.Context, perm string, unitID uuid.UUID) (bool, error) {
	inner, err := l.resolve(ctx)
	if err != nil {
		return false, err
	}
	return inner.AllowsInUnit(ctx, perm, unitID)
}

func (l *lazyScopedEvaluator) AllowsSubtree(ctx context.Context, perm string, unitID uuid.UUID) (bool, error) {
	inner, err := l.resolve(ctx)
	if err != nil {
		return false, err
	}
	return inner.AllowsSubtree(ctx, perm, unitID)
}

// buildScoped mengembalikan evaluator data-level untuk request ini.
//
// Tanpa tenant (citizen, atau token sementara sebelum pemilihan tenant) evaluator TETAP dipasang,
// terikat Authority KOSONG — jadi setiap pengecekan unit menjawab TIDAK. Sengaja bukan nil:
// gateway.Context memaknai nil sebagai permisif, dan permisif di sini berarti seluruh scope ABAC
// terbuka untuk konteks yang justru paling sedikit terverifikasi. Grant sentral pun tak diemisikan
// di jalur ini — `TenantWide` tak punya arti bila tenant-nya belum ditentukan.
func (f *evaluatorFactory) buildScoped(engine *permission.Engine, cat permission.RefCatalog, claims *port.Claims) port.ScopedEvaluator {
	if claims.TenantID == "" || f.scopedDeps == nil {
		return permission.NewScopedEngine(engine, denyAllHierarchy{}).Bind(permission.Authority{})
	}
	tenantID, userID, refs := claims.TenantID, claims.PersonID, roleRefsOf(claims)
	return &lazyScopedEvaluator{
		build: func(ctx context.Context) (port.ScopedEvaluator, error) {
			deps, err := f.scopedDeps(ctx, tenantID)
			if err != nil {
				return nil, err
			}
			auth, err := permission.BuildAuthority(ctx, cat, deps.roleGrants, deps.delegated, userID, refs)
			if err != nil {
				return nil, err
			}
			return permission.NewScopedEngine(engine, deps.tree).Bind(auth), nil
		},
	}
}

// roleRefsOf memetakan klaim role menjadi RoleRef ber-LAPIS ASAL. Duplikasi kecil dari
// gateway.NewContextFromClaims disengaja: yang dibagi adalah ATURAN-nya (nama tenant bukan nama
// sentral, ADR-019/B8), dan menyalurkan Context ke factory hanya untuk ini akan membalik arah
// dependensi composition root → gateway.
func roleRefsOf(claims *port.Claims) []permission.RoleRef {
	refs := make([]permission.RoleRef, 0, len(claims.TenantRoles)+len(claims.CentralRoles))
	for _, r := range claims.TenantRoles {
		refs = append(refs, permission.TenantRef(r))
	}
	for _, r := range claims.CentralRoles {
		refs = append(refs, permission.CentralRef(r))
	}
	return refs
}

// denyAllHierarchy dipakai pada konteks tanpa tenant: tak ada tree OPD untuk ditanya. Ia tak
// pernah terpanggil (Authority kosong berarti covers() pulang sebelum menyentuh tree), tapi
// dipasang eksplisit agar tak ada jalan di mana nil Hierarchy menjadi nil-deref bila kelak ada
// grant yang lolos ke sini.
type denyAllHierarchy struct{}

func (denyAllHierarchy) IsWithin(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return false, nil
}
