package workflow

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// WorkflowInstance adalah satu jalannya workflow untuk satu entitas bisnis.
// DefinitionVersion dikunci saat Start — perubahan definisi setelah instance
// dimulai tidak mengubah alur yang sedang berjalan (PRD F1, F7).
//
// PR-3.2.1: instance dikembalikan sebagai nilai ke caller; storage di-handle PR-3.2.3.
// Engine stateless terhadap instance — caller menyimpan dan meneruskan saat Execute.
type WorkflowInstance struct {
	ID                uuid.UUID
	TenantID          string // tenant pemilik instance (dari AuthContext saat Start) — dipakai routing eskalasi SLA
	DefinitionID      string
	DefinitionVersion int
	EntityID          uuid.UUID
	CurrentState      string
	StartedAt         time.Time
	History           []TransitionRecord

	// Version adalah penghitung optimistic locking (konvensi framework, CLAUDE.md §Data
	// integrity). Instance BARU punya Version 0; setiap Save yang sukses menaikkannya satu.
	// InstanceStore menolak simpanan yang versinya tak lagi cocok dengan baris di DB
	// (core.ErrConflict) — tanpa itu, dua transisi bersamaan atas instance yang sama sama-sama
	// membaca state lama, sama-sama lolos guard, dan keduanya memanggil use case: satu surat
	// terdisposisi dua kali dengan hanya satu jejak di history.
	Version int

	// RoleBindings diisi HANYA oleh StartFromTemplate (PR-N2) — instance yang dimulai via Start
	// (defID langsung, unbound) membiarkannya nil. Ini SALINAN beku dari
	// TenantWorkflowConfig.RoleBindings pada saat Start: dipakai ulang di setiap Execute (lewat
	// ApplyBindings) agar EscalateToRole & NotifySpec.ToRole tetap peran KONKRET tenant
	// sepanjang hidup instance, konsisten dengan DefinitionVersion yang juga dikunci saat Start
	// — bukan dibaca ulang dari TemplateStore (yang bisa saja sudah direkonfigurasi tenant)
	// setiap transisi.
	RoleBindings map[string]string
}

// TransitionRecord adalah entri immutable dalam riwayat instance.
// Setiap transisi sukses menghasilkan satu record — tidak pernah dihapus atau diubah.
type TransitionRecord struct {
	From      string
	To        string
	Action    string // nama use case yang dipanggil, kosong bila tidak ada
	ActorID   uuid.UUID
	Timestamp time.Time
	Comment   string
}

// InstanceStore adalah driven port persistensi WorkflowInstance (ADR-022). Engine sendiri tetap
// STATELESS terhadap instance — ia menerima *WorkflowInstance dan memutasinya di memori; yang
// menyimpan adalah pemanggil (driving adapter workflow di gateway).
//
// Sampai PR-W4a port ini tak ada sama sekali, dan akibatnya dua kemampuan yang sudah ditulis
// tak bisa berjalan: transisi atas instance dari request sebelumnya, dan guard race eskalasi SLA
// (InstanceStateReader). Ia meng-embed InstanceStateReader karena implementasi DB yang sama
// melayani keduanya — pemisahan port tetap ada untuk pemakai yang HANYA butuh baca state.
type InstanceStore interface {
	InstanceStateReader

	// Save menyimpan instance (insert bila baru, update bila sudah ada) di bawah pemeriksaan
	// optimistic locking terhadap Version. Sukses menaikkan inst.Version di memori agar
	// pemanggil bisa langsung menyimpan lagi. Versi tak cocok → core.ErrConflict.
	Save(ctx context.Context, inst *WorkflowInstance) error

	// Get mengambil instance lengkap (termasuk History & RoleBindings) untuk melanjutkan
	// transisi. ErrInstanceNotFound bila tak ada.
	Get(ctx context.Context, id uuid.UUID) (*WorkflowInstance, error)

	// TryLockInstance mencoba mengambil kunci EKSKLUSIF atas satu instance, tanpa menunggu.
	// ok=false berarti transisi lain atas instance itu sedang berjalan — pemanggil harus
	// menolak permintaannya (409), bukan mengantre.
	//
	// Ia ada karena optimistic locking SAJA tidak cukup untuk transisi. Guard versi
	// melindungi BARIS instance: penulis yang kalah ditolak — tapi ditolak SESUDAH action-nya
	// terlanjur berjalan (baris bisnis tertulis, event terbit). Untuk alur pemerintahan efek
	// itu yang berbahaya, bukan barisnya: satu surat terdisposisi dua kali dengan satu jejak
	// di history. Kunci ini memindahkan penolakan ke DEPAN action.
	//
	// TIDAK menunggu (try, bukan lock) disengaja: mengantre akan menahan koneksi selama use
	// case berjalan, dan lonjakan request pada satu instance berubah menjadi habisnya pool.
	//
	// release WAJIB dipanggil (defer) — juga saat pemanggil gagal di tengah jalan. release
	// pada ok=false adalah no-op yang aman.
	TryLockInstance(ctx context.Context, id uuid.UUID) (release func(), ok bool, err error)
}
