package port

// PermissionEvaluator mengevaluasi keputusan RBAC: diberi nama-nama role yang
// dipegang actor, apakah sebuah permission diberikan. Diimplementasi oleh
// core/permission.Engine. gateway.Context memakainya untuk RequirePermission
// tanpa bergantung pada implementasi konkret (core/* hanya lewat port).
type PermissionEvaluator interface {
	// Allows melaporkan apakah salah satu role memberi permission perm.
	Allows(roles []string, perm string) bool
}
