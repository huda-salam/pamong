package workflow

import (
	"fmt"

	"github.com/huda-salam/pamong/core"
)

// ErrDefinitionNotFound dipublikasikan saat WorkflowDefinition dengan ID tertentu
// tidak ada di store (HTTP 404).
func ErrDefinitionNotFound(id string) error {
	return core.ErrNotFound("WorkflowDefinition", id)
}

// ErrInvalidDefinition dipublikasikan saat WorkflowDefinition gagal validasi saat
// Register ke store — mis. initial_state tidak ada, transisi merujuk state tak dikenal.
func ErrInvalidDefinition(reason string) error {
	return core.ErrValidation("workflow_definition", reason)
}

// ErrTransitionNotFound dipublikasikan saat tidak ada transisi yang cocok dari
// current_state untuk aksi yang diminta (HTTP 422 — bukan 404: instance ada tapi
// aksi tidak valid di state ini).
func ErrTransitionNotFound(state, action string) error {
	return core.ErrValidation("transition",
		fmt.Sprintf("tidak ada transisi dari state %q untuk aksi %q", state, action))
}

// ErrTerminalState dipublikasikan saat Execute dipanggil pada instance yang sudah
// di state terminal — tidak ada transisi keluar yang mungkin.
func ErrTerminalState(state string) error {
	return core.ErrValidation("state",
		fmt.Sprintf("state %q adalah terminal — tidak bisa melakukan transisi lebih lanjut", state))
}

// ErrGuardFailed dipublikasikan saat satu atau lebih guard expression tidak terpenuhi
// (HTTP 403 — actor tidak memenuhi syarat transisi ini).
func ErrGuardFailed(expr string) error {
	return core.ErrPermissionDenied(fmt.Sprintf("workflow.guard(%s)", expr))
}

// ErrActionUnknown dipublikasikan saat action name di transisi tidak dikenal
// dispatcher — sinyal bahwa use case belum didaftarkan (HTTP 422).
func ErrActionUnknown(action string) error {
	return core.ErrValidation("action",
		fmt.Sprintf("use case %q tidak terdaftar di action dispatcher", action))
}
