package domain

import "github.com/huda-salam/pamong/core"

// Domain error identity memakai error types framework agar auto-map ke HTTP status
// (CODE_CONVENTION #3). Konflik unik (NIK/NIP duplikat) dihasilkan adapter via
// core.ErrConflict saat pelanggaran unique constraint terdeteksi.
var (
	ErrNIKInvalid      = core.ErrValidation("nik", "harus 16 digit angka")
	ErrNamaKosong      = core.ErrValidation("nama_lengkap", "tidak boleh kosong")
	ErrNIPInvalid      = core.ErrValidation("nip", "harus 18 digit angka")
	ErrNIPWajibASN     = core.ErrValidation("nip", "wajib diisi untuk ASN")
	ErrNIPTerisiNonASN = core.ErrValidation("nip", "harus kosong untuk non-ASN")
	ErrStatusInvalid   = core.ErrValidation("status", "harus 'asn' atau 'non_asn'")
	ErrPersonIDKosong  = core.ErrValidation("person_id", "tidak boleh kosong")
	ErrCredTypeInvalid = core.ErrValidation("cred_type", "tidak dikenal")
	ErrCredValueKosong = core.ErrValidation("cred_value", "tidak boleh kosong")

	ErrTenantIDInvalid   = core.ErrValidation("tenant_id", "harus lowercase, mulai huruf, 3-100 char (a-z0-9-)")
	ErrTenantNamaKosong  = core.ErrValidation("nama", "tidak boleh kosong")
	ErrTenantTierInvalid = core.ErrValidation("tier", "harus 1, 2, atau 3")
	ErrTenantDBKosong    = core.ErrValidation("db", "db_host dan db_name wajib diisi")
)
