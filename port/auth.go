package port

import (
	"context"

	"github.com/google/uuid"
)

// AuthContext membawa identitas actor, tenant, dan context pembatalan.
// Use case menerima AuthContext sebagai pengganti context.Context biasa;
// ia memenuhi context.Context sehingga bisa diteruskan ke port lain (repo, seq, dsb).
type AuthContext interface {
	context.Context

	PersonID() uuid.UUID
	Persona() string
	EmploymentStatus() string
	TenantID() string
	HasRole(role string) bool
	HasCentralRole(role string) bool
	RequirePermission(perm string) error
	// RequirePermissionInUnit menegakkan permission DATA-LEVEL (ABAC, PR-2.3.5): selain
	// "punya perm", resource harus berada dalam jangkauan unit kerja actor (unit/subtree/
	// tenant-wide, termasuk lewat delegasi aktif). Dipakai handler yang melayani resource
	// milik unit tertentu; unitID = unit pemilik resource.
	RequirePermissionInUnit(perm string, unitID uuid.UUID) error
	// RequirePermissionInSubtree menegakkan wewenang atas unitID BESERTA SELURUH KETURUNANNYA
	// (ADR-021). Dipakai saat actor memberikan jangkauan subtree kepada orang lain — memberi
	// `include_subtree` atas sebuah unit berarti memberi jangkauan atas turunannya, jadi ia
	// menuntut wewenang yang menjangkau turunan itu, bukan wewenang atas unit itu saja.
	RequirePermissionInSubtree(perm string, unitID uuid.UUID) error
	IsCitizen() bool
	IsCrossTenant() bool
}
