package domain

// Permission identity — dipakai use case lewat ctx.RequirePermission. Tidak ada
// hardcode string di luar konstanta ini (CODE_CONVENTION #8).
const (
	PermPersonBuat       = "identity:person:buat"
	PermEmploymentLampir = "identity:employment:lampir"

	PermTenantDaftar   = "identity:tenant:daftar"
	PermTenantBaca     = "identity:tenant:baca"
	PermTenantNonaktif = "identity:tenant:nonaktif"

	// PermAssignmentTugaskan = menugaskan employment ke tenant (home tenant).
	// PermAssignmentCrossTenant = tambahan wajib bila penugasan cross-tenant
	// (is_home_tenant=false; mis. PJ Bupati) — sesuai catatan skema id.tenant_assignments.
	PermAssignmentTugaskan    = "identity:assignment:tugaskan"
	PermAssignmentCrossTenant = "identity:assignment:cross_tenant"
)
