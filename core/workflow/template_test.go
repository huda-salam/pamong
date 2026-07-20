package workflow_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core/workflow"
)

// ===== Fixtures =====

// defStandar adalah template 2-tahap: diterima → didisposisi → selesai.
var defStandar = workflow.WorkflowDefinition{
	ID:              "surat_masuk.disposisi.standar",
	Entity:          "surat_masuk.SuratMasuk",
	Version:         1,
	EffectiveFrom:   time.Now(),
	InitialState:    "diterima",
	AuthoringSource: "developer",
	States: []workflow.State{
		{Name: "diterima", Label: "Diterima agendaris"},
		{Name: "didisposisi", Label: "Menunggu tindak lanjut",
			EscalateToRole: "validator_sla"},
		{Name: "selesai", Label: "Selesai", IsTerminal: true},
	},
	Transitions: []workflow.Transition{
		{
			From:   "diterima",
			To:     "didisposisi",
			On:     "disposisi",
			Action: "DisposisiSurat",
			Notify: &workflow.NotifySpec{ToRole: "validator_tahap_1", Template: "notif_disposisi"},
		},
		{
			From: "didisposisi",
			To:   "selesai",
			On:   "selesai",
		},
	},
}

// defTigaTahap adalah template 3-tahap: diterima → validasi → persetujuan → selesai.
var defTigaTahap = workflow.WorkflowDefinition{
	ID:              "surat_masuk.disposisi.tiga_tahap",
	Entity:          "surat_masuk.SuratMasuk",
	Version:         1,
	EffectiveFrom:   time.Now(),
	InitialState:    "diterima",
	AuthoringSource: "developer",
	States: []workflow.State{
		{Name: "diterima", Label: "Diterima agendaris"},
		{Name: "validasi", Label: "Menunggu validasi"},
		{Name: "persetujuan", Label: "Menunggu persetujuan",
			EscalateToRole: "validator_sla"},
		{Name: "selesai", Label: "Selesai", IsTerminal: true},
	},
	Transitions: []workflow.Transition{
		{From: "diterima", To: "validasi", On: "validasi", Action: "DisposisiSurat"},
		{From: "validasi", To: "persetujuan", On: "setujui",
			Notify: &workflow.NotifySpec{ToRole: "validator_tahap_1", Template: "notif_persetujuan"}},
		{From: "persetujuan", To: "selesai", On: "selesai"},
	},
}

// newDefStore membuat MemoryStore berisi defStandar dan defTigaTahap.
func newDefStore(t *testing.T) workflow.DefinitionStore {
	t.Helper()
	store := workflow.NewMemoryStore()
	if err := store.Register(defStandar); err != nil {
		t.Fatalf("register defStandar: %v", err)
	}
	if err := store.Register(defTigaTahap); err != nil {
		t.Fatalf("register defTigaTahap: %v", err)
	}
	return store
}

// ===== ApplyBindings =====

func TestApplyBindings_TidakAdaBinding_KembalikanSalinan(t *testing.T) {
	got := workflow.ApplyBindings(defStandar, nil)
	if got.Transitions[0].Notify.ToRole != "validator_tahap_1" {
		t.Errorf("ToRole harus tidak berubah, dapat %q", got.Transitions[0].Notify.ToRole)
	}
	if got.States[1].EscalateToRole != "validator_sla" {
		t.Errorf("EscalateToRole harus tidak berubah, dapat %q", got.States[1].EscalateToRole)
	}
}

func TestApplyBindings_GantiToRole(t *testing.T) {
	bindings := map[string]string{
		"validator_tahap_1": "ppk_opd",
	}
	got := workflow.ApplyBindings(defStandar, bindings)

	if got.Transitions[0].Notify.ToRole != "ppk_opd" {
		t.Errorf("ToRole: mau %q, dapat %q", "ppk_opd", got.Transitions[0].Notify.ToRole)
	}
	// Peran yang tidak ada di binding tetap asli.
	if got.States[1].EscalateToRole != "validator_sla" {
		t.Errorf("EscalateToRole tidak di-bind harus tetap, dapat %q", got.States[1].EscalateToRole)
	}
}

