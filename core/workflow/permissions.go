package workflow

// Permission tata-kelola workflow tenant (CLAUDE.md §Permission: {modul}:{entity}:{aksi}).
// Dikelola admin tenant. Dicek di TemplateChoiceManager.SetChoice via port.AuthContext
// — bukan di-hardcode string di tempat lain (CLAUDE.md aturan #8).
const (
	PermTemplatePilih = "workflow:template:pilih"
)
