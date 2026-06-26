package port

import (
	"context"

	"github.com/google/uuid"
)

// PermissionEvaluator mengevaluasi keputusan RBAC: diberi nama-nama role yang
// dipegang actor, apakah sebuah permission diberikan. Diimplementasi oleh
// core/permission.Engine. gateway.Context memakainya untuk RequirePermission
// tanpa bergantung pada implementasi konkret (core/* hanya lewat port).
type PermissionEvaluator interface {
	// Allows melaporkan apakah salah satu role memberi permission perm.
	Allows(roles []string, perm string) bool
}

// ScopedEvaluator mengevaluasi keputusan permission DATA-LEVEL (ABAC, PR-2.3.5) untuk SATU
// actor (actor-bound): apakah actor boleh melakukan perm atas resource yang dimiliki unitID.
// Diimplementasi oleh core/permission.ScopedEngine.Bind(Authority). gateway.Context memakainya
// untuk RequirePermissionInUnit tanpa bergantung pada tipe core. Berbeda dari
// PermissionEvaluator yang menjawab "punya permission?" tanpa scope.
type ScopedEvaluator interface {
	AllowsInUnit(ctx context.Context, perm string, unitID uuid.UUID) (bool, error)
}
