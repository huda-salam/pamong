package permission

import "github.com/huda-salam/pamong/port"

// Engine mengevaluasi keputusan RBAC: diberi kumpulan role yang dipegang actor,
// apakah sebuah permission diberikan.
//
// Resolusi konflik PENUH (F7 PRD) ditegakkan sejak PR-2.3.3, saat lapis central
// (global/scoped) dan tenant hidup berdampingan lewat catalog komposit. Aturannya
// (CLAUDE.md "Lapisan role" — Opsi A):
//
//   - GLOBAL menang tanpa syarat: bila ada role global yang memberi perm → IZIN,
//     termasuk untuk permission strict (global = otoritas platform tak bersyarat,
//     "selalu menang atas role apapun"). Role global yang TIDAK memberi perm bersifat
//     netral — tidak pernah memblokir.
//   - Antar role non-global (scoped + tenant, satu tingkat prioritas):
//   - perm biasa  → UNION  (satu role memberi sudah cukup).
//   - perm strict → INTERSECTION (izin hanya bila SEMUA role non-global yang dipegang
//     memberi perm, dan minimal satu memberi). Konsekuensi sengaja: memegang role
//     yang tak memberi perm strict akan memblokirnya (segregation of duties) — karena
//     itu strict dipakai hemat.
//
// Layer tiap role dibaca dari catalog (RoleCatalog.Lookup), jadi kontrak Engine &
// port.PermissionEvaluator tidak berubah saat lapis tenant ditambahkan (titik ekstensi #1).
type Engine struct {
	catalog RoleCatalog
	strict  map[Permission]struct{}
}

var _ port.PermissionEvaluator = (*Engine)(nil)

// NewEngine membuat Engine dengan catalog role dan daftar permission strict (opsional).
func NewEngine(catalog RoleCatalog, strict ...Permission) *Engine {
	s := make(map[Permission]struct{}, len(strict))
	for _, p := range strict {
		s[p] = struct{}{}
	}
	return &Engine{catalog: catalog, strict: s}
}

// Allows melaporkan apakah role yang dipegang actor memberi perm, mengikuti resolusi
// global-precedence + (union | strict-intersection) di atas. Role yang tidak terdaftar
// di catalog diabaikan (bukan error) — actor bisa membawa nama role yang belum dikenal
// proses ini; ia tidak ikut dihitung dalam intersection.
func (e *Engine) Allows(roles []string, perm string) bool {
	strict := e.IsStrict(perm)

	// Tally lapis non-global (scoped + tenant) untuk keputusan union/intersection.
	nonGlobalSeen, nonGlobalGrant := 0, 0
	for _, name := range roles {
		role, ok := e.catalog.Lookup(name)
		if !ok {
			continue
		}
		grants := roleGrants(role, perm)
		if role.Layer == LayerGlobal {
			if grants {
				return true // global menang tanpa syarat (termasuk strict)
			}
			continue // global tanpa grant = netral, tidak memblokir
		}
		nonGlobalSeen++
		if grants {
			nonGlobalGrant++
			if !strict {
				return true // union: satu role non-global cukup
			}
		}
	}
	if strict {
		// intersection: minimal satu role non-global, dan SEMUA memberi perm.
		return nonGlobalSeen > 0 && nonGlobalGrant == nonGlobalSeen
	}
	return false
}

// roleGrants melaporkan apakah definisi role memuat perm.
func roleGrants(role Role, perm string) bool {
	for _, g := range role.Permissions {
		if g == perm {
			return true
		}
	}
	return false
}

// IsStrict melaporkan apakah permission ditandai strict — dikecualikan dari resolusi
// union antar role non-global (intersection: semua harus mengizinkan; lihat Allows).
func (e *Engine) IsStrict(perm string) bool {
	_, ok := e.strict[perm]
	return ok
}
