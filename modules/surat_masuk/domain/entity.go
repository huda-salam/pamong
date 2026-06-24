package domain

import (
	"time"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core/domain"
)

// EntitySuratMasuk adalah definisi entity Tier 3 (punya business logic & workflow).
// Audit dan Lockable WAJIB eksplisit — tidak ada default diam-diam (CODING_PHILOSOPHY #3).
var EntitySuratMasuk = domain.EntityDef{
	Name:      "SuratMasuk",
	Schema:    "surat_masuk",
	Tablename: "surat_masuk.surat_masuks",
	Tier:      domain.Tier3,

	// Surat masuk diaudit penuh (siapa membuat, mengubah, mendisposisi).
	Audit: domain.Audited{},
	// Lockable terhadap periode fiskal berdasarkan tanggal agenda; saat tahun
	// ditutup, surat tahun itu tidak bisa dimutasi.
	Lockable:       domain.Lockable{FiscalField: "tanggal_agenda"},
	HasAttachments: true,
	Searchable:     []string{"nomor_agenda", "nomor_surat", "perihal", "pengirim"},

	Fields: []domain.FieldDef{
		{Name: "nomor_agenda", Type: domain.FieldText, Required: true, Unique: true},
		{Name: "nomor_surat", Type: domain.FieldText, Required: true},
		{Name: "tanggal_surat", Type: domain.FieldDate, Required: true},
		{Name: "tanggal_agenda", Type: domain.FieldDate, Required: true},
		{Name: "pengirim", Type: domain.FieldText, Required: true},
		{Name: "perihal", Type: domain.FieldText, Required: true},
		{Name: "sifat", Type: domain.FieldEnum, Required: true,
			Options: []string{"biasa", "segera", "rahasia"}},
		// status dikelola workflow engine, bukan diset modul langsung.
		{Name: "status", Type: domain.FieldText},
	},
}

// EntityDisposisi menyimpan instruksi disposisi berjenjang atas sebuah surat.
var EntityDisposisi = domain.EntityDef{
	Name:      "Disposisi",
	Schema:    "surat_masuk",
	Tablename: "surat_masuk.disposisis",
	Tier:      domain.Tier3,
	Audit:     domain.Audited{},
	// Disposisi mengikuti penguncian surat induknya melalui tanggal.
	Lockable: domain.Lockable{FiscalField: "tanggal"},
	Fields: []domain.FieldDef{
		{Name: "surat_id", Type: domain.FieldLink, Required: true, LinkTo: "surat_masuk.SuratMasuk"},
		{Name: "dari_jabatan", Type: domain.FieldText, Required: true},
		// Penerima disimpan sebagai PERAN/jabatan; resolusi ke orang di layer permission.
		{Name: "kepada_jabatan", Type: domain.FieldText, Required: true},
		{Name: "instruksi", Type: domain.FieldText, Required: true},
		{Name: "tanggal", Type: domain.FieldDateTime, Required: true},
	},
}

// SuratMasuk adalah domain struct yang dipakai use case (bukan FieldDef).
// Uang tidak ada di modul ini; bila ada, gunakan decimal.Decimal — tidak pernah float64.
type SuratMasuk struct {
	ID            uuid.UUID
	NomorAgenda   string
	NomorSurat    string
	TanggalSurat  time.Time
	TanggalAgenda time.Time
	Pengirim      string
	Perihal       string
	Sifat         string
	Status        string
	Version       int // optimistic lock — dikelola framework
}

// Validate memuat invariant domain murni (tanpa I/O, tanpa dependency).
func (s *SuratMasuk) Validate() error {
	if s.NomorSurat == "" {
		return ErrNomorSuratKosong
	}
	if s.TanggalAgenda.Before(s.TanggalSurat) {
		return ErrTanggalAgendaSebelumSurat
	}
	return nil
}

// Disposisi domain struct.
type Disposisi struct {
	ID            uuid.UUID
	SuratID       uuid.UUID
	DariJabatan   string
	KepadaJabatan string
	Instruksi     string
	Tanggal       time.Time
}
