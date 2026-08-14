package workflow_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/core/workflow"
	"github.com/huda-salam/pamong/port"
	"github.com/huda-salam/pamong/testkit"
)

// actionSpy adalah port.WorkflowAction yang merekam input yang diterimanya.
type actionSpy struct {
	calls []port.WorkflowActionInput
	err   error
}

func (a *actionSpy) RunWorkflowAction(_ port.AuthContext, in port.WorkflowActionInput) error {
	a.calls = append(a.calls, in)
	return a.err
}

func TestActionRegistry_DispatchMeneruskanInstanceDanParams(t *testing.T) {
	spy := &actionSpy{}
	reg := workflow.NewActionRegistry()
	if err := reg.RegisterAction("DisposisiSurat", spy); err != nil {
		t.Fatalf("register action: %v", err)
	}

	inst := workflow.WorkflowInstance{
		ID:       uuid.New(),
		TenantID: "pemkot-surabaya",
		EntityID: uuid.New(),
	}
	params := map[string]any{"kepada_jabatan": "kabag_umum"}

	if err := reg.Dispatch(testkit.Ctx(t), "DisposisiSurat", inst, params); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	if len(spy.calls) != 1 {
		t.Fatalf("action dipanggil %d kali, ingin 1", len(spy.calls))
	}
	got := spy.calls[0]
	if got.TenantID != inst.TenantID || got.InstanceID != inst.ID || got.EntityID != inst.EntityID {
		t.Errorf("identitas instance tidak diteruskan utuh: %+v", got)
	}
	if got.Action != "DisposisiSurat" {
		t.Errorf("Action = %q, ingin DisposisiSurat", got.Action)
	}
	if got.Params["kepada_jabatan"] != "kabag_umum" {
		t.Errorf("params tidak diteruskan: %+v", got.Params)
	}
}

// Action yang gagal harus mengembalikan error ASLINYA — engine memakainya untuk membatalkan
// transisi, jadi membungkusnya jadi error generik akan menghapus sebab yang dilihat klien.
func TestActionRegistry_DispatchMeneruskanErrorAction(t *testing.T) {
	sentinel := core.ErrValidation("kepada_jabatan", "wajib diisi")
	reg := workflow.NewActionRegistry()
	if err := reg.RegisterAction("Gagal", &actionSpy{err: sentinel}); err != nil {
		t.Fatalf("register action: %v", err)
	}

	err := reg.Dispatch(testkit.Ctx(t), "Gagal", workflow.WorkflowInstance{}, nil)
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, ingin error asli dari action", err)
	}
}

func TestActionRegistry_DispatchActionTakDikenal(t *testing.T) {
	reg := workflow.NewActionRegistry()

	err := reg.Dispatch(testkit.Ctx(t), "TidakAda", workflow.WorkflowInstance{}, nil)

	var fe *core.FrameworkError
	if !errors.As(err, &fe) || fe.Code != "VALIDATION_ERROR" {
		t.Fatalf("err = %v, ingin VALIDATION_ERROR (ErrActionUnknown)", err)
	}
}

// Pendaftaran ganda ditolak: pemenangnya kalau tidak akan ditentukan urutan Bootstrap, dan
// transisi tenant memanggil use case modul yang salah tanpa gejala apa pun.
func TestActionRegistry_RegisterMenolakDuplikatDanNil(t *testing.T) {
	reg := workflow.NewActionRegistry()
	if err := reg.RegisterAction("A", &actionSpy{}); err != nil {
		t.Fatalf("register pertama: %v", err)
	}

	if err := reg.RegisterAction("A", &actionSpy{}); err == nil {
		t.Error("pendaftaran nama ganda diterima; ingin ditolak")
	}
	if err := reg.RegisterAction("B", nil); err == nil {
		t.Error("pendaftaran action nil diterima; ingin ditolak")
	}
	if err := reg.RegisterAction("", &actionSpy{}); err == nil {
		t.Error("pendaftaran nama kosong diterima; ingin ditolak")
	}

	if names := reg.Names(); len(names) != 1 || names[0] != "A" {
		t.Errorf("Names() = %v, ingin hanya [A]", names)
	}
}
