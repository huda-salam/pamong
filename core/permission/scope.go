package permission

import (
	"context"

	"github.com/google/uuid"
)

// ResourceScope mendeskripsikan data yang menjadi sasaran pengecekan permission data-level
// (ABAC, PR-2.3.5). MVP: hanya unit kerja pemilik resource. Sengaja struct (bukan UUID
// telanjang) agar atribut scope lain — DEFERRED(Phase-3.x): tahun anggaran/periode — bisa
// ditambah additive tanpa mengubah signature evaluator.
type ResourceScope struct {
	UnitKerjaID uuid.UUID
}

// Grant adalah satu permission yang EFEKTIF dipegang actor beserta JANGKAUAN datanya. Dibangun
// dari assignment role tenant, role sentral (TenantWide), atau delegasi aktif. Berbeda dari
// permission.Role (RBAC tanpa scope): Grant menambah dimensi "data mana".
//
//   - TenantWide=true  → menjangkau seluruh tenant (role sentral global/scoped, atau assignment
//     role tenant tanpa unit). UnitKerjaID/Subtree diabaikan.
//   - TenantWide=false → terikat UnitKerjaID. Subtree=true menambah seluruh keturunan unit itu
//     pada hierarki OPD (lihat Hierarchy).
type Grant struct {
	Permission  Permission
	TenantWide  bool
	UnitKerjaID uuid.UUID // bermakna saat !TenantWide
	Subtree     bool      // saat !TenantWide: jangkau keturunan UnitKerjaID
}

// Authority adalah seluruh kewenangan efektif actor di SATU tenant pada satu titik waktu.
// Dibangun oleh middleware auth (2.4) dari resolver central+tenant+delegasi; di PR-2.3.5
// dibangun langsung oleh test (test berperan sebagai middleware).
//
// Tiga komponen sengaja dipisah karena perannya beda di evaluasi data-level:
//   - RoleNames      → masukan Tahap 1 (RBAC) lewat Engine.Allows; mempertahankan resolusi
//     strict-intersection & global-precedence 2.3.3 (butuh SEMUA nama role, termasuk yang TAK
//     memberi perm, untuk hitung intersection).
//   - RoleGrants     → jangkauan unit dari assignment role (Tahap 2). Union antar grant.
//   - DelegatedGrants→ pelimpahan eksplisit dari delegasi aktif. Mandiri: tak tunduk pada
//     strict-intersection role (delegator sudah berwenang saat melimpahkan), sehingga delegatee
//     bisa memegang perm yang tak ada di role-nya sendiri.
type Authority struct {
	RoleNames       []string
	RoleGrants      []Grant
	DelegatedGrants []Grant
}

// Hierarchy menjawab keanggotaan subtree OPD dalam satu tenant. Port — diimplementasi di
// adapter tenant DB (tenantrole/adapter/db.OrgUnitHierarchy). Domain core tak tahu Postgres.
type Hierarchy interface {
	// IsWithin melaporkan apakah unit sama dengan root atau keturunan root pada tree OPD.
	IsWithin(ctx context.Context, root, unit uuid.UUID) (bool, error)
}
