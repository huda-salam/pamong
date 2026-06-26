package domain

// Permission admin tenant untuk mengelola delegasi/PLT. Format {modul}:{entity}:{aksi}
// (CLAUDE.md) — namespace "iam" (identity & access management) menandai kapabilitas framework
// lintas-modul, bukan satu modul bisnis (selaras tenantrole). Selalu dirujuk lewat konstanta
// ini (CODE_CONVENTION #8).
const (
	// PermDelegasiBuat — membuat delegasi wewenang ke user lain.
	PermDelegasiBuat = "iam:delegasi:buat"
)
