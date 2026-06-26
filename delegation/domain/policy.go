package domain

// NonDelegableSet adalah himpunan permission yang TAK BOLEH didelegasikan (PRD F5, mis. TTD
// KPA tertentu). Di-inject ke use case CreateDelegation. MVP menerima daftar eksplisit
// (kosong = tak ada larangan).
//
// DEFERRED(Phase-2.4): sumber dari flag non_delegable per-permission di manifest modul,
// menggantikan daftar manual ini (lihat ROADMAP). Saat itu use case mengambil himpunan dari
// registry permission alih-alih konstruksi manual.
type NonDelegableSet map[string]bool

// NewNonDelegableSet membangun himpunan dari daftar permission.
func NewNonDelegableSet(perms ...string) NonDelegableSet {
	s := make(NonDelegableSet, len(perms))
	for _, p := range perms {
		s[p] = true
	}
	return s
}

// Contains melaporkan apakah perm termasuk yang tak boleh didelegasikan.
func (s NonDelegableSet) Contains(perm string) bool { return s[perm] }
