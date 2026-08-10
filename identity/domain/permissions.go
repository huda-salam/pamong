package domain

// Permission identity — dipakai use case lewat ctx.RequirePermission. Tidak ada
// hardcode string di luar konstanta ini (CODE_CONVENTION #8).
const (
	PermPersonBuat       = "identity:person:buat"
	PermEmploymentLampir = "identity:employment:lampir"

	// PermCredentialBuat = membuat kredensial login untuk seorang person (PR-W2).
	// Dipisah dari identity:person:buat karena wewenangnya berbeda jenis: membuat person
	// hanya mencatat seseorang ada, sedangkan membuat kredensial memberi orang itu CARA MASUK.
	// Operator entri data boleh yang pertama tanpa otomatis boleh yang kedua.
	PermCredentialBuat = "identity:credential:buat"

	PermTenantDaftar   = "identity:tenant:daftar"
	PermTenantBaca     = "identity:tenant:baca"
	PermTenantNonaktif = "identity:tenant:nonaktif"

	// PermAssignmentTugaskan = menugaskan employment ke tenant (home tenant).
	// PermAssignmentCrossTenant = tambahan wajib bila penugasan cross-tenant
	// (is_home_tenant=false; mis. PJ Bupati) — sesuai catatan skema id.tenant_assignments.
	PermAssignmentTugaskan    = "identity:assignment:tugaskan"
	PermAssignmentCrossTenant = "identity:assignment:cross_tenant"

	// Role sentral (PR-2.3.2) — dikelola admin platform. Membuat role global/scoped
	// dan menugaskannya ke person adalah mutasi identitas sensitif (ter-audit ADR-003).
	PermCentralRoleBuat   = "identity:central_role:buat"
	PermCentralRoleAssign = "identity:central_role:assign"
)
