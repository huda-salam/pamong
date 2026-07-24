package workflow_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core/workflow"
	"github.com/huda-salam/pamong/testkit"
)

// defSLA adalah definisi dengan dua state ber-SLA berurutan lalu terminal tanpa SLA.
// initial_state ber-SLA agar Start langsung menjadwalkan deadline.
var defSLA = workflow.WorkflowDefinition{
	ID:            "test.sla.standar",
	Entity:        "test.Dok",
	Version:       1,
	EffectiveFrom: time.Now(),
	InitialState:  "menunggu_validasi",
	States: []workflow.State{
		{Name: "menunggu_validasi", SLAHours: 24, EscalateToRole: "kepala_dinas", Actions: []string{"validasi"}},
		{Name: "menunggu_persetujuan", SLAHours: 48, EscalateToRole: "sekda", Actions: []string{"setuju"}},
		{Name: "selesai", IsTerminal: true},
	},
	Transitions: []workflow.Transition{
		{From: "menunggu_validasi", To: "menunggu_persetujuan", On: "validasi"},
		{From: "menunggu_persetujuan", To: "selesai", On: "setuju"},
	},
	AuthoringSource: "developer",
}

// newSLAEngine merakit engine dengan defSLA + scheduler mock terpasang.
func newSLAEngine(t *testing.T, sched workflow.DeadlineScheduler) *workflow.Engine {
	t.Helper()
	store := workflow.NewMemoryStore()
	if err := store.Register(defSLA); err != nil {
		t.Fatalf("register defSLA: %v", err)
	}
	return workflow.New(store, &dispatchRecord{}, guardAlwaysTrue{}, workflow.WithDeadlines(sched))
}

// ===== Penjadwalan deadline saat masuk state =====

func TestEngine_Start_StateBerSLA_MenjadwalkanDeadline(t *testing.T) {
	sched := testkit.NewMockDeadlineScheduler()
	eng := newSLAEngine(t, sched)
	ctx := testkit.Ctx(t, testkit.WithTenant("pemkot-surabaya"))

	before := time.Now()
	inst, err := eng.Start(ctx, defSLA.ID, uuid.New())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	key := workflow.DeadlineKey(inst.ID, "menunggu_validasi")
	d, ok := sched.ScheduledFor(key)
	if !ok {
		t.Fatalf("deadline untuk initial_state ber-SLA harus dijadwalkan (key %q)", key)
	}
	if d.Escalation.EscalateToRole != "kepala_dinas" {
		t.Errorf("escalate role: mau %q, dapat %q", "kepala_dinas", d.Escalation.EscalateToRole)
	}
	if d.Escalation.TenantID != "pemkot-surabaya" {
		t.Errorf("tenant di escalation: mau %q, dapat %q", "pemkot-surabaya", d.Escalation.TenantID)
	}
	if d.Escalation.InstanceID != inst.ID || d.Escalation.State != "menunggu_validasi" {
		t.Errorf("escalation instance/state tidak tepat: %+v", d.Escalation)
	}
	// FireAt ~ now + 24h (toleransi lebar; yang penting di masa depan sesuai SLAHours).
	wantMin := before.Add(24 * time.Hour)
	if d.FireAt.Before(wantMin.Add(-time.Minute)) {
		t.Errorf("FireAt %v harus >= now+24h (%v)", d.FireAt, wantMin)
	}
}