func TestApplyBindings_GantiEscalateToRole(t *testing.T) {
	bindings := map[string]string{
		"validator_sla": "kepala_dinas",
	}
	got := workflow.ApplyBindings(defStandar, bindings)

	if got.States[1].EscalateToRole != "kepala_dinas" {
		t.Errorf("EscalateToRole: mau %q, dapat %q", "kepala_dinas", got.States[1].EscalateToRole)
	}
}

func TestApplyBindings_TidakMutasiDefinisiAsli(t *testing.T) {
	originalToRole := defStandar.Transitions[0].Notify.ToRole
	bindings := map[string]string{"validator_tahap_1": "ppk_opd"}
	_ = workflow.ApplyBindings(defStandar, bindings)

	if defStandar.Transitions[0].Notify.ToRole != originalToRole {
		t.Error("ApplyBindings tidak boleh mutasi definisi asli")
	}
}

// ===== MemoryTemplateStore.SetTenantTemplate =====

func TestMemoryTemplateStore_Set_FieldWajibKosong_Error(t *testing.T) {
	store := workflow.NewMemoryTemplateStore(newDefStore(t))

	cases := []workflow.TenantWorkflowConfig{
		{Slot: "surat_masuk.disposisi", TemplateID: "x"},      // tanpa TenantID
		{TenantID: "tenant-a", TemplateID: "x"},               // tanpa Slot
		{TenantID: "tenant-a", Slot: "surat_masuk.disposisi"}, // tanpa TemplateID
	}
	for _, cfg := range cases {
		if err := store.SetTenantTemplate(cfg); err == nil {
			t.Errorf("SetTenantTemplate(%+v) harus error", cfg)
		}
	}
}

func TestMemoryTemplateStore_Set_Idempoten_TimpaPilihan(t *testing.T) {
	store := workflow.NewMemoryTemplateStore(newDefStore(t))

	cfg1 := workflow.TenantWorkflowConfig{
		TenantID:   "tenant-a",
		Slot:       "surat_masuk.disposisi",
		TemplateID: defStandar.ID,
	}
	cfg2 := workflow.TenantWorkflowConfig{
		TenantID:   "tenant-a",
		Slot:       "surat_masuk.disposisi",
		TemplateID: defTigaTahap.ID,
	}
	if err := store.SetTenantTemplate(cfg1); err != nil {
		t.Fatalf("set pertama: %v", err)
	}
	if err := store.SetTenantTemplate(cfg2); err != nil {
		t.Fatalf("set kedua: %v", err)
	}
	got, _ := store.GetTenantConfig("tenant-a", "surat_masuk.disposisi")
	if got.TemplateID != defTigaTahap.ID {
		t.Errorf("pilihan seharusnya ditimpa ke tiga_tahap, dapat %q", got.TemplateID)
	}
}

// ===== MemoryTemplateStore.GetTenantConfig =====

func TestMemoryTemplateStore_GetConfig_BelumAda_Error(t *testing.T) {
	store := workflow.NewMemoryTemplateStore(newDefStore(t))

	_, err := store.GetTenantConfig("tenant-x", "surat_masuk.disposisi")
	if err == nil {
		t.Fatal("GetTenantConfig untuk config belum ada harus error")
	}
}

// Store tidak boleh berbagi map bindings dengan caller: memutasi map yang dikirim
// ke Set atau yang dikembalikan Get tidak boleh mengubah isi store.
func TestMemoryTemplateStore_RoleBindings_TerisolasiDariCaller(t *testing.T) {
	store := workflow.NewMemoryTemplateStore(newDefStore(t))
	bindings := map[string]string{"validator_tahap_1": "ppk_opd"}
	err := store.SetTenantTemplate(workflow.TenantWorkflowConfig{
		TenantID:     "tenant-a",
		Slot:         "surat_masuk.disposisi",
		TemplateID:   defStandar.ID,
		RoleBindings: bindings,
	})
	if err != nil {
		t.Fatalf("SetTenantTemplate: %v", err)
	}

	bindings["validator_tahap_1"] = "diretas_lewat_map_caller"

	got, err := store.GetTenantConfig("tenant-a", "surat_masuk.disposisi")
	if err != nil {
		t.Fatalf("GetTenantConfig: %v", err)
	}
	if got.RoleBindings["validator_tahap_1"] != "ppk_opd" {
		t.Errorf("mutasi map caller bocor ke store, dapat %q",
			got.RoleBindings["validator_tahap_1"])
	}

	got.RoleBindings["validator_tahap_1"] = "diretas_lewat_map_hasil_get"

	ulang, err := store.GetTenantConfig("tenant-a", "surat_masuk.disposisi")
	if err != nil {
		t.Fatalf("GetTenantConfig ulang: %v", err)
	}
	if ulang.RoleBindings["validator_tahap_1"] != "ppk_opd" {
		t.Errorf("mutasi map hasil Get bocor ke store, dapat %q",
			ulang.RoleBindings["validator_tahap_1"])
	}
}

