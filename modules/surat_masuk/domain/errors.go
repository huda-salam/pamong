package domain

import "github.com/huda-salam/pamong/core"

// Domain errors memakai error types framework agar auto-map ke HTTP status oleh gateway
// (CODE_CONVENTION #3). Modul tidak menangani mapping sendiri.
var (
	ErrNomorSuratKosong          = core.ErrValidation("nomor_surat", "tidak boleh kosong")
	ErrTanggalAgendaSebelumSurat = core.ErrValidation("tanggal_agenda", "tidak boleh sebelum tanggal surat")
	ErrSuratTidakDitemukan       = core.ErrNotFound("SuratMasuk", "")
)

// PermissionDenied & Conflict dihasilkan framework saat RequirePermission / optimistic
// lock gagal; modul tidak membuatnya manual.