func TestEngine_Start_StateTanpaSLA_TidakMenjadwalkan(t *testing.T) {
	// Definisi initial_state tanpa SLA.
	store := workflow.NewMemoryStore()
	if err := store.Register(defDisposisi); err != nil {
		t.Fatalf("register: %v", err)
	}
	sched := testkit.NewMockDeadlineScheduler()
	eng := workflow.New(store, &dispatchRecord{}, guardAlwaysTrue{}, workflow.WithDeadlines(sched))

	if _, err := eng.Start(testkit.Ctx(t), defDisposisi.ID, uuid.New()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if len(sched.Scheduled) != 0 {
		t.Errorf("state tanpa SLA tidak boleh menjadwalkan deadline, dapat %d", len(sched.Scheduled))
	}
}

func TestEngine_Start_TanpaScheduler_TidakPanic(t *testing.T) {
	// Engine tanpa WithDeadlines: SLA nonaktif, Start tetap jalan (backward-compatible).
	store := workflow.NewMemoryStore()
	if err := store.Register(defSLA); err != nil {
		t.Fatalf("register: %v", err)
	}
	eng := workflow.New(store, &dispatchRecord{}, guardAlwaysTrue{})
	if _, err := eng.Start(testkit.Ctx(t), defSLA.ID, uuid.New()); err != nil {
		t.Fatalf("Start tanpa scheduler harus sukses: %v", err)
	}
}

// ===== Transisi: batalkan timer state lama, jadwalkan state baru =====

func TestEngine_Execute_Transisi_BatalkanLamaJadwalkanBaru(t *testing.T) {
	sched := testkit.NewMockDeadlineScheduler()
	eng := newSLAEngine(t, sched)
	ctx := testkit.Ctx(t, testkit.WithTenant("pemkot-malang"))

	inst, _ := eng.Start(ctx, defSLA.ID, uuid.New())
	keyLama := workflow.DeadlineKey(inst.ID, "menunggu_validasi")
	keyBaru := workflow.DeadlineKey(inst.ID, "menunggu_persetujuan")

	if err := eng.Execute(ctx, inst, "validasi", nil); err != nil {
		t.Fatalf("Execute validasi: %v", err)
	}

	if !sched.WasCancelled(keyLama) {
		t.Errorf("timer state lama (%q) harus dibatalkan saat keluar", keyLama)
	}
	d, ok := sched.ScheduledFor(keyBaru)
	if !ok {
		t.Fatalf("timer state baru (%q) harus dijadwalkan", keyBaru)
	}
	if d.Escalation.EscalateToRole != "sekda" {
		t.Errorf("escalate role state baru: mau %q, dapat %q", "sekda", d.Escalation.EscalateToRole)
	}
}

func TestEngine_Execute_MasukStateTerminalTanpaSLA_HanyaBatalkan(t *testing.T) {
	sched := testkit.NewMockDeadlineScheduler()
	eng := newSLAEngine(t, sched)
	ctx := testkit.Ctx(t)

	inst, _ := eng.Start(ctx, defSLA.ID, uuid.New())
	_ = eng.Execute(ctx, inst, "validasi", nil) // → menunggu_persetujuan (SLA)
	scheduledBefore := len(sched.Scheduled)

	// Transisi ke "selesai" (terminal, tanpa SLA): batalkan timer persetujuan, tak jadwal baru.
	if err := eng.Execute(ctx, inst, "setuju", nil); err != nil {
		t.Fatalf("Execute setuju: %v", err)
	}
	if !sched.WasCancelled(workflow.DeadlineKey(inst.ID, "menunggu_persetujuan")) {
		t.Error("timer state persetujuan harus dibatalkan saat masuk terminal")
	}
	if len(sched.Scheduled) != scheduledBefore {
		t.Errorf("state terminal tanpa SLA tidak boleh menjadwalkan deadline baru")
	}
}

func TestEngine_Execute_SchedulerGagal_ErrorDipropagasi(t *testing.T) {
	sched := testkit.NewMockDeadlineScheduler()
	eng := newSLAEngine(t, sched)
	ctx := testkit.Ctx(t)
	inst, _ := eng.Start(ctx, defSLA.ID, uuid.New())

	// Cancel pertama pada transisi berikutnya gagal → error dipropagasi.
	sched.FailNext = errors.New("scheduler down")
	if err := eng.Execute(ctx, inst, "validasi", nil); err == nil {
		t.Fatal("kegagalan scheduler saat transisi harus dipropagasi")
	}
}

// ===== EscalationCoordinator — guard race + eskalasi =====

func TestEscalationCoordinator_MasihDiState_Eskalasi(t *testing.T) {
	reader := testkit.NewMockInstanceStateReader()
	esc := testkit.NewMockEscalator()
	coord := workflow.NewEscalationCoordinator(reader, esc)

	instID := uuid.New()
	reader.Set(instID, "menunggu_validasi") // masih di state ber-SLA
	e := workflow.Escalation{
		TenantID:       "pemkot-surabaya",
		InstanceID:     instID,
		State:          "menunggu_validasi",
		EscalateToRole: "kepala_dinas",
	}

	if err := coord.Deliver(context.Background(), e); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if esc.Count() != 1 {
		t.Fatalf("harus tepat 1 eskalasi, dapat %d", esc.Count())
	}
	if esc.Escalated[0].EscalateToRole != "kepala_dinas" {
		t.Errorf("eskalasi ke role salah: %+v", esc.Escalated[0])
	}
}

func TestEscalationCoordinator_SudahPindah_NoOp(t *testing.T) {
	reader := testkit.NewMockInstanceStateReader()
	esc := testkit.NewMockEscalator()
	coord := workflow.NewEscalationCoordinator(reader, esc)

	instID := uuid.New()
	reader.Set(instID, "menunggu_persetujuan") // sudah pindah dari state ber-deadline
	e := workflow.Escalation{
		InstanceID:     instID,
		State:          "menunggu_validasi", // deadline dijadwalkan untuk state lama
		EscalateToRole: "kepala_dinas",
	}

	if err := coord.Deliver(context.Background(), e); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if esc.Count() != 0 {
		t.Errorf("instance sudah pindah state → tidak boleh ada eskalasi, dapat %d", esc.Count())
	}
}

func TestEscalationCoordinator_InstanceTakAda_ErrorDipropagasi(t *testing.T) {
	reader := testkit.NewMockInstanceStateReader() // kosong
	esc := testkit.NewMockEscalator()
	coord := workflow.NewEscalationCoordinator(reader, esc)

	err := coord.Deliver(context.Background(), workflow.Escalation{
		InstanceID: uuid.New(),
		State:      "menunggu_validasi",
	})
	if err == nil {
		t.Fatal("instance tak ditemukan harus mengembalikan error, bukan eskalasi diam-diam")
	}
	if esc.Count() != 0 {
		t.Error("tidak boleh eskalasi saat state tak terbaca")
	}
}

func TestEscalationCoordinator_EscalatorGagal_ErrorDipropagasi(t *testing.T) {
	reader := testkit.NewMockInstanceStateReader()
	esc := testkit.NewMockEscalator()
	esc.FailNext = errors.New("notification down")
	coord := workflow.NewEscalationCoordinator(reader, esc)

	instID := uuid.New()
	reader.Set(instID, "s1")
	err := coord.Deliver(context.Background(), workflow.Escalation{InstanceID: instID, State: "s1"})
	if err == nil {
		t.Fatal("kegagalan escalator harus dipropagasi agar scheduler bisa replay")
	}
}

// ===== Integrasi ringan: transisi SEBELUM deadline → timer batal, no eskalasi =====

// Menyimulasikan alur nyata: engine menjadwal deadline, lalu transisi membatalkannya sebelum
// fire. Karena batal, deadline tak pernah "lewat". Bila TOH fire (batal luput), guard race
// membuatnya no-op karena instance sudah pindah state.
func TestSLA_TransisiSebelumDeadline_TidakAdaEskalasi(t *testing.T) {
	sched := testkit.NewMockDeadlineScheduler()
	eng := newSLAEngine(t, sched)
	reader := testkit.NewMockInstanceStateReader()
	esc := testkit.NewMockEscalator()
	coord := workflow.NewEscalationCoordinator(reader, esc)
	ctx := testkit.Ctx(t)

	inst, _ := eng.Start(ctx, defSLA.ID, uuid.New())
	keyValidasi := workflow.DeadlineKey(inst.ID, "menunggu_validasi")

	// Transisi sebelum deadline → timer state awal dibatalkan.
	if err := eng.Execute(ctx, inst, "validasi", nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !sched.WasCancelled(keyValidasi) {
		t.Fatal("deadline state awal harus dibatalkan oleh transisi")
	}

	// Andai deadline state awal TOH fire (batal luput): instance kini di menunggu_persetujuan,
	// guard race menemukan mismatch → no-op.
	reader.Set(inst.ID, inst.CurrentState) // "menunggu_persetujuan"
	staleDeadline, _ := sched.ScheduledFor(keyValidasi)
	if err := coord.Deliver(context.Background(), staleDeadline.Escalation); err != nil {
		t.Fatalf("Deliver deadline basi: %v", err)
	}
	if esc.Count() != 0 {
		t.Errorf("deadline basi tak boleh mengeskalasi, dapat %d", esc.Count())
	}
}
