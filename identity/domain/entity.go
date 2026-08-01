// Package domain adalah inti identitas: person (anchor NIK), employment (opsional,
// NIP untuk ASN), credential (banyak per person). Zero dependency infrastruktur —
// hanya stdlib + uuid. Postgres/pgx hidup di adapter/db.
package domain

import (
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

var (
	nikRe = regexp.MustCompile(`^\d{16}$`) // NIK: 16 digit
	nipRe = regexp.MustCompile(`^\d{18}$`) // NIP: 18 digit
)

// bentukPengenalRusak menolak nilai yang punya control character (CR/LF/TAB/NUL/C1) atau
// spasi di tepi. Berlaku untuk pengenal yang dipakai sebagai KUNCI PENCARIAN sekaligus
// ALAMAT TUJUAN: cred_value, email, no_hp.
//
// Aturannya ada DI SINI, bukan di lapis repo, karena masalahnya bukan kripto melainkan
// invariant: sejak pengenal di-lookup lewat blind index (PR-3.8.6), "nilai yang sama" punya
// definisi yang lebih longgar dari kesamaan byte — infra/crypto.normalize membuang spasi tepi
// (dan meng-case-fold `email`) sebelum meng-HMAC. Nilai yang disimpan verbatim karena itu bisa
// berbeda dari ejaan mana pun yang me-resolve kepadanya, dan selisihnya bocor ke luar: alamat
// ber-CRLF me-resolve ke baris nyata lalu DITOLAK transport di hilir (SMTP membacanya sebagai
// header injection → 500), sementara alamat asing menjawab 200. Beda respons itulah orakel
// keberadaan akun — sama seperti yang ditutup di jalur baca RequestOTP, kini di jalur TULIS.
//
// Menormalkan di seal() BUKAN jalan keluar: ia membuat nilai terdekripsi ≠ nilai yang
// didaftarkan (dump berbeda dari yang diketik warga, tanpa jejak), sekaligus menyalin tabel
// kebijakan infra/crypto ke lapis repo sebagai sumber kebenaran kedua. Menolak di pintu masuk
// menjaga verbatim tetap verbatim: yang tersimpan hanya nilai yang sudah kanonik.
func bentukPengenalRusak(v string) bool {
	if v != strings.TrimSpace(v) {
		return true
	}
	for _, r := range v {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

// Person adalah satu manusia — anchor identitas global ada di NIK. Setiap orang punya
// satu baris ini, terlepas dari status kepegawaian.
type Person struct {
	ID          uuid.UUID
	NIK         string // unik global, 16 digit
	NamaLengkap string
	TglLahir    *time.Time // opsional
	NoHP        string
	Email       string
	IsActive    bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Validate memeriksa invariant person tanpa I/O.
func (p *Person) Validate() error {
	if !nikRe.MatchString(p.NIK) {
		return ErrNIKInvalid
	}
	if p.NamaLengkap == "" {
		return ErrNamaKosong
	}
	// Kontak person menempuh jalur yang sama dengan cred_value: tersimpan terenkripsi +
	// blind index, lalu ikut ke clone gov.user_profiles dan dari sana menjadi alamat kirim
	// notifikasi (PR-N3b/ADR-013). Aturan bentuknya karena itu sama.
	if bentukPengenalRusak(p.Email) {
		return ErrEmailFormat
	}
	if bentukPengenalRusak(p.NoHP) {
		return ErrNoHPFormat
	}
	return nil
}

// EmploymentStatus membedakan ASN (punya NIP) dari non-ASN (tanpa NIP).
type EmploymentStatus string

const (
	StatusASN    EmploymentStatus = "asn"
	StatusNonASN EmploymentStatus = "non_asn"
)

// Employment adalah relasi kepegawaian — OPSIONAL. Tidak semua person adalah pegawai.
// Satu person bisa punya >1 employment sepanjang waktu.
type Employment struct {
	ID           uuid.UUID
	PersonID     uuid.UUID
	Status       EmploymentStatus
	NIP          string // wajib & 18 digit bila ASN; kosong bila non-ASN
	InstansiAsal string
	IsActive     bool
	ValidFrom    time.Time
	ValidUntil   *time.Time // nil = berlaku tak terbatas
	CreatedAt    time.Time
}

// Validate menegakkan constraint kepegawaian: status=asn ⇒ NIP wajib (18 digit);
// status=non_asn ⇒ NIP harus kosong (PRD F1).
func (e *Employment) Validate() error {
	switch e.Status {
	case StatusASN:
		if e.NIP == "" {
			return ErrNIPWajibASN
		}
		if !nipRe.MatchString(e.NIP) {
			return ErrNIPInvalid
		}
	case StatusNonASN:
		if e.NIP != "" {
			return ErrNIPTerisiNonASN
		}
	default:
		return ErrStatusInvalid
	}
	if e.PersonID == uuid.Nil {
		return ErrPersonIDKosong
	}
	return nil
}

// IsActiveAt melaporkan apakah employment aktif pada saat now: flag IsActive menyala DAN now
// berada dalam masa berlaku [ValidFrom, ValidUntil). Dipakai alur login (PR-2.4.3) untuk
// menolak pegawai non-aktif/kedaluwarsa masuk portal internal. Fungsi murni — teruji tanpa DB.
func (e *Employment) IsActiveAt(now time.Time) bool {
	if !e.IsActive {
		return false
	}
	if now.Before(e.ValidFrom) {
		return false
	}
	if e.ValidUntil != nil && !now.Before(*e.ValidUntil) {
		return false
	}
	return true
}

// CredType adalah jenis identifier login. Semua credential satu person resolve ke person
// yang sama.
type CredType string

const (
	CredNIP   CredType = "nip"
	CredNIK   CredType = "nik"
	CredEmail CredType = "email"
	CredNoHP  CredType = "no_hp"
	CredOAuth CredType = "oauth"
)

var validCredTypes = map[CredType]bool{
	CredNIP: true, CredNIK: true, CredEmail: true, CredNoHP: true, CredOAuth: true,
}

// Credential adalah satu jalur login (NIP/NIK/email/no_hp/oauth) milik person.
type Credential struct {
	ID         uuid.UUID
	PersonID   uuid.UUID
	CredType   CredType
	CredValue  string
	SecretHash string // bcrypt hash; kosong bila SSO/OTP-only
	IsPrimary  bool
	LastUsedAt *time.Time
	CreatedAt  time.Time
}

// Validate memeriksa tipe & nilai credential.
func (c *Credential) Validate() error {
	if c.PersonID == uuid.Nil {
		return ErrPersonIDKosong
	}
	if !validCredTypes[c.CredType] {
		return ErrCredTypeInvalid
	}
	if c.CredValue == "" {
		return ErrCredValueKosong
	}
	if bentukPengenalRusak(c.CredValue) {
		return ErrCredValueFormat
	}
	return nil
}
