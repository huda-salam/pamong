// Package workflow mengorkestrasi use case yang membentang lintas waktu dan aktor.
// Definisi workflow adalah DATA (bukan kode) — bisa berbeda antar tenant dan ber-versi.
// Logika sesungguhnya tetap di use case Go; engine hanya mengelola state dan memanggil
// use case lewat ActionDispatcher port (CLAUDE.md §7, PRD workflow F1).
package workflow

import "time"

// WorkflowDefinition adalah template alur kerja yang dapat dipilih dan di-override
// per-tenant. ID mengikuti format {modul}.{alur}.{varian} (mis. "surat_masuk.disposisi.standar").
// AuthoringSource = "developer" untuk seed/template; "tenant" untuk yang ditulis pemda (future).
type WorkflowDefinition struct {
	ID              string
	Entity          string    // entity yang dikelola, mis. "surat_masuk.SuratMasuk"
	Version         int       // dinaikkan setiap perubahan; instance kunci ke versi saat mulai
	EffectiveFrom   time.Time // versi ini berlaku sejak kapan
	InitialState    string    // state awal saat instance dimulai
	States          []State
	Transitions     []Transition
	AuthoringSource string // "developer" | "tenant"
}

// stateMap membangun indeks nama→State untuk lookup O(1).
func (d *WorkflowDefinition) stateMap() map[string]State {
	m := make(map[string]State, len(d.States))
	for _, s := range d.States {
		m[s.Name] = s
	}
	return m
}

// State adalah satu titik dalam alur kerja. Actions mendaftarkan aksi yang bisa
// dipicu dari state ini (dipakai UI untuk menampilkan tombol yang relevan).
// IsTerminal=true berarti tidak ada transisi keluar — instance selesai.
type State struct {
	Name           string
	Label          string
	SLAHours       int    // 0 = tanpa SLA; > 0 = deadline dari scheduler (PR-3.2.6)
	EscalateToRole string // role yang dinotifikasi saat SLA terlewat
	IsTerminal     bool
	Actions        []string // aksi yang tersedia di state ini (untuk UI)
}

// Transition mendefinisikan perpindahan state. On adalah nama aksi pemicu.
// Guards di-AND-kan: semua harus terpenuhi agar transisi lolos.
// Action (opsional) adalah NAMA USE CASE yang dipanggil engine — bukan logika inline.
// Workflow berbicara PERAN; pemetaan peran→orang ada di layer permission (PR-3.2.4).
type Transition struct {
	From   string
	To     string
	On     string   // nama aksi/event pemicu
	Guards []string // ekspresi boolean, di-compile saat load (PR-3.2.5)
	Action string   // nama use case (kosong = tidak ada use case, transisi sah)
	Notify *NotifySpec
}

// NotifySpec mendefinisikan notifikasi yang dikirim setelah transisi sukses.
// Pengiriman aktual dilakukan core/notification (DEFERRED PR-3.6.x) via port.
type NotifySpec struct {
	ToRole   string
	Template string
}
