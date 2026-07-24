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
//
// deadlines (opsional, PR-3.2.6) mendaftarkan/membatalkan timer SLA saat instance masuk/keluar
// state ber-SLA. nil = SLA nonaktif (engine berperilaku persis seperti sebelum PR-3.2.6).
type Engine struct {
	store     DefinitionStore
	dispatch  ActionDispatcher
	guard     GuardEvaluator
	deadlines DeadlineScheduler
}

// Option mengkonfigurasi Engine saat konstruksi (pola functional option — menambah
// kemampuan opsional tanpa mengubah signature New yang sudah dipakai pemanggil lama).
type Option func(*Engine)

// WithDeadlines memasang DeadlineScheduler agar engine menjadwalkan eskalasi SLA (PRD F6).
// Tanpa opsi ini, state ber-SLAHours>0 tidak menjadwalkan apa pun (engine tenant-agnostik
// tetap; penjadwalan adalah driven port, bukan kewajiban engine inti).
func WithDeadlines(sched DeadlineScheduler) Option {
	return func(e *Engine) { e.deadlines = sched }
}

// New membuat Engine. store, dispatch, guard wajib non-nil; opsi menambah kemampuan opsional.
func New(store DefinitionStore, dispatch ActionDispatcher, guard GuardEvaluator, opts ...Option) *Engine {
	e := &Engine{store: store, dispatch: dispatch, guard: guard}
	for _, o := range opts {
		o(e)
	}
	return e
}

// Start membuat WorkflowInstance baru untuk entitas tertentu. Instance di-set ke
// initial_state dari definisi; versi definisi dikunci saat ini (perubahan setelah
// start tidak mengubah instance yang sedang berjalan — PRD F1).
func (e *Engine) Start(ctx port.AuthContext, defID string, entityID uuid.UUID) (*WorkflowInstance, error) {
	def, err := e.store.Get(defID)
	if err != nil {
		return nil, err
	}
	inst := &WorkflowInstance{
		ID:                uuid.New(),
		TenantID:          ctx.TenantID(),
		DefinitionID:      def.ID,
		DefinitionVersion: def.Version,
		EntityID:          entityID,
		CurrentState:      def.InitialState,
		StartedAt:         time.Now(),
	}
	// Instance MASUK initial_state → jadwalkan deadline SLA bila state itu ber-SLA (PRD F6).
	if err := e.scheduleSLA(ctx, inst, def.stateMap()[def.InitialState]); err != nil {
		return nil, err
	}
	return inst, nil
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
// Atomicity: bila guard/action gagal, instance dikembalikan ke state semula (state tidak
// berubah, history tidak ditambah). Caller bertanggung jawab persistensi setelah Execute
// sukses. Penjadwalan/pembatalan timer SLA terjadi SETELAH transisi tercatat (PR-3.2.6):
// transisi domain sudah otoritatif, sehingga kegagalan scheduler dipropagasi tanpa membatalkan
// transisi — guard race di fire-time menjadi backstop kebenaran.
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
	leftState := instance.CurrentState
	record := TransitionRecord{
		From:      leftState,
		To:        tr.To,
		Action:    tr.Action,
		ActorID:   ctx.PersonID(),
		Timestamp: time.Now(),
		Comment:   comment,
	}
	instance.CurrentState = tr.To
	instance.History = append(instance.History, record)

	// SLA (PRD F6): transisi = instance keluar leftState & masuk tr.To. Batalkan timer state
	// yang ditinggalkan, lalu jadwalkan timer state baru bila ber-SLA. Urutan cancel→schedule
	// menjaga self-loop (From==To) tetap benar: timer lama dibatalkan sebelum yang baru dibuat.
	// Transisi domain sudah OTORITATIF di titik ini; kegagalan penjadwalan SLA dikembalikan
	// sebagai error (agar wiring rusak terlihat) — backstop-nya guard race di fire-time yang
	// meng-no-op-kan deadline basi maupun menutup deadline yang gagal dibatalkan.
	if err := e.cancelSLA(ctx, instance, leftState); err != nil {
		return err
	}
	if err := e.scheduleSLA(ctx, instance, stateMap[tr.To]); err != nil {
		return err
	}
	return nil
}

// scheduleSLA mendaftarkan deadline bila DeadlineScheduler terpasang DAN state ber-SLA
// (SLAHours>0). No-op selain itu. Deadline membawa PERAN eskalasi apa adanya dari definisi —
// engine tak meresolusi peran→orang (itu adapter Escalator, tenant-agnostik).
func (e *Engine) scheduleSLA(ctx port.AuthContext, inst *WorkflowInstance, st State) error {
	if e.deadlines == nil || st.SLAHours <= 0 {
		return nil
	}
	d := Deadline{
		Key:    DeadlineKey(inst.ID, st.Name),
		FireAt: time.Now().Add(time.Duration(st.SLAHours) * time.Hour),
		Escalation: Escalation{
			TenantID:       ctx.TenantID(),
			InstanceID:     inst.ID,
			State:          st.Name,
			EscalateToRole: st.EscalateToRole,
		},
	}
	return e.deadlines.ScheduleDeadline(ctx, d)
}

// cancelSLA membatalkan deadline state yang ditinggalkan. No-op bila scheduler tak terpasang.
// CancelDeadline idempoten — membatalkan key yang tak pernah dijadwalkan (state tanpa SLA)
// bukan error, sehingga pemanggil tak perlu tahu apakah state punya SLA.
func (e *Engine) cancelSLA(ctx port.AuthContext, inst *WorkflowInstance, stateName string) error {
	if e.deadlines == nil {
		return nil
	}
	return e.deadlines.CancelDeadline(ctx, DeadlineKey(inst.ID, stateName))
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
