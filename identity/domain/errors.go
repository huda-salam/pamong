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

	ErrEmploymentIDKosong   = core.ErrValidation("employment_id", "tidak boleh kosong")
	ErrAssignedByKosong     = core.ErrValidation("assigned_by", "tidak boleh kosong")
	ErrEmploymentTidakAktif = core.ErrValidation("employment_id", "employment tidak aktif atau sudah berakhir")
	ErrTenantTidakAktif     = core.ErrValidation("tenant_id", "tenant tidak ditemukan atau tidak aktif")
	ErrAssignmentDuplikat   = core.ErrValidation("tenant_id", "sudah ada penugasan aktif ke tenant ini")

	ErrCentralRoleNameInvalid = core.ErrValidation("name", "harus snake_case, mulai huruf, 3-100 char (a-z0-9_)")
	ErrCentralRoleLabelKosong = core.ErrValidation("label", "tidak boleh kosong")
	ErrScopeTypeInvalid       = core.ErrValidation("scope_type", "harus 'global' atau 'scoped'")
	ErrRoleIDKosong           = core.ErrValidation("role_id", "tidak boleh kosong")
	// Koherensi scope_type role vs tenant_scope assignment (ditegakkan di use case).
	ErrScopeWajibUntukScoped = core.ErrValidation("tenant_scope", "wajib diisi untuk role scoped")
	ErrScopeDilarangGlobal   = core.ErrValidation("tenant_scope", "harus kosong untuk role global")
)
