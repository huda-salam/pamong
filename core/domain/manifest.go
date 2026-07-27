package domain

// Manifest mendeklarasikan identitas, kemampuan, dan dependency sebuah modul bisnis.
// Ini titik tunggal yang dibaca registry — tidak ada penemuan implisit.
type Manifest struct {
	Name    string
	Version string
	Domain  string

	// DependsOn mendaftarkan modul lain yang wajib di-bootstrap lebih dulu.
	// Dependency harus membentuk DAG; siklus ditolak pamongctl validate modules.
	DependsOn []string

	// DataLifecycle: "annual_cutoff" atau "continuous" (lihat CLAUDE.md #12).
	DataLifecycle string

	Entities    []EntityDef
	Events      EventManifest
	Permissions PermissionManifest
	Workflows   []WorkflowRef
	Config      ConfigSpec
}

// EventManifest mendaftarkan event yang diproduksi dan dikonsumsi modul.
type EventManifest struct {
	Produces []EventDef
	Consumes []EventSubscription
}

// EventDef mendefinisikan satu event yang dipublikasikan modul.
// Schema divalidasi event bus saat publish; perubahan Schema menaikkan versi.
type EventDef struct {
	Name   string
	Schema any // contoh struct payload; dipakai untuk validasi schema
}

// EventSubscription mendaftarkan event dari modul lain yang ingin dikonsumsi.
type EventSubscription struct {
	Event   string
	Handler string // nama method di adapter/event/consumer.go
}

// PermissionManifest mendaftarkan semua permission yang didefinisikan modul.
type PermissionManifest struct {
	Groups  []PermissionGroup
	Exports []string // permission yang bisa dirujuk modul lain
	Imports []PermissionImport
}

// PermissionGroup mengelompokkan permission untuk kemudahan assignment ke role.
type PermissionGroup struct {
	Name        string
	Label       string
	Permissions []PermissionDef
}

// PermissionDef adalah satu entri permission dengan format {modul}:{entity}:{aksi}.
type PermissionDef struct {
	Name  string
	Label string

	// Strict menandai permission yang tunduk pada resolusi INTERSECTION antar role
	// non-global (segregation of duties): izin hanya bila SEMUA role non-global yang
	// dipegang actor memberi perm — memegang satu role yang tak memberinya memblokir.
	// Default false = permission biasa (union). Manifest adalah satu-satunya titik
	// deklarasi strict; dikumpulkan saat boot (Registry.StrictPermissions) dan disuntik
	// ke permission.Engine. Semantik intersection sudah diputus F7/PRD (lihat
	// core/permission/engine.go) & ADR-014 — field ini hanya titik deklarasinya.
	Strict bool
}

// PermissionImport mendaftarkan permission modul lain yang dipakai modul ini.
// Tanpa entri ini, pemakaian permission modul lain ditolak linter.
type PermissionImport struct {
	From       string
	Permission string
}

// WorkflowRef menunjuk ke file YAML definisi workflow baseline modul.
type WorkflowRef struct {
	Path string
}
