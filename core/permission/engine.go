package permission

import "github.com/huda-salam/pamong/port"

// Engine mengevaluasi keputusan RBAC: diberi kumpulan role yang dipegang actor,
// apakah sebuah permission diberikan.
//
// Resolusi PR-2.3.1 = UNION: cukup salah satu role yang dikenali catalog memberi
// permission (sejalan F1 PRD). Prioritas "global menang" dan strict-intersection
// (F7 PRD) baru berdampak saat lapis central + tenant hidup berdampingan —
// diaktifkan pada PR-2.3.2 (central roles) & PR-2.3.3 (tenant roles). Sampai itu,
// strict hanya dimodelkan (lihat IsStrict), belum mengubah hasil evaluasi.
type Engine struct {
	catalog RoleCatalog
	strict  map[Permission]struct{}
}

var _ port.PermissionEvaluator = (*Engine)(nil)

// NewEngine membuat Engine dengan catalog role dan daftar permission strict
// (opsional). Permission strict ditandai kini agar deklarasinya stabil; aturan
// intersection-nya ditegakkan saat resolusi lintas-lapis aktif (2.3.2/2.3.3).
func NewEngine(catalog RoleCatalog, strict ...Permission) *Engine {
	s := make(map[Permission]struct{}, len(strict))
	for _, p := range strict {
		s[p] = struct{}{}
	}
	return &Engine{catalog: catalog, strict: s}
}

// Allows melaporkan apakah salah satu role yang dipegang actor memberi perm.
// Role yang tidak terdaftar di catalog diabaikan (bukan error) — actor bisa
// membawa nama role yang belum dikenal proses ini.
func (e *Engine) Allows(roles []string, perm string) bool {
	for _, name := range roles {
		role, ok := e.catalog.Lookup(name)
		if !ok {
			continue
		}
		for _, g := range role.Permissions {
			if g == perm {
				return true
			}
		}
	}
	return false
}

// IsStrict melaporkan apakah permission ditandai strict — yakni dikecualikan dari
// resolusi union saat lapis role bertumpuk (semua role harus mengizinkan). Aturan
// intersection-nya ditegakkan pada PR-2.3.2/2.3.3; di 2.3.1 ini metadata model.
func (e *Engine) IsStrict(perm string) bool {
	_, ok := e.strict[perm]
	return ok
}