func TestMemoryTemplateStore_GetConfig_SetByDisimpan(t *testing.T) {
	store := workflow.NewMemoryTemplateStore(newDefStore(t))
	aktorID := uuid.New()
	cfg := workflow.TenantWorkflowConfig{
		TenantID:   "tenant-a",
		Slot:       "surat_masuk.disposisi",
		TemplateID: defStandar.ID,
		SetBy:      &aktorID,
	}
	_ = store.SetTenantTemplate(cfg)

	got, err := store.GetTenantConfig("tenant-a", "surat_masuk.disposisi")
	if err != nil {
		t.Fatalf("GetTenantConfig: %v", err)
	}
	if got.SetBy == nil || *got.SetBy != aktorID {
		t.Errorf("SetBy tidak tersimpan, dapat %v", got.SetBy)
	}
}

// ===== DoD utama: dua tenant, template berbeda, use case identik =====

// TestDuaTenant_TemplateBerbeda_UseCaseIdentik membuktikan bahwa:
//   - Tenant A menggunakan template standar (2-tahap)
//   - Tenant B menggunakan template tiga_tahap (3-tahap)
//   - Engine memulai instance via definisi yang berbeda per-tenant
//   - Keduanya memanggil use case yang sama ("DisposisiSurat") lewat ActionDispatcher
//     yang identik — bisnis logic tidak digandakan
func TestDuaTenant_TemplateBerbeda_UseCaseIdentik(t *testing.T) {
	defStore := newDefStore(t)
	tplStore := workflow.NewMemoryTemplateStore(defStore)

	// Tenant A memilih template standar (2-tahap).
	_ = tplStore.SetTenantTemplate(workflow.TenantWorkflowConfig{
		TenantID:   "tenant-a",
		Slot:       "surat_masuk.disposisi",
		TemplateID: defStandar.ID,
	})
	// Tenant B memilih template tiga_tahap (3-tahap).
	_ = tplStore.SetTenantTemplate(workflow.TenantWorkflowConfig{
		TenantID:   "tenant-b",
		Slot:       "surat_masuk.disposisi",
		TemplateID: defTigaTahap.ID,
	})

	// Resolusi per-tenant: definisi berbeda.
	defA, err := tplStore.GetForTenant("tenant-a", "surat_masuk.disposisi")
	if err != nil {
		t.Fatalf("GetForTenant tenant-a: %v", err)
	}
	defB, err := tplStore.GetForTenant("tenant-b", "surat_masuk.disposisi")
	if err != nil {
		t.Fatalf("GetForTenant tenant-b: %v", err)
	}

	if defA.ID != defStandar.ID {
		t.Errorf("tenant-a harus pakai standar, dapat %q", defA.ID)
	}
	if defB.ID != defTigaTahap.ID {
		t.Errorf("tenant-b harus pakai tiga_tahap, dapat %q", defB.ID)
	}

	// Engine yang sama (use case identik) dipakai keduanya.
	eng := workflow.New(defStore, &dispatchRecord{}, guardAlwaysTrue{})

	entityA := uuid.New()
	entityB := uuid.New()

	instA, err := eng.Start(actorDenganPermission(t), defA.ID, entityA)
	if err != nil {
		t.Fatalf("Start tenant-a: %v", err)
	}
	instB, err := eng.Start(actorDenganPermission(t), defB.ID, entityB)
	if err != nil {
		t.Fatalf("Start tenant-b: %v", err)
	}

	// Tenant A: aksi pertama = "disposisi" → ke "didisposisi".
	if err := eng.Execute(actorDenganPermission(t), instA, "disposisi", nil); err != nil {
		t.Fatalf("Execute tenant-a disposisi: %v", err)
	}
	if instA.CurrentState != "didisposisi" {
		t.Errorf("tenant-a state: mau didisposisi, dapat %q", instA.CurrentState)
	}

	// Tenant B: aksi pertama = "validasi" → ke "validasi".
	if err := eng.Execute(actorDenganPermission(t), instB, "validasi", nil); err != nil {
		t.Fatalf("Execute tenant-b validasi: %v", err)
	}
	if instB.CurrentState != "validasi" {
		t.Errorf("tenant-b state: mau validasi, dapat %q", instB.CurrentState)
	}
}

