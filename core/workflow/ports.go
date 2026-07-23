package workflow

import "github.com/huda-salam/pamong/port"

// ActionDispatcher dipanggil Engine untuk mengeksekusi satu use case berdasarkan
// nama (string dari definisi workflow). Implementasi WAJIB memanggil use case nyata —
// tidak ada business logic inline di sini (linter: workflow-action-no-logic).
//
// Action tidak dikenal → kembalikan ErrActionUnknown agar transisi dibatalkan.
// Action gagal (use case return error) → kembalikan error aslinya agar transisi batal.
type ActionDispatcher interface {
	Dispatch(ctx port.AuthContext, action string, instance WorkflowInstance) error
}

// GuardEvaluator mengevaluasi satu ekspresi guard menjadi boolean.
// Implementasi harus stateless dan bebas side-effect — guard hanya membaca konteks,
// tidak memutasi apapun (PRD workflow F5). Syntax error pada ekspresi harus
// dideteksi saat compile (Load/Register), bukan saat Evaluate.
//
// Implementasi produksi: DSLGuardEvaluator (guard.go, PR-3.2.5) — DSL boolean sempit
// dengan compile+cache. Test boleh memakai stub sederhana.
type GuardEvaluator interface {
	Evaluate(expr string, actor port.AuthContext, entity map[string]any) (bool, error)
}

// DefinitionStore menyimpan dan mengambil WorkflowDefinition.
// Register memvalidasi struktur definisi (semua state dirujuk ada, ada terminal state,
// initial_state ada). Implementasi in-memory (PR-3.2.1); DB-backed di PR-3.2.3.
//
// Definisi ber-versi: Register menyimpan versi baru tanpa menghapus yang lama, Get
// mengembalikan versi TERBARU, GetVersion mengembalikan versi spesifik. Engine memakai
// GetVersion agar instance berjalan terkunci ke versi definisi saat Start — perubahan
// definisi setelahnya tidak mengubah instance yang sedang berjalan (PRD F1/F7, PR-3.2.7).
type DefinitionStore interface {
	Register(def WorkflowDefinition) error
	Get(id string) (WorkflowDefinition, error)
	GetVersion(id string, version int) (WorkflowDefinition, error)
}

// TemplateStore mengelola pilihan template workflow per-tenant beserta parameter
// binding peran. Setiap tenant dapat memilih template yang berbeda untuk satu
// "slot" (tipe workflow), dan memetakan peran generik dalam template ke role
// konkret milik tenant tersebut.
//
// GetForTenant adalah entry point utama: mengambil WorkflowDefinition yang tepat
// untuk tenant+slot yang diminta, sudah dengan role binding diterapkan.
// Implementasi in-memory (PR-3.2.4); DB-backed di infra/workflow.DBTemplateStore.
type TemplateStore interface {
	// GetForTenant mengembalikan WorkflowDefinition yang dipilih tenant untuk slot
	// tertentu, dengan role binding diterapkan. Kembalikan ErrTemplateNotConfigured
	// bila tenant belum menetapkan pilihan untuk slot tersebut.
	GetForTenant(tenantID, slot string) (WorkflowDefinition, error)

	// SetTenantTemplate menyimpan atau mengganti pilihan template dan binding milik
	// tenant. Idempoten: panggilan kedua menimpa pilihan sebelumnya.
	SetTenantTemplate(cfg TenantWorkflowConfig) error

	// GetTenantConfig mengembalikan config tersimpan untuk inspeksi (admin UI).
	// Kembalikan ErrTemplateNotConfigured bila belum ada.
	GetTenantConfig(tenantID, slot string) (TenantWorkflowConfig, error)
}
