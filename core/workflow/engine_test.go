package workflow_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core/workflow"
	"github.com/huda-salam/pamong/port"
	"github.com/huda-salam/pamong/testkit"
)

// ===== Test fixtures =====

// defDisposisi adalah definisi minimal yang mencerminkan pola disposisi.yaml.
var defDisposisi = workflow.WorkflowDefinition{
	ID:            "surat_masuk.disposisi.standar",
	Entity:        "surat_masuk.SuratMasuk",
	Version:       1,
	EffectiveFrom: time.Now(),
	InitialState:  "diterima",
	States: []workflow.State{
		{Name: "diterima", Label: "Diterima agendaris", Actions: []string{"disposisi"}},
		{Name: "didisposisi", Label: "Menunggu tindak lanjut", Actions: []string{"selesai"}},
		{Name: "selesai", Label: "Selesai", IsTerminal: true},
	},
	Transitions: []workflow.Transition{
		{
			From:   "diterima",
			To:     "didisposisi",
			On:     "disposisi",
			Guards: []string{"actor.has_permission('surat_masuk:surat:disposisi')"},
			Action: "DisposisiSurat",
		},
		{
			From: "didisposisi",
			To:   "selesai",
			On:   "selesai",
			// Transisi tanpa action — valid, hanya pindah state.
		},
	},
	AuthoringSource: "developer",
}

// guardAlwaysTrue evaluator yang selalu lolos — untuk test yang tidak menguji guard.
type guardAlwaysTrue struct{}

func (guardAlwaysTrue) Evaluate(_ string, _ port.AuthContext, _ map[string]any) (bool, error) {
	return true, nil
}

// guardAlwaysFalse evaluator yang selalu gagal — untuk test guard block.
type guardAlwaysFalse struct{}

func (guardAlwaysFalse) Evaluate(expr string, _ port.AuthContext, _ map[string]any) (bool, error) {
	return false, nil
}

// dispatchRecord dispatcher yang merekam panggilan dan kembalikan error yang disetting.
type dispatchRecord struct {
	called  []string
	errNext error
}

func (d *dispatchRecord) Dispatch(_ port.AuthContext, action string, _ workflow.WorkflowInstance) error {
	d.called = append(d.called, action)
	if d.errNext != nil {
		err := d.errNext
		d.errNext = nil // hanya sekali
		return err
	}
	return nil
}

// unknownDispatcher dispatcher yang selalu kembalikan ErrActionUnknown.
type unknownDispatcher struct{}

func (unknownDispatcher) Dispatch(_ port.AuthContext, action string, _ workflow.WorkflowInstance) error {
	return workflow.ErrActionUnknown(action)
}

// newEngine membuat engine siap pakai dengan definisi defDisposisi.
func newEngine(t *testing.T, guard workflow.GuardEvaluator, dispatch workflow.ActionDispatcher) *workflow.Engine {
	t.Helper()
	store := workflow.NewMemoryStore()
	if err := store.Register(defDisposisi); err != nil {
		t.Fatalf("register definisi: %v", err)
	}
	return workflow.New(store, dispatch, guard)
}

// actorDenganPermission membuat AuthContext dengan HasRole selalu false,
// RequirePermission selalu nil (berhasil).
func actorDenganPermission(t *testing.T) port.AuthContext {
	t.Helper()
	return testkit.Ctx(t, testkit.WithPersonID(uuid.New()))
}

// ===== Test Engine.Start =====

func TestEngine_Start_SetInitialState(t *testing.T) {
	eng := newEngine(t, guardAlwaysTrue{}, &dispatchRecord{})
	entityID := uuid.New()
	ctx := actorDenganPermission(t)

	inst, err := eng.Start(ctx, defDisposisi.ID, entityID)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if inst.CurrentState != defDisposisi.InitialState {
		t.Errorf("initial state: mau %q, dapat %q", defDisposisi.InitialState, inst.CurrentState)
	}
	if inst.EntityID != entityID {
		t.Errorf("entity ID tidak cocok")
	}
	if inst.DefinitionID != defDisposisi.ID {
		t.Errorf("definition ID tidak cocok")
	}
	if inst.DefinitionVersion != defDisposisi.Version {
		t.Errorf("definition version tidak cocok")
	}
}

func TestEngine_Start_DefinisiTidakAda(t *testing.T) {
	store := workflow.NewMemoryStore()
	eng := workflow.New(store, &dispatchRecord{}, guardAlwaysTrue{})

	_, err := eng.Start(actorDenganPermission(t), "tidak.ada", uuid.New())
	if err == nil {
		t.Fatal("Start dengan definisi tak ada harus gagal")
	}
}

