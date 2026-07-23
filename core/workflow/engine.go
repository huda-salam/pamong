package workflow

import (
	"time"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/port"
)

// Engine adalah state machine runner yang mengorkestrasi use case lintas waktu.
// Ia hanya tahu tentang transisi, guard, dan action dispatch — tidak tahu apa yang
// terjadi di dalam satu langkah (itu use case modul).
//
// Engine bersifat stateless terhadap instance: caller menyimpan *WorkflowInstance
// dan meneruskannya ke setiap panggilan Execute. Storage instance ada di PR-3.2.3.
//
// Action di transisi = nama use case (string). Engine memanggilnya lewat
// ActionDispatcher; tidak pernah ada business logic inline di engine.
type Engine struct {
	store    DefinitionStore
	dispatch ActionDispatcher
	guard    GuardEvaluator
}

// New membuat Engine. Semua dependency wajib non-nil.
func New(store DefinitionStore, dispatch ActionDispatcher, guard GuardEvaluator) *Engine {
	return &Engine{store: store, dispatch: dispatch, guard: guard}
}

// Start membuat WorkflowInstance baru untuk entitas tertentu. Instance di-set ke
// initial_state dari definisi; versi definisi dikunci saat ini (perubahan setelah
// start tidak mengubah instance yang sedang berjalan — PRD F1).
func (e *Engine) Start(ctx port.AuthContext, defID string, entityID uuid.UUID) (*WorkflowInstance, error) {
	def, err := e.store.Get(defID)
	if err != nil {
		return nil, err
	}
	return &WorkflowInstance{
		ID:                uuid.New(),
		DefinitionID:      def.ID,
		DefinitionVersion: def.Version,
		EntityID:          entityID,
		CurrentState:      def.InitialState,
		StartedAt:         time.Now(),
	}, nil
}

// Execute menjalankan satu transisi pada instance yang diberikan tanpa komentar.
// Setara ExecuteWithComment(..., "").
func (e *Engine) Execute(ctx port.AuthContext, instance *WorkflowInstance, action string, entity map[string]any) error {
	return e.ExecuteWithComment(ctx, instance, action, entity, "")
}

// ExecuteWithComment menjalankan satu transisi dan menyimpan komentar aktor pada
// TransitionRecord (mis. alasan disposisi/penolakan) — masuk ke riwayat immutable.
//
// Alur: cek terminal → cari transisi → evaluasi guard → dispatch action → update state.
//
// Instance memakai VERSI definisi yang dikunci saat Start (instance.DefinitionVersion),
// bukan versi terbaru — perubahan definisi setelah instance dimulai tidak mengubah alur
// yang sedang berjalan (PRD F1/F7, PR-3.2.7).
//
// Atomicity: bila salah satu langkah gagal, instance dikembalikan ke state semula
// (state tidak berubah, history tidak ditambah). Caller bertanggung jawab persistensi
// setelah Execute sukses.
//
// entity adalah snapshot data bisnis entity saat ini — dipakai guard evaluation
// (mis. `entity.nilai > 100`). Boleh nil bila tidak ada guard yang mengakses entity.
func (e *Engine) ExecuteWithComment(ctx port.AuthContext, instance *WorkflowInstance, action string, entity map[string]any, comment string) error {
	def, err := e.store.GetVersion(instance.DefinitionID, instance.DefinitionVersion)
	if err != nil {
		return err
	}

	// Cari state saat ini — harus ada.
	stateMap := def.stateMap()
	currentState, ok := stateMap[instance.CurrentState]
	if !ok {
		return ErrInvalidDefinition(
			"current_state instance tidak ada di definisi versi yang dikunci")
	}

	// State terminal tidak punya transisi keluar.
	if currentState.IsTerminal {
		return ErrTerminalState(instance.CurrentState)
	}

	// Cari transisi yang cocok: from = currentState, on = action.
	tr, err := e.findTransition(def, instance.CurrentState, action)
	if err != nil {
		return err
	}

	// Evaluasi semua guard (AND). Guard gagal → tolak, state tidak berubah.
	for _, expr := range tr.Guards {
		ok, err := e.guard.Evaluate(expr, ctx, entity)
		if err != nil {
			return err
		}
		if !ok {
			return ErrGuardFailed(expr)
		}
	}

	// Dispatch use case (action). Kosong = tidak ada use case, transisi tetap valid.
	if tr.Action != "" {
		if err := e.dispatch.Dispatch(ctx, tr.Action, *instance); err != nil {
			// Use case gagal → transisi batal; state tidak berubah.
			return err
		}
	}

	// Semua lolos: pindah state dan catat history (immutable, append-only).
	record := TransitionRecord{
		From:      instance.CurrentState,
		To:        tr.To,
		Action:    tr.Action,
		ActorID:   ctx.PersonID(),
		Timestamp: time.Now(),
		Comment:   comment,
	}
	instance.CurrentState = tr.To
	instance.History = append(instance.History, record)
	return nil
}

// findTransition mencari transisi yang cocok: from = fromState, on = action.
// Bila tidak ada → ErrTransitionNotFound (transisi ilegal dari state ini).
func (e *Engine) findTransition(def WorkflowDefinition, fromState, action string) (Transition, error) {
	for _, tr := range def.Transitions {
		if tr.From == fromState && tr.On == action {
			return tr, nil
		}
	}
	return Transition{}, ErrTransitionNotFound(fromState, action)
}
