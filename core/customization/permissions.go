package customization

// Permission tata-kelola kustomisasi tenant (CLAUDE.md §Permission: {modul}:{entity}:{aksi}).
// Dikelola admin tenant. Dicek di Manager (jalur tulis) via port.AuthContext.RequirePermission
// — bukan di-hardcode string di tempat lain (CLAUDE.md aturan #8).
const (
	PermCustomFieldBuat  = "customization:custom_field:buat"
	PermCustomFieldHapus = "customization:custom_field:hapus"
	PermLabelUbah        = "customization:label:ubah"
	PermCapabilityUbah   = "customization:capability:ubah"
)
