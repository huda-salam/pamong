package workflow_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core/workflow"
	"github.com/huda-salam/pamong/testkit"
)

func TestTemplateChoiceManager_SetChoice_ValidatesAndStampsActor(t *testing.T) {
	defStore := newDefStore(t) // berisi defStandar & defTigaTahap
	tplStore := workflow.NewMemoryTemplateStore(defStore)
	mgr := workflow.NewTemplateChoiceManager(tplStore, defStore)

	actor := uuid.New()
	ctx := testkit.Ctx(t, testkit.WithTenant("tenant-a"), testkit.WithPersonID(actor))

	cfg := workflow.TenantWorkflowConfig{
		TenantID:     "tenant-a",
		Slot:         "surat_masuk.disposisi",
		TemplateID:   defStandar.ID,
		RoleBindings: map[string]string{"validator_tahap_1": "ppk_opd"},
	}
	if err := mgr.SetChoice(ctx, cfg, time.Time{}); err != nil {
		t.Fatalf("SetChoice: %v", err)
	}

	got, err := tplStore.GetTenantConfig("tenant-a", "surat_masuk.disposisi")
	if err != nil {
		t.Fatal(err)
	}
	if got.SetBy == nil || *got.SetBy != actor {
		t.Errorf("SetBy: want %v, got %v", actor, got.SetBy)
	}
	if got.EffectiveFrom.IsZero() {
		t.Error("EffectiveFrom harus terisi")
	}
	if got.TemplateID != defStandar.ID {
		t.Errorf("TemplateID: want %q, got %q", defStandar.ID, got.TemplateID)
	}
}

// DoD butir (d): template_id yang tidak terdaftar ditolak SAAT TULIS, bukan tertunda
// sampai GetForTenant.
func TestTemplateChoiceManager_SetChoice_RejectsUnknownTemplate(t *testing.T) {
	defStore := newDefStore(t)
	tplStore := workflow.NewMemoryTemplateStore(defStore)
	mgr := workflow.NewTemplateChoiceManager(tplStore, defStore)

	ctx := testkit.Ctx(t, testkit.WithTenant("tenant-a"), testkit.WithPersonID(uuid.New()))
	cfg := workflow.TenantWorkflowConfig{
		TenantID:   "tenant-a",
		Slot:       "surat_masuk.disposisi",
		TemplateID: "tidak.ada.template",
	}
	if err := mgr.SetChoice(ctx, cfg, time.Time{}); err == nil {
		t.Fatal("template_id tak terdaftar harus ditolak saat tulis")
	}
	// Tidak ada versi tertulis saat ditolak.
	versions, _ := tplStore.GetTenantConfigVersions("tenant-a", "surat_masuk.disposisi")
	if len(versions) != 0 {
		t.Fatalf("tak boleh ada versi tertulis saat validasi gagal, got %d", len(versions))
	}
}

func TestTemplateChoiceManager_SetChoice_RejectsMissingFields(t *testing.T) {
	defStore := newDefStore(t)
	mgr := workflow.NewTemplateChoiceManager(workflow.NewMemoryTemplateStore(defStore), defStore)
	ctx := testkit.Ctx(t, testkit.WithTenant("tenant-a"), testkit.WithPersonID(uuid.New()))

	if err := mgr.SetChoice(ctx, workflow.TenantWorkflowConfig{
		TenantID: "tenant-a", Slot: "surat_masuk.disposisi", // TemplateID kosong
	}, time.Time{}); err == nil {
		t.Error("template_id kosong harus ditolak")
	}
}
