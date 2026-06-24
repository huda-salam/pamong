package domain

// Konstanta permission — TIDAK ada string literal permission tersebar di kode
// (CODE_CONVENTION: no hardcode). Format: {modul}:{entity}:{aksi}.
const (
	PermSuratBuat      = "surat_masuk:surat:buat"
	PermSuratBaca      = "surat_masuk:surat:baca"
	PermSuratDisposisi = "surat_masuk:surat:disposisi"
)
