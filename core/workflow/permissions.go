package workflow

// Permission tata-kelola workflow tenant (CLAUDE.md §Permission: {modul}:{entity}:{aksi}).
// Dikelola admin tenant. Dicek di TemplateChoiceManager.SetChoice via port.AuthContext
// — bukan di-hardcode string di tempat lain (CLAUDE.md aturan #8).
const (
	PermTemplatePilih = "workflow:template:pilih"

	// Permission permukaan runtime workflow (PR-W4a). Ia BUKAN pengganti guard di definisi:
	// gerbang ini menjawab "boleh memakai mesin workflow di tenant ini", sedangkan guard
	// menjawab "boleh melakukan transisi INI pada alur ini" (mis.
	// actor.has_permission('surat_masuk:surat:disposisi')). Keduanya berlaku berurutan —
	// menghapus salah satunya membuat lapis yang tersisa menjawab pertanyaan yang bukan
	// tugasnya.
	PermInstanceMulai    = "workflow:instance:mulai"
	PermInstanceTransisi = "workflow:instance:transisi"
	PermInstanceBaca     = "workflow:instance:baca"
)
