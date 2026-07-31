package audit

// Permission tata-kelola pembacaan audit (CLAUDE.md §Permission: {modul}:{entity}:{aksi}).
// Dicek di Reader lewat port.AuthContext — bukan string literal di tempat lain
// (CLAUDE.md aturan #8).
const (
	// PermSensitiveBaca mengizinkan pemegangnya MEMBUKA nilai diff ber-class personal_id/
	// specific yang tersimpan terenkripsi (ADR-009 §6 butir 1, ADR-002). Tanpa permission ini
	// riwayat audit tetap terbaca seluruhnya — hanya nilai pengenalnya yang tertutup, sehingga
	// pemeriksa biasa tetap melihat SIAPA mengubah APA dan KAPAN tanpa perlu melihat NIK.
	PermSensitiveBaca = "audit:sensitive:baca"
)
