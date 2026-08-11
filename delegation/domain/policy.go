package domain

import "strings"

// NonDelegableSet adalah himpunan permission yang TAK BOLEH didelegasikan (PRD F5, mis. TTD
// KPA tertentu). Di-inject ke use case CreateDelegation.
//
// Entri boleh berupa permission utuh (`keuangan:spm:terbitkan`) ATAU **namespace** berakhiran
// `:*` (`identity:*`). Wildcard ada karena yang perlu dilarang seringkali seluruh keluarga
// permission, dan mendaftarnya satu-satu berarti setiap permission baru di keluarga itu diam-diam
// menjadi boleh didelegasikan — larangan yang bocor seiring waktu tanpa ada yang mengubahnya.
//
// DEFERRED(Phase-2.4): sumber dari flag non_delegable per-permission di manifest modul,
// menggantikan daftar manual ini (lihat ROADMAP). Saat itu use case mengambil himpunan dari
// registry permission alih-alih konstruksi manual.
type NonDelegableSet struct {
	exact      map[string]bool
	namespaces []string // prefiks termasuk titik dua, mis. "identity:"
}

// NewNonDelegableSet membangun himpunan dari daftar permission dan/atau namespace (`ns:*`).
// Kosong = tak ada larangan — dan itu keadaan yang HARUS disengaja: lihat DefaultNonDelegable.
func NewNonDelegableSet(perms ...string) NonDelegableSet {
	s := NonDelegableSet{exact: make(map[string]bool, len(perms))}
	for _, p := range perms {
		if ns, ok := strings.CutSuffix(p, "*"); ok {
			s.namespaces = append(s.namespaces, ns)
			continue
		}
		s.exact[p] = true
	}
	return s
}

// DefaultNonDelegable adalah larangan MINIMUM yang berlaku di semua tenant. Ia bukan kenyamanan:
// himpunan yang kosong membuat delegasi menjadi jalur pemberian wewenang tanpa pagar apa pun,
// sementara delegasi justru jalur MANDIRI di evaluator (tak tunduk strict-intersection role) dan
// pembuatnya belum diwajibkan memegang sendiri permission yang ia limpahkan (ADR-021, keputusan
// tertunda). Sampai pemeriksaan per-permission itu ada, dua namespace ini yang menutup lubang
// terburuk:
//
//   - `identity:*` — permission lapis SENTRAL. Ia sudah dipagari agar tak bisa masuk role tenant
//     (reservedPermissionPrefix di tenantrole); tanpa pagar yang sama di delegasi, jalan pintasnya
//     tinggal "delegasikan" alih-alih "berikan lewat role", dengan hasil akhir yang sama:
//     penerbitan kredensial bagi person mana pun, lalu login sebagai orang itu.
//   - `iam:*` — permission yang MEMBERI wewenang (buat/tugaskan role tenant, buat delegasi).
//     Melimpahkannya berarti melimpahkan kemampuan melimpahkan; sekali lolos, containment unit
//     apa pun bisa dilebarkan sendiri oleh penerimanya secara berantai.
//
// Larangan tambahan spesifik-tenant (mis. TTD KPA) ditambahkan di atas ini, bukan menggantinya.
func DefaultNonDelegable(extra ...string) NonDelegableSet {
	return NewNonDelegableSet(append([]string{"identity:*", "iam:*"}, extra...)...)
}

// Contains melaporkan apakah perm termasuk yang tak boleh didelegasikan — lewat pencocokan utuh
// atau lewat namespace.
func (s NonDelegableSet) Contains(perm string) bool {
	if s.exact[perm] {
		return true
	}
	for _, ns := range s.namespaces {
		if strings.HasPrefix(perm, ns) {
			return true
		}
	}
	return false
}
