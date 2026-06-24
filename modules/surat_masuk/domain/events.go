package domain

import (
	"time"

	"github.com/google/uuid"
)

// Konstanta event — publish WAJIB pakai konstanta, bukan string literal
// [linter: event-must-use-const]. Format: {modul}.{entity}.{kejadian_past_tense}.
const (
	EventSuratDiterima    = "surat_masuk.surat.diterima"
	EventSuratDidisposisi = "surat_masuk.surat.didisposisi"
)

// SuratDiterimaPayload adalah schema event; didaftarkan di manifest dan divalidasi
// event bus saat publish.
type SuratDiterimaPayload struct {
	SuratID     uuid.UUID `json:"surat_id"`
	NomorAgenda string    `json:"nomor_agenda"`
	Sifat       string    `json:"sifat"`
	DiterimaAt  time.Time `json:"diterima_at"`
}

// SuratDidisposisiPayload schema event disposisi.
type SuratDidisposisiPayload struct {
	SuratID       uuid.UUID `json:"surat_id"`
	DisposisiID   uuid.UUID `json:"disposisi_id"`
	KepadaJabatan string    `json:"kepada_jabatan"`
}
