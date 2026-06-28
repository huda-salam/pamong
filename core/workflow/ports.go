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
// PR-3.2.1: implementasi minimal untuk test. DSL penuh (aritmatika, field entity,
// operator boolean) dibangun di PR-3.2.5.
type GuardEvaluator interface {
	Evaluate(expr string, actor port.AuthContext, entity map[string]any) (bool, error)
}

// DefinitionStore menyimpan dan mengambil WorkflowDefinition.
// Register memvalidasi struktur definisi (semua state dirujuk ada, ada terminal state,
// initial_state ada). Implementasi in-memory (PR-3.2.1); DB-backed di PR-3.2.3.
type DefinitionStore interface {
	Register(def WorkflowDefinition) error
	Get(id string) (WorkflowDefinition, error)
}
