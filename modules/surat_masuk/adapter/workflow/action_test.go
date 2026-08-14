package workflow_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/huda-salam/pamong/core"
	wfadapter "github.com/huda-salam/pamong/modules/surat_masuk/adapter/workflow"
	smdomain "github.com/huda-salam/pamong/modules/surat_masuk/domain"
	"github.com/huda-salam/pamong/modules/surat_masuk/usecase"
	"github.com/huda-salam/pamong/port"
	"github.com/huda-salam/pamong/testkit"
)

// suratRepoStub mengembalikan satu surat yang sudah ada; disposisiRepoSpy merekam yang disimpan.
type suratRepoStub struct{ id uuid.UUID }

func (s suratRepoStub) FindByID(_ context.Context, id uuid.UUID) (*smdomain.SuratMasuk, error) {
	if id != s.id {
		return nil, core.ErrNotFound("SuratMasuk", id.String())
	}
	return &smdomain.SuratMasuk{ID: id}, nil
}
func (suratRepoStub) Save(context.Context, *smdomain.SuratMasuk) error   { return nil }
func (suratRepoStub) Update(context.Context, *smdomain.SuratMasuk) error { return nil }

type disposisiRepoSpy struct{ saved []*smdomain.Disposisi }

func (d *disposisiRepoSpy) Save(_ context.Context, x *smdomain.Disposisi) error {
	d.saved = append(d.saved, x)
	return nil
}

func (d *disposisiRepoSpy) ListBySurat(context.Context, uuid.UUID) ([]*smdomain.Disposisi, error) {
	return nil, nil
}
func newAction(t *testing.T, suratID uuid.UUID) (*wfadapter.DisposisiAction, *disposisiRepoSpy) {
	t.Helper()
	spy := &disposisiRepoSpy{}
	uc := usecase.NewDisposisiSurat(suratRepoStub{id: suratID}, spy,
		testkit.NewMockUserResolver(), testkit.NewMockPublisher())
	return wfadapter.NewDisposisiAction(uc), spy
}

// Params aktor dipetakan ke input use case; SURAT diambil dari EntityID instance, bukan params —
// kalau tidak, aktor bisa mendisposisi surat lain lewat instance yang boleh ia sentuh.
func TestDisposisiAction_MemetakanParamsDanEntityID(t *testing.T) {
	suratID := uuid.New()
	action, spy := newAction(t, suratID)
	ctx := testkit.Ctx(t, testkit.WithPermission(smdomain.PermSuratDisposisi))

	err := action.RunWorkflowAction(ctx, port.WorkflowActionInput{
		EntityID: suratID,
		Action:   "DisposisiSurat",
		Params: map[string]any{
			"kepada_jabatan": "kabag_umum",
			"instruksi":      "tindak lanjuti",
			// Upaya menyelundupkan surat lain lewat params harus TIDAK berpengaruh.
			"surat_id":  uuid.New().String(),
			"entity_id": uuid.New().String(),
		},
	})
	if err != nil {
		t.Fatalf("RunWorkflowAction: %v", err)
	}

	if len(spy.saved) != 1 {
		t.Fatalf("disposisi tersimpan %d, ingin 1", len(spy.saved))
	}
	got := spy.saved[0]
	if got.SuratID != suratID {
		t.Errorf("SuratID = %v, ingin dari EntityID instance (%v)", got.SuratID, suratID)
	}
	if got.KepadaJabatan != "kabag_umum" || got.Instruksi != "tindak lanjuti" {
		t.Errorf("params tak terpetakan: %+v", got)
	}
}

// Params wajib yang kosong = error transisi (engine membatalkan transisi), bukan disposisi
// tanpa tujuan yang tersimpan diam-diam.
func TestDisposisiAction_ParamsWajibKosong(t *testing.T) {
	suratID := uuid.New()
	action, spy := newAction(t, suratID)
	ctx := testkit.Ctx(t, testkit.WithPermission(smdomain.PermSuratDisposisi))

	cases := map[string]map[string]any{
		"tanpa kepada_jabatan":        {"instruksi": "x"},
		"kepada_jabatan kosong":       {"kepada_jabatan": ""},
		"kepada_jabatan bukan string": {"kepada_jabatan": 42},
		"params nil":                  nil,
	}
	for nama, params := range cases {
		t.Run(nama, func(t *testing.T) {
			err := action.RunWorkflowAction(ctx, port.WorkflowActionInput{
				EntityID: suratID, Action: "DisposisiSurat", Params: params,
			})
			var fe *core.FrameworkError
			if !errors.As(err, &fe) || fe.Code != "VALIDATION_ERROR" {
				t.Fatalf("err = %v, ingin VALIDATION_ERROR", err)
			}
		})
	}
	if len(spy.saved) != 0 {
		t.Fatalf("%d disposisi tersimpan meski params tak sah", len(spy.saved))
	}
}

// Permission tetap ditegakkan use case saat dipanggil lewat workflow — action bukan jalan pintas
// yang melewati gerbang yang berlaku untuk panggilan langsung.
func TestDisposisiAction_PermissionTetapDitegakkan(t *testing.T) {
	suratID := uuid.New()
	action, spy := newAction(t, suratID)
	ctx := testkit.Ctx(t) // tanpa permission disposisi

	err := action.RunWorkflowAction(ctx, port.WorkflowActionInput{
		EntityID: suratID, Action: "DisposisiSurat",
		Params: map[string]any{"kepada_jabatan": "kabag_umum"},
	})

	var fe *core.FrameworkError
	if !errors.As(err, &fe) || fe.Code != "PERMISSION_DENIED" {
		t.Fatalf("err = %v, ingin PERMISSION_DENIED", err)
	}
	if len(spy.saved) != 0 {
		t.Fatal("disposisi tersimpan meski permission ditolak")
	}
}
