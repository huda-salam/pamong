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
	roles := testkit.NewMockRoleChecker("ppk_opd")
	mgr := workflow.NewTemplateChoiceManager(tplStore, defStore, roles)

	actor := uuid.New()
	ctx := testkit.Ctx(t, testkit.WithTenant("tenant-a"), testkit.WithPersonID(actor),
		testkit.WithPermission(workflow.PermTemplatePilih))

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
	mgr := workflow.NewTemplateChoiceManager(tplStore, defStore, testkit.NewMockRoleChecker())

	ctx := testkit.Ctx(t, testkit.WithTenant("tenant-a"), testkit.WithPersonID(uuid.New()),
		testkit.WithPermission(workflow.PermTemplatePilih))
	cfg := workflow.TenantWorkflowConfig{
		TenantID:   "tenant-a",
		Slot:       "surat_masuk.disposisi",
		TemplateID: "surat_masuk.disposisi.tidak_ada", // milik slot, tapi tak terdaftar di DefinitionStore
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

// PR-N2 bagian C: RoleBindings yang menunjuk role tak terdaftar di tenant harus ditolak
// SAAT TULIS — sebelum notifikasi hidup sempat mengirim ke peran yang salah.
// PR-N2 code review: RoleChecker tak terpasang (nil) TIDAK BOLEH panic saat RoleBindings
// non-kosong — harus ditolak eksplisit sebagai kesalahan wiring.
func TestTemplateChoiceManager_SetChoice_RoleCheckerNil_RoleBindingsNonKosong_Ditolak(t *testing.T) {
	defStore := newDefStore(t)
	tplStore := workflow.NewMemoryTemplateStore(defStore)
	mgr := workflow.NewTemplateChoiceManager(tplStore, defStore, nil)

	ctx := testkit.Ctx(t, testkit.WithTenant("tenant-a"), testkit.WithPersonID(uuid.New()),
		testkit.WithPermission(workflow.PermTemplatePilih))
	cfg := workflow.TenantWorkflowConfig{
		TenantID:     "tenant-a",
		Slot:         "surat_masuk.disposisi",
		TemplateID:   defStandar.ID,
		RoleBindings: map[string]string{"validator_tahap_1": "ppk_opd"},
	}
	if err := mgr.SetChoice(ctx, cfg, time.Time{}); err == nil {
		t.Fatal("RoleChecker nil + RoleBindings non-kosong harus ditolak, bukan panic atau lolos")
	}
}

// RoleChecker nil TAPI RoleBindings kosong tetap harus jalan — tak ada yang perlu divalidasi.
func TestTemplateChoiceManager_SetChoice_RoleCheckerNil_RoleBindingsKosong_TetapJalan(t *testing.T) {
	defStore := newDefStore(t)
	tplStore := workflow.NewMemoryTemplateStore(defStore)
	mgr := workflow.NewTemplateChoiceManager(tplStore, defStore, nil)

	ctx := testkit.Ctx(t, testkit.WithTenant("tenant-a"), testkit.WithPersonID(uuid.New()),
		testkit.WithPermission(workflow.PermTemplatePilih))
	cfg := workflow.TenantWorkflowConfig{
		TenantID:   "tenant-a",
		Slot:       "surat_masuk.disposisi",
		TemplateID: defStandar.ID,
	}
	if err := mgr.SetChoice(ctx, cfg, time.Time{}); err != nil {
		t.Fatalf("RoleChecker nil tanpa RoleBindings harus tetap sukses: %v", err)
	}
}

func TestTemplateChoiceManager_SetChoice_RejectsUnknownRoleBinding(t *testing.T) {
	defStore := newDefStore(t)
	tplStore := workflow.NewMemoryTemplateStore(defStore)
	roles := testkit.NewMockRoleChecker("ppk_opd") // "role_siluman" TIDAK dikenal
	mgr := workflow.NewTemplateChoiceManager(tplStore, defStore, roles)

	ctx := testkit.Ctx(t, testkit.WithTenant("tenant-a"), testkit.WithPersonID(uuid.New()),
		testkit.WithPermission(workflow.PermTemplatePilih))
	cfg := workflow.TenantWorkflowConfig{
		TenantID:   "tenant-a",
		Slot:       "surat_masuk.disposisi",
		TemplateID: defStandar.ID,
		RoleBindings: map[string]string{
			"validator_tahap_1": "ppk_opd",      // dikenal
			"validator_sla":     "role_siluman", // TIDAK dikenal
		},
	}
	if err := mgr.SetChoice(ctx, cfg, time.Time{}); err == nil {
		t.Fatal("RoleBindings yang menunjuk role tak terdaftar harus ditolak")
	}
	versions, _ := tplStore.GetTenantConfigVersions("tenant-a", "surat_masuk.disposisi")
	if len(versions) != 0 {
		t.Fatalf("tak boleh ada versi tertulis saat validasi RoleBindings gagal, got %d", len(versions))
	}
}

func TestTemplateChoiceManager_SetChoice_RejectsMissingFields(t *testing.T) {
	defStore := newDefStore(t)
	mgr := workflow.NewTemplateChoiceManager(workflow.NewMemoryTemplateStore(defStore), defStore, testkit.NewMockRoleChecker())
	ctx := testkit.Ctx(t, testkit.WithTenant("tenant-a"), testkit.WithPersonID(uuid.New()),
		testkit.WithPermission(workflow.PermTemplatePilih))

	if err := mgr.SetChoice(ctx, workflow.TenantWorkflowConfig{
		TenantID: "tenant-a", Slot: "surat_masuk.disposisi", // TemplateID kosong
	}, time.Time{}); err == nil {
		t.Error("template_id kosong harus ditolak")
	}
}

// ===== PR-3.3.2b butir (c): permission, tenant-from-token, slot ownership =====

func TestTemplateChoiceManager_SetChoice_PermissionDenied(t *testing.T) {
	defStore := newDefStore(t)
	mgr := workflow.NewTemplateChoiceManager(workflow.NewMemoryTemplateStore(defStore), defStore, testkit.NewMockRoleChecker())
	// Tanpa WithPermission(PermTemplatePilih).
	ctx := testkit.Ctx(t, testkit.WithTenant("tenant-a"), testkit.WithPersonID(uuid.New()))

	cfg := workflow.TenantWorkflowConfig{
		TenantID:   "tenant-a",
		Slot:       "surat_masuk.disposisi",
		TemplateID: defStandar.ID,
	}
	if err := mgr.SetChoice(ctx, cfg, time.Time{}); err == nil {
		t.Fatal("actor tanpa PermTemplatePilih harus ditolak")
	}
}

func TestTemplateChoiceManager_SetChoice_TenantDipaksaDariToken(t *testing.T) {
	defStore := newDefStore(t)
	tplStore := workflow.NewMemoryTemplateStore(defStore)
	mgr := workflow.NewTemplateChoiceManager(tplStore, defStore, testkit.NewMockRoleChecker())
	ctx := testkit.Ctx(t, testkit.WithTenant("tenant-token"), testkit.WithPersonID(uuid.New()),
		testkit.WithPermission(workflow.PermTemplatePilih))

	// cfg.TenantID mencoba menulis ke tenant lain — harus diabaikan, dipaksa dari token.
	cfg := workflow.TenantWorkflowConfig{
		TenantID:   "tenant-lain",
		Slot:       "surat_masuk.disposisi",
		TemplateID: defStandar.ID,
	}
	if err := mgr.SetChoice(ctx, cfg, time.Time{}); err != nil {
		t.Fatalf("SetChoice: %v", err)
	}

	if _, err := tplStore.GetTenantConfig("tenant-lain", "surat_masuk.disposisi"); err == nil {
		t.Fatal("tidak boleh ada config tertulis di tenant-lain")
	}
	got, err := tplStore.GetTenantConfig("tenant-token", "surat_masuk.disposisi")
	if err != nil {
		t.Fatalf("GetTenantConfig tenant-token: %v", err)
	}
	if got.TemplateID != defStandar.ID {
		t.Errorf("TemplateID: want %q, got %q", defStandar.ID, got.TemplateID)
	}
}

func TestTemplateChoiceManager_SetChoice_SlotMismatch(t *testing.T) {
	defStore := newDefStore(t)
	mgr := workflow.NewTemplateChoiceManager(workflow.NewMemoryTemplateStore(defStore), defStore, testkit.NewMockRoleChecker())
	ctx := testkit.Ctx(t, testkit.WithTenant("tenant-a"), testkit.WithPersonID(uuid.New()),
		testkit.WithPermission(workflow.PermTemplatePilih))

	// TemplateID milik slot "surat_masuk.disposisi", dirujuk dari slot lain.
	cfg := workflow.TenantWorkflowConfig{
		TenantID:   "tenant-a",
		Slot:       "keuangan.spm",
		TemplateID: defStandar.ID,
	}
	if err := mgr.SetChoice(ctx, cfg, time.Time{}); err == nil {
		t.Fatal("template_id yang bukan milik slot harus ditolak")
	}
}

// TestTemplateChoiceManager_SetChoice_SlotBertingkat_Ditolak membuktikan bahwa prefix
// string SAJA tidak cukup: slot "keuangan.spm" adalah prefix dari template milik slot
// bertingkat "keuangan.spm.lanjutan" yang berbeda — wajib tetap ditolak (bukan lolos
// karena kebetulan string-prefix cocok).
func TestTemplateChoiceManager_SetChoice_SlotBertingkat_Ditolak(t *testing.T) {
	defStore := newDefStore(t)
	mgr := workflow.NewTemplateChoiceManager(workflow.NewMemoryTemplateStore(defStore), defStore, testkit.NewMockRoleChecker())
	ctx := testkit.Ctx(t, testkit.WithTenant("tenant-a"), testkit.WithPersonID(uuid.New()),
		testkit.WithPermission(workflow.PermTemplatePilih))

	cfg := workflow.TenantWorkflowConfig{
		TenantID:   "tenant-a",
		Slot:       "keuangan.spm",
		TemplateID: "keuangan.spm.lanjutan.standar", // milik slot "keuangan.spm.lanjutan", bukan "keuangan.spm"
	}
	if err := mgr.SetChoice(ctx, cfg, time.Time{}); err == nil {
		t.Fatal("template_id milik slot bertingkat yang lebih spesifik harus ditolak, bukan lolos lewat string-prefix")
	}
}
