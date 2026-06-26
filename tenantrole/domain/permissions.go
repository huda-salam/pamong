package domain

// Permission admin tenant untuk mengelola role tenant. Format {modul}:{entity}:{aksi}
// (CLAUDE.md) — namespace "iam" (identity & access management) menandai kapabilitas
// framework lintas-modul, bukan satu modul bisnis. Selalu dirujuk lewat konstanta ini
// (CODE_CONVENTION #8). Definisi formalnya ke registry manifest menyusul PR-2.3.4.
const (
	// PermTenantRoleBuat — membuat definisi role tenant + grant permission-nya.
	PermTenantRoleBuat = "iam:tenant_role:buat"
	// PermTenantRoleAssign — menugaskan role tenant ke user.
	PermTenantRoleAssign = "iam:tenant_role:assign"
)