// TestDuaTenant_RoleBinding_Diterapkan membuktikan bahwa role binding per-tenant
// mengubah ToRole pada Notify dan EscalateToRole pada State.
func TestDuaTenant_RoleBinding_Diterapkan(t *testing.T) {
	tplStore := workflow.NewMemoryTemplateStore(newDefStore(t))

	_ = tplStore.SetTenantTemplate(workflow.TenantWorkflowConfig{
		TenantID:   "tenant-a",
		Slot:       "surat_masuk.disposisi",
		TemplateID: defStandar.ID,
		RoleBindings: map[string]string{
			"validator_tahap_1": "ppk_opd",
			"validator_sla":     "kepala_dinas",
		},
	})
	_ = tplStore.SetTenantTemplate(workflow.TenantWorkflowConfig{
		TenantID:   "tenant-b",
		Slot:       "surat_masuk.disposisi",
		TemplateID: defStandar.ID,
		RoleBindings: map[string]string{
			"validator_tahap_1": "kabag_umum",
			"validator_sla":     "direktur",
		},
	})

	defA, _ := tplStore.GetForTenant("tenant-a", "surat_masuk.disposisi")
	defB, _ := tplStore.GetForTenant("tenant-b", "surat_masuk.disposisi")

	// Tenant A: binding ke ppk_opd & kepala_dinas
	if got := defA.Transitions[0].Notify.ToRole; got != "ppk_opd" {
		t.Errorf("tenant-a Notify.ToRole: mau ppk_opd, dapat %q", got)
	}
	if got := defA.States[1].EscalateToRole; got != "kepala_dinas" {
		t.Errorf("tenant-a EscalateToRole: mau kepala_dinas, dapat %q", got)
	}

	// Tenant B: binding ke kabag_umum & direktur
	if got := defB.Transitions[0].Notify.ToRole; got != "kabag_umum" {
		t.Errorf("tenant-b Notify.ToRole: mau kabag_umum, dapat %q", got)
	}
	if got := defB.States[1].EscalateToRole; got != "direktur" {
		t.Errorf("tenant-b EscalateToRole: mau direktur, dapat %q", got)
	}

	// Definisi asli tidak ikut berubah (immutability terjaga).
	if defStandar.Transitions[0].Notify.ToRole != "validator_tahap_1" {
		t.Error("definisi asli tidak boleh termutasi oleh binding")
	}
}

// TestGetForTenant_TemplateIDTidakDikenal_Error: config ada tapi template_id
// merujuk definisi yang tidak ada di store.
func TestGetForTenant_TemplateIDTidakDikenal_Error(t *testing.T) {
	defStore := workflow.NewMemoryStore() // kosong — tidak ada definisi
	tplStore := workflow.NewMemoryTemplateStore(defStore)

	_ = tplStore.SetTenantTemplate(workflow.TenantWorkflowConfig{
		TenantID:   "tenant-x",
		Slot:       "surat_masuk.disposisi",
		TemplateID: "tidak.ada.sama.sekali",
	})

	_, err := tplStore.GetForTenant("tenant-x", "surat_masuk.disposisi")
	if err == nil {
		t.Fatal("GetForTenant dengan template_id tak dikenal harus error")
	}
}
