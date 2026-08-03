// Package sync adalah clone engine identity: meng-subscribe event identity dan menulis
// salinan read-only person+employment ke gov.user_profiles pada DB tenant tujuan. Inilah
// jembatan identity DB sentral → tenant DB (CLAUDE.md "Identity sync engine").
//
// Modul bisnis TIDAK menyentuh package ini; mereka membaca data user lewat UserResolver
// port. Sync engine berdiri di sisi adapter (boleh import infra) — bukan domain/usecase.
package sync

import (
	"context"

	"github.com/google/uuid"
)

// UserProfileClone adalah salinan person+employment yang ditulis ke gov.user_profiles satu
// tenant. Sumbernya DUA: atribut non-pengenal (nama, status, cross-tenant) datang dari payload
// event, sedangkan keempat pengenal kelas personal_id datang dari CloneSource — dibaca balik ke
// identity saat event ditangani, karena payload event adalah jalur samping yang wajib ditutup
// (ADR-009 §6, ADR-018). Jangan mengembalikan pengenal ke payload demi "satu sumber": nilainya
// akan mendarat plaintext di gov.outbox_events dan di stream NATS ber-retensi.
type UserProfileClone struct {
	PersonID         uuid.UUID
	AssignmentID     uuid.UUID
	NIK              string
	NIP              string
	NamaLengkap      string
	EmploymentStatus string
	IsCrossTenant    bool
	// Kontak person (dari id.persons) — mengisi Recipient.Email/Phone untuk channel
	// notifikasi email/SMS (PR-N3b, ADR-013). Kosong = kanal itu tak tersedia untuk user ini.
	Email string
	NoHP  string
}

// Writer menulis clone ke DB satu tenant. Implementasi (writer_tenantdb.go) memilih pool
// tenant lewat TenantConnManager lalu upsert idempoten (event bisa terkirim ulang).
// Diabstraksikan sebagai port agar Engine bisa diuji tanpa Postgres.
type Writer interface {
	Upsert(ctx context.Context, tenantID string, c UserProfileClone) error
}

// Identifiers adalah keempat pengenal kelas personal_id (ADR-009) yang dibutuhkan clone.
// Ia TIDAK ikut di payload event — Engine memintanya ke CloneSource saat event ditangani.
type Identifiers struct {
	NIK   string
	NIP   string // kosong untuk employment non-ASN
	Email string
	NoHP  string
}

// CloneSource membaca pengenal person+employment dari identity DB saat event ditangani.
//
// Ia ada karena pengenal DIBUANG dari payload event (PR-3.8.5b, ADR-009 §6 butir 2): jalur
// samping ditutup dengan menghapus nilainya, bukan menyegelnya. Menyegel di payload akan
// menaruh ciphertext realm sentral di stream/outbox — kewajiban dekripsi yang hidup selama
// retensi stream dan melintasi rotasi kunci maupun patahan format (`0x01` sudah ditolak
// `Decrypt` sejak ADR-016) — sekaligus memaksa penulis clone memegang kunci realm sentral,
// persis pengaturan yang ADR-017 hindari.
//
// Konsekuensi yang disengaja: clone kini menerima nilai saat HANDLING, bukan saat event
// terbit. Itu justru kontrak `gov.user_profiles` yang berlaku — clone HIDUP ("siapa user ini
// sekarang"), bukan sumber dokumen historis (CLAUDE.md §Identity). Dokumen bisnis yang butuh
// nilai saat itu sudah wajib men-snapshot sendiri.
//
// Implementasinya (source_repo.go) berdiri di atas repo identity yang SUDAH mendekripsi realm
// sentral — jadi kunci sentral tak pernah keluar dari sisi identity.
type CloneSource interface {
	// Identifiers mengembalikan pengenal person + NIP employment-nya. employmentID boleh
	// uuid.Nil (person tanpa employment) → NIP kosong.
	//
	// Person/employment yang tak ditemukan WAJIB menghasilkan error, bukan nilai kosong:
	// clone berpengenal kosong tak bisa dibedakan dari person yang memang tak punya NIP, dan
	// ia melumpuhkan lookup (`ResolveByNIK`) serta routing notifikasi tanpa satu pun gejala.
	// Error di sini menahan event agar di-retry — gagal lantang, bukan clone cacat senyap.
	Identifiers(ctx context.Context, personID, employmentID uuid.UUID) (Identifiers, error)
}
