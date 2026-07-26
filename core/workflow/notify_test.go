package workflow_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core/workflow"
	"github.com/huda-salam/pamong/testkit"
)

// defBindDemo adalah definisi kecil dengan SATU transisi ber-Notify DAN satu state tujuan
// ber-SLA+EscalateToRole — dipakai membuktikan bahwa role binding tenant diterapkan KONSISTEN
// baik untuk NotifySpec.ToRole maupun Escalation.EscalateToRole (PR-N2 bagian A/B), termasuk
// pada transisi PERTAMA (bukan hanya initial_state).
var defBindDemo = workflow.WorkflowDefinition{
	ID:           "test.binddemo.standar",
	Entity:       "test.Dok",
	Version:      1,
	InitialState: "mulai",
	States: []workflow.State{
		{Name: "mulai", Actions: []string{"lanjut"}},
		{Name: "menunggu", SLAHours: 24, EscalateToRole: "validator_sla", Actions: []string{"selesai"}},
		{Name: "selesai", IsTerminal: true},
	},
	Transitions: []workflow.Transition{
		{From: "mulai", To: "menunggu", On: "lanjut", Action: "DisposisiSurat",
			Notify: &workflow.NotifySpec{ToRole: "validator_tahap_1", Template: "notif_lanjut"}},
		{From: "menunggu", To: "selesai", On: "selesai"},
	},
	AuthoringSource: "developer",
}

// ===== StartFromTemplate =====

func TestEngine_StartFromTemplate_TanpaTemplateStore_Error(t *testing.T) {
	store := workflow.NewMemoryStore()
	_ = store.Register(defBindDemo)
	eng := workflow.New(store, &dispatchRecord{}, guardAlwaysTrue{}) // tanpa WithTemplates

	_, err := eng.StartFromTemplate(testkit.Ctx(t, testkit.WithTenant("tenant-a")), "test.binddemo", uuid.New())
	if err == nil {
		t.Fatal("StartFromTemplate tanpa WithTemplates harus gagal")
	}
}

func TestEngine_StartFromTemplate_TenantBelumPilih_Error(t *testing.T) {
	store := workflow.NewMemoryStore()
	_ = store.Register(defBindDemo)
	tplStore := workflow.NewMemoryTemplateStore(store)
	eng := workflow.New(store, &dispatchRecord{}, guardAlwaysTrue{}, workflow.WithTemplates(tplStore))

	_, err := eng.StartFromTemplate(testkit.Ctx(t, testkit.WithTenant("tenant-x")), "test.binddemo", uuid.New())
	if err == nil {
		t.Fatal("tenant belum memilih template untuk slot ini harus gagal")
	}
}

func TestEngine_StartFromTemplate_MembekukanRoleBindings(t *testing.T) {
	store := workflow.NewMemoryStore()
	_ = store.Register(defBindDemo)
	tplStore := workflow.NewMemoryTemplateStore(store)
	_ = tplStore.SetTenantTemplate(workflow.TenantWorkflowConfig{
		TenantID:   "tenant-a",
		Slot:       "test.binddemo",
		TemplateID: defBindDemo.ID,
		RoleBindings: map[string]string{
			"validator_tahap_1": "ppk_opd",
			"validator_sla":     "kepala_dinas",
		},
	})
	eng := workflow.New(store, &dispatchRecord{}, guardAlwaysTrue{}, workflow.WithTemplates(tplStore))

	inst, err := eng.StartFromTemplate(testkit.Ctx(t, testkit.WithTenant("tenant-a")), "test.binddemo", uuid.New())
	if err != nil {
		t.Fatalf("StartFromTemplate: %v", err)
	}
	if inst.CurrentState != "mulai" || inst.DefinitionID != defBindDemo.ID || inst.DefinitionVersion != 1 {
		t.Errorf("instance dasar tidak tepat: %+v", inst)
	}
	if got := inst.RoleBindings["validator_sla"]; got != "kepala_dinas" {
		t.Errorf("RoleBindings tidak dibekukan ke instance: %+v", inst.RoleBindings)
	}
}

// ===== Konsistensi binding: Notify transisi + SLA eskalasi sama-sama pakai role konkret =====

