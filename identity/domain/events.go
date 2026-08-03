package domain

import "github.com/google/uuid"

// Event identitas mengikuti format {modul}.{entity}.{kejadian_past_tense} (CLAUDE.md).
// Use case menerbitkannya lewat port.EventPublisher; sync engine (identity/sync)
// mengonsumsinya untuk meng-clone data ke gov.user_profiles tenant. Nama event wajib
// berupa konstanta — tidak ada string literal di publisher (linter event-must-use-const).
//
// Payload membawa KOORDINAT (id) + atribut non-pengenal, dan sengaja TIDAK membawa nilai
// kelas `personal_id` (nik, nip, email, no_hp — ADR-009). Sampai PR-3.8.5a payload dirancang
// "fat" agar consumer tak perlu membaca-balik identity DB; PR-3.8.5b membalikkan pilihan itu
// untuk pengenal saja, karena payload adalah jalur samping yang wajib ditutup (ADR-009 §6
// butir 2) dan menghapus nilainya lebih murah daripada menyegelnya:
//
//   - blob tersegel di stream NATS retensi panjang / `gov.outbox_events.payload` menjadi
//     kewajiban dekripsi PERMANEN — melintasi rotasi kunci dan patahan format (`0x01` sudah
//     ditolak `Decrypt` sejak ADR-016);
//   - ciphertext identity ber-realm sentral, sedangkan clone ber-realm tenant (ADR-017), jadi
//     ia tak bisa diteruskan apa adanya — penulis clone harus memegang kunci realm sentral.
//
// Consumer yang butuh nilainya memintanya lewat port di sisi identity (`sync.CloneSource`).
// `NamaLengkap` TETAP di payload: kelasnya `personal`, bukan `personal_id` — ADR-009 sengaja
// tidak mengenkripsinya di kolom, jadi mengeluarkannya dari sini akan menerapkan standar yang
// lebih ketat daripada yang berlaku at-rest.
//
// Menghapus field BUKAN perubahan breaking di kawat: `SchemaRegistry` mencocokkan identitas
// tipe Go (schema.go), dan `Unmarshal` memakai encoding/json yang mengabaikan key tak dikenal —
// pesan lama ter-unmarshal mulus, nilai lamanya sekadar tak terbaca. Yang TIDAK dibersihkan
// mesin ini: pesan/baris outbox yang terlanjur mengendap tetap memuat plaintext (runbook).
const (
	EventPersonDibuat         = "identity.person.dibuat"
	EventEmploymentDibuat     = "identity.employment.dibuat"
	EventEmploymentDitugaskan = "identity.employment.ditugaskan"
)

// PersonDibuatPayload menyertai EventPersonDibuat — master person baru di identity DB.
// Tanpa NIK (ADR-009 §6): event ini belum punya satu pun subscriber, jadi pengenalnya murni
// liabilitas. Consumer yang kelak butuh NIK memakai port, bukan menambahkannya kembali.
type PersonDibuatPayload struct {
	PersonID    uuid.UUID
	NamaLengkap string
}

// EmploymentDibuatPayload menyertai EventEmploymentDibuat — person ini kini punya
// kepegawaian (belum tentu tertugaskan ke tenant manapun). Tanpa NIP, alasan sama dengan
// PersonDibuatPayload.
type EmploymentDibuatPayload struct {
	EmploymentID uuid.UUID
	PersonID     uuid.UUID
	Status       string
}

// EmploymentDitugaskanPayload menyertai EventEmploymentDitugaskan — pemicu clone person
// ke gov.user_profiles tenant tujuan.
//
// NIK/NIP/Email/NoHP DIBUANG di PR-3.8.5b; `identity/sync` memintanya ke `CloneSource`.
// Keputusan #1 ADR-013 (kontak mendarat di clone tenant, jalur KIRIM notifikasi tetap satu
// join same-schema) tidak berubah — yang berganti hanya KURIRNYA dari event ke baca-balik
// saat sync. Opsi yang dulu ditolak ADR-013 adalah baca live saat KIRIM, di sisi tenant;
// yang ini sekali per penugasan, di sisi identity.
type EmploymentDitugaskanPayload struct {
	AssignmentID     uuid.UUID
	EmploymentID     uuid.UUID
	PersonID         uuid.UUID
	TenantID         string
	NamaLengkap      string
	EmploymentStatus string
	IsCrossTenant    bool
}
