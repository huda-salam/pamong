package workflow_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/huda-salam/pamong/core/workflow"
	"github.com/huda-salam/pamong/port"
	"github.com/huda-salam/pamong/testkit"
)

// guardSpy merekam map entity yang dilihatnya — dipakai membuktikan bahwa params action TIDAK
// pernah sampai ke guard.
type guardSpy struct {
	sawEntity []map[string]any
}

func (g *guardSpy) Evaluate(_ string, _ port.AuthContext, entity map[string]any) (bool, error) {
	g.sawEntity = append(g.sawEntity, entity)
	return true, nil
}

// ExecuteRequest meneruskan Params ke dispatcher; Execute (pembungkus lama) tidak membawa params.
func TestEngine_ExecuteRequestMeneruskanParamsKeDispatcher(t *testing.T) {
	dispatch := &dispatchRecord{}
	engine := newEngine(t, guardAlwaysTrue{}, dispatch)
	ctx := testkit.Ctx(t)

	inst, err := engine.Start(ctx, defDisposisi.ID, uuid.New())
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	err = engine.ExecuteRequest(ctx, inst, workflow.TransitionRequest{
		Action: "disposisi",
		Params: map[string]any{"kepada_jabatan": "kabag_umum", "instruksi": "tindak lanjuti"},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	if dispatch.lastParams["kepada_jabatan"] != "kabag_umum" {
		t.Fatalf("params tidak sampai ke dispatcher: %+v", dispatch.lastParams)
	}
}

func TestEngine_ExecuteTanpaParamsMengirimNil(t *testing.T) {
	dispatch := &dispatchRecord{}
	engine := newEngine(t, guardAlwaysTrue{}, dispatch)
	ctx := testkit.Ctx(t)

	inst, err := engine.Start(ctx, defDisposisi.ID, uuid.New())
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := engine.Execute(ctx, inst, "disposisi", nil); err != nil {
		t.Fatalf("execute: %v", err)
	}

	if dispatch.lastParams != nil {
		t.Fatalf("params = %+v, ingin nil untuk Execute tanpa argumen", dispatch.lastParams)
	}
}

// Invariant keamanan ADR-022 Keputusan 2: guard hanya melihat SNAPSHOT ENTITY, tak pernah params
// yang dikirim aktor pada request yang sama. Tanpa pemisahan ini aktor bisa menuliskan sendiri
// nilai yang menentukan apakah ia boleh lewat.
func TestEngine_GuardTidakMelihatParamsAktor(t *testing.T) {
	guard := &guardSpy{}
	engine := newEngine(t, guard, &dispatchRecord{})
	ctx := testkit.Ctx(t)

	inst, err := engine.Start(ctx, defDisposisi.ID, uuid.New())
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	err = engine.ExecuteRequest(ctx, inst, workflow.TransitionRequest{
		Action: "disposisi",
		Entity: map[string]any{"nilai": 10},
		Params: map[string]any{"nilai": 999_999},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	if len(guard.sawEntity) == 0 {
		t.Fatal("guard tidak pernah dievaluasi")
	}
	for i, seen := range guard.sawEntity {
		if seen["nilai"] != 10 {
			t.Errorf("guard[%d] melihat nilai %v, ingin 10 (dari Entity, bukan Params)", i, seen["nilai"])
		}
	}
}