// TestEngine_StartFromTemplate_BindingKonsistenDiNotifyDanSLA membuktikan role binding tenant
// diterapkan pada KEDUA jalur (Notify transisi & Escalation SLA) untuk transisi PERTAMA setelah
// Start — bukan hanya initial_state (backlog ROADMAP §810).
func TestEngine_StartFromTemplate_BindingKonsistenDiNotifyDanSLA(t *testing.T) {
	store := workflow.NewMemoryStore()
	_ = store.Register(defBindDemo)
	tplStore := workflow.NewMemoryTemplateStore(store)
	_ = tplStore.SetTenantTemplate(workflow.TenantWorkflowConfig{
		TenantID:   "tenant-a",
		Slot:       "test.binddemo",
		TemplateID: defBindDemo.ID,
		RoleBindings: map[string]string{
			"validator_tahap_1": "ppk_opd",
			"validator_sla":     "kepala_dinas",
		},
	})

	sched := testkit.NewMockDeadlineScheduler()
	notifier := testkit.NewMockTransitionNotifier()
	eng := workflow.New(store, &dispatchRecord{}, guardAlwaysTrue{},
		workflow.WithTemplates(tplStore), workflow.WithDeadlines(sched), workflow.WithNotifier(notifier))

	ctx := testkit.Ctx(t, testkit.WithTenant("tenant-a"), testkit.WithPersonID(uuid.New()))
	inst, err := eng.StartFromTemplate(ctx, "test.binddemo", uuid.New())
	if err != nil {
		t.Fatalf("StartFromTemplate: %v", err)
	}
	if err := eng.Execute(ctx, inst, "lanjut", nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// Notify transisi: ToRole harus role KONKRET tenant, bukan peran generik "validator_tahap_1".
	if notifier.Count() != 1 {
		t.Fatalf("notifier harus terpanggil 1x, dapat %d", notifier.Count())
	}
	call := notifier.Notified[0]
	if call.Spec.ToRole != "ppk_opd" {
		t.Errorf("Notify ToRole: mau %q (bound), dapat %q", "ppk_opd", call.Spec.ToRole)
	}
	if call.TenantID != "tenant-a" {
		t.Errorf("Notify tenant: mau tenant-a, dapat %q", call.TenantID)
	}

	// SLA state baru ("menunggu"): EscalateToRole harus role KONKRET, bukan "validator_sla".
	key := workflow.DeadlineKey(inst.ID, "menunggu")
	d, ok := sched.ScheduledFor(key)
	if !ok {
		t.Fatalf("deadline untuk state 'menunggu' harus dijadwalkan")
	}
	if d.Escalation.EscalateToRole != "kepala_dinas" {
		t.Errorf("EscalateToRole: mau %q (bound), dapat %q", "kepala_dinas", d.Escalation.EscalateToRole)
	}
}

// ===== TransitionNotifier: seam opsional, best-effort =====

func TestEngine_Execute_TanpaNotifySpec_NotifierTidakDipanggil(t *testing.T) {
	store := workflow.NewMemoryStore()
	_ = store.Register(defBindDemo)
	notifier := testkit.NewMockTransitionNotifier()
	eng := workflow.New(store, &dispatchRecord{}, guardAlwaysTrue{}, workflow.WithNotifier(notifier))
	ctx := actorDenganPermission(t)

	inst, _ := eng.Start(ctx, defBindDemo.ID, uuid.New())
	if err := eng.Execute(ctx, inst, "lanjut", nil); err != nil { // transisi ber-Notify
		t.Fatalf("Execute lanjut: %v", err)
	}
	if err := eng.Execute(ctx, inst, "selesai", nil); err != nil { // transisi TANPA Notify
		t.Fatalf("Execute selesai: %v", err)
	}
	if notifier.Count() != 1 {
		t.Errorf("notifier harus tetap terpanggil 1x (transisi kedua tanpa Notify), dapat %d", notifier.Count())
	}
}

func TestEngine_Execute_TanpaNotifierTerpasang_TidakPanic(t *testing.T) {
	store := workflow.NewMemoryStore()
	_ = store.Register(defBindDemo)
	eng := workflow.New(store, &dispatchRecord{}, guardAlwaysTrue{}) // tanpa WithNotifier
	ctx := actorDenganPermission(t)

	inst, _ := eng.Start(ctx, defBindDemo.ID, uuid.New())
	if err := eng.Execute(ctx, inst, "lanjut", nil); err != nil {
		t.Fatalf("Execute tanpa notifier harus tetap sukses: %v", err)
	}
}

// TestEngine_Execute_NotifierGagal_TransisiTetapBerhasil membuktikan kegagalan notifikasi
// TIDAK membatalkan transisi domain (transisi sudah otoritatif) — error tetap dipropagasi
// agar caller async bisa retry, tapi state & history instance sudah berubah.
func TestEngine_Execute_NotifierGagal_TransisiTetapBerhasil(t *testing.T) {
	store := workflow.NewMemoryStore()
	_ = store.Register(defBindDemo)
	notifier := testkit.NewMockTransitionNotifier()
	notifier.FailNext = errors.New("kirim notifikasi gagal")
	eng := workflow.New(store, &dispatchRecord{}, guardAlwaysTrue{}, workflow.WithNotifier(notifier))
	ctx := actorDenganPermission(t)

	inst, _ := eng.Start(ctx, defBindDemo.ID, uuid.New())
	err := eng.Execute(ctx, inst, "lanjut", nil)
	if err == nil {
		t.Fatal("kegagalan notifier harus dipropagasi sebagai error")
	}
	if inst.CurrentState != "menunggu" {
		t.Errorf("state harus tetap berpindah walau notifier gagal (transisi sudah otoritatif), dapat %q", inst.CurrentState)
	}
	if len(inst.History) != 1 {
		t.Errorf("history harus tetap tercatat walau notifier gagal, dapat %d", len(inst.History))
	}
	if notifier.Count() != 1 {
		t.Errorf("notifier harus tetap terekam terpanggil walau gagal, dapat %d", notifier.Count())
	}
}