// ===== Test Engine.Execute — transisi valid =====

func TestEngine_Execute_TransisiValid_StateBeruah(t *testing.T) {
	dispatch := &dispatchRecord{}
	eng := newEngine(t, guardAlwaysTrue{}, dispatch)
	ctx := actorDenganPermission(t)

	inst, _ := eng.Start(ctx, defDisposisi.ID, uuid.New())
	if err := eng.Execute(ctx, inst, "disposisi", nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if inst.CurrentState != "didisposisi" {
		t.Errorf("state setelah disposisi: mau %q, dapat %q", "didisposisi", inst.CurrentState)
	}
}

func TestEngine_Execute_TransisiValid_HistoryTercatat(t *testing.T) {
	eng := newEngine(t, guardAlwaysTrue{}, &dispatchRecord{})
	ctx := actorDenganPermission(t)

	inst, _ := eng.Start(ctx, defDisposisi.ID, uuid.New())
	_ = eng.Execute(ctx, inst, "disposisi", nil)

	if len(inst.History) != 1 {
		t.Fatalf("history harus punya 1 record, dapat %d", len(inst.History))
	}
	rec := inst.History[0]
	if rec.From != "diterima" || rec.To != "didisposisi" || rec.Action != "DisposisiSurat" {
		t.Errorf("history record tidak tepat: %+v", rec)
	}
	if rec.ActorID == uuid.Nil {
		t.Error("ActorID harus terisi dari AuthContext")
	}
	if rec.Timestamp.IsZero() {
		t.Error("Timestamp harus terisi")
	}
}

func TestEngine_Execute_TransisiValid_ActionTerpanggil(t *testing.T) {
	dispatch := &dispatchRecord{}
	eng := newEngine(t, guardAlwaysTrue{}, dispatch)
	ctx := actorDenganPermission(t)

	inst, _ := eng.Start(ctx, defDisposisi.ID, uuid.New())
	_ = eng.Execute(ctx, inst, "disposisi", nil)

	if len(dispatch.called) != 1 || dispatch.called[0] != "DisposisiSurat" {
		t.Errorf("dispatcher harus dipanggil dengan DisposisiSurat, dipanggil: %v", dispatch.called)
	}
}

func TestEngine_Execute_TanpaAction_TransisiTetapJalan(t *testing.T) {
	dispatch := &dispatchRecord{}
	eng := newEngine(t, guardAlwaysTrue{}, dispatch)
	ctx := actorDenganPermission(t)

	inst, _ := eng.Start(ctx, defDisposisi.ID, uuid.New())
	_ = eng.Execute(ctx, inst, "disposisi", nil) // state → didisposisi
	// Transisi ke selesai tidak punya action — dispatcher tidak boleh dipanggil lagi.
	dispatchCountBefore := len(dispatch.called)
	if err := eng.Execute(ctx, inst, "selesai", nil); err != nil {
		t.Fatalf("Execute tanpa action: %v", err)
	}
	if inst.CurrentState != "selesai" {
		t.Errorf("state harus selesai, dapat %q", inst.CurrentState)
	}
	if len(dispatch.called) != dispatchCountBefore {
		t.Errorf("dispatcher tidak boleh dipanggil untuk transisi tanpa action")
	}
}

// ===== Test Engine.Execute — transisi ilegal =====

func TestEngine_Execute_TransisiIlegal_AksiTidakAdaDariState(t *testing.T) {
	eng := newEngine(t, guardAlwaysTrue{}, &dispatchRecord{})
	ctx := actorDenganPermission(t)

	inst, _ := eng.Start(ctx, defDisposisi.ID, uuid.New())
	// "selesai" bukan aksi valid dari state "diterima".
	err := eng.Execute(ctx, inst, "selesai", nil)
	if err == nil {
		t.Fatal("Execute aksi tidak valid dari state ini harus gagal")
	}
	if inst.CurrentState != "diterima" {
		t.Errorf("state harus tidak berubah setelah transisi ilegal, dapat %q", inst.CurrentState)
	}
}

func TestEngine_Execute_StateTerminal_Ditolak(t *testing.T) {
	eng := newEngine(t, guardAlwaysTrue{}, &dispatchRecord{})
	ctx := actorDenganPermission(t)

	inst, _ := eng.Start(ctx, defDisposisi.ID, uuid.New())
	_ = eng.Execute(ctx, inst, "disposisi", nil) // → didisposisi
	_ = eng.Execute(ctx, inst, "selesai", nil)   // → selesai (terminal)

	err := eng.Execute(ctx, inst, "apapun", nil)
	if err == nil {
		t.Fatal("Execute dari state terminal harus ditolak")
	}
	if inst.CurrentState != "selesai" {
		t.Errorf("state terminal harus tidak berubah, dapat %q", inst.CurrentState)
	}
}

// ===== Test Engine.Execute — guard =====

func TestEngine_Execute_GuardGagal_Ditolak_StateUnchanged(t *testing.T) {
	eng := newEngine(t, guardAlwaysFalse{}, &dispatchRecord{})
	ctx := actorDenganPermission(t)

	inst, _ := eng.Start(ctx, defDisposisi.ID, uuid.New())
	err := eng.Execute(ctx, inst, "disposisi", nil)
	if err == nil {
		t.Fatal("Execute dengan guard gagal harus ditolak")
	}
	if inst.CurrentState != "diterima" {
		t.Errorf("state harus tidak berubah setelah guard gagal, dapat %q", inst.CurrentState)
	}
	if len(inst.History) != 0 {
		t.Error("history tidak boleh bertambah saat guard gagal")
	}
}

// ===== Test Engine.Execute — action failure =====

func TestEngine_Execute_ActionGagal_TransisiBatal(t *testing.T) {
	dispatch := &dispatchRecord{errNext: errors.New("use case gagal")}
	eng := newEngine(t, guardAlwaysTrue{}, dispatch)
	ctx := actorDenganPermission(t)

	inst, _ := eng.Start(ctx, defDisposisi.ID, uuid.New())
	err := eng.Execute(ctx, inst, "disposisi", nil)
	if err == nil {
		t.Fatal("Execute harus gagal saat action gagal")
	}
	if inst.CurrentState != "diterima" {
		t.Errorf("state harus tidak berubah saat action gagal, dapat %q", inst.CurrentState)
	}
	if len(inst.History) != 0 {
		t.Error("history tidak boleh bertambah saat action gagal")
	}
}

func TestEngine_Execute_ActionTakTerdaftar_Ditolak(t *testing.T) {
	// unknownDispatcher mengembalikan ErrActionUnknown untuk semua action.
	eng := newEngine(t, guardAlwaysTrue{}, unknownDispatcher{})
	ctx := actorDenganPermission(t)

	inst, _ := eng.Start(ctx, defDisposisi.ID, uuid.New())
	err := eng.Execute(ctx, inst, "disposisi", nil)
	if err == nil {
		t.Fatal("Execute dengan action tak terdaftar harus ditolak")
	}
	if inst.CurrentState != "diterima" {
		t.Errorf("state harus tidak berubah saat action tak terdaftar, dapat %q", inst.CurrentState)
	}
}

// ===== Test MemoryStore.Register — validasi definisi =====

func TestStore_Register_TransisiKeStateTakAda_Ditolak(t *testing.T) {
	store := workflow.NewMemoryStore()
	def := workflow.WorkflowDefinition{
		ID:           "test.invalid",
		InitialState: "a",
		States: []workflow.State{
			{Name: "a"},
			{Name: "selesai", IsTerminal: true},
		},
		Transitions: []workflow.Transition{
			{From: "a", To: "TIDAK_ADA", On: "aksi"}, // To tidak ada di States
		},
	}
	if err := store.Register(def); err == nil {
		t.Error("register definisi dengan transisi ke state tak ada harus ditolak")
	}
}

func TestStore_Register_TanpaTerminalState_Ditolak(t *testing.T) {
	store := workflow.NewMemoryStore()
	def := workflow.WorkflowDefinition{
		ID:           "test.noterminal",
		InitialState: "a",
		States: []workflow.State{
			{Name: "a"}, // tidak ada IsTerminal=true
		},
		Transitions: []workflow.Transition{
			{From: "a", To: "a", On: "loop"},
		},
	}
	if err := store.Register(def); err == nil {
		t.Error("register definisi tanpa terminal state harus ditolak")
	}
}

func TestStore_Register_InitialStateTakAda_Ditolak(t *testing.T) {
	store := workflow.NewMemoryStore()
	def := workflow.WorkflowDefinition{
		ID:           "test.badseed",
		InitialState: "TIDAK_ADA",
		States: []workflow.State{
			{Name: "a", IsTerminal: true},
		},
	}
	if err := store.Register(def); err == nil {
		t.Error("register definisi dengan initial_state tak ada harus ditolak")
	}
}
