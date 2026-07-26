package workflow_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/huda-salam/pamong/core/notification"
	"github.com/huda-salam/pamong/core/scheduler"
	coreWf "github.com/huda-salam/pamong/core/workflow"
	infraWf "github.com/huda-salam/pamong/infra/workflow"
	"github.com/huda-salam/pamong/testkit"
)

// newRunner merakit scheduler.Runner memory dengan handler eskalasi terdaftar. clock membuat
// waktu deterministik agar RunDue bisa "melewati" deadline tanpa menunggu nyata.
func newRunner(t *testing.T, coord *coreWf.EscalationCoordinator, clock func() time.Time) (*scheduler.Runner, scheduler.JobStore) {
	t.Helper()
	reg := scheduler.NewRegistry()
	if err := reg.Register(infraWf.EscalationJobKey, infraWf.EscalationJob(coord)); err != nil {
		t.Fatalf("register handler eskalasi: %v", err)
	}
	store := scheduler.NewMemoryJobStore()
	runner := scheduler.NewRunner(reg, store, time.Minute).WithClock(clock)
	return runner, store
}

// TestSchedulerDeadlines_FireMemicuEskalasi: jadwalkan deadline, majukan clock melewati FireAt,
// RunDue → EscalationJob → coordinator (instance masih di state) → eskalasi terkirim.
func TestSchedulerDeadlines_FireMemicuEskalasi(t *testing.T) {
	now := time.Now()
	clock := func() time.Time { return now }

	reader := testkit.NewMockInstanceStateReader()
	esc := testkit.NewMockEscalator()
	coord := coreWf.NewEscalationCoordinator(reader, esc)
	runner, store := newRunner(t, coord, clock)
	deadlines := infraWf.NewSchedulerDeadlines(runner, store)

	instID := uuid.New()
	reader.Set(instID, "menunggu_validasi") // masih di state saat deadline lewat
	d := coreWf.Deadline{
		Key:    coreWf.DeadlineKey(instID, "menunggu_validasi"),
		FireAt: now.Add(24 * time.Hour),
		Escalation: coreWf.Escalation{
			TenantID:       "pemkot-surabaya",
			InstanceID:     instID,
			State:          "menunggu_validasi",
			EscalateToRole: "kepala_dinas",
		},
	}
	if err := deadlines.ScheduleDeadline(context.Background(), d); err != nil {
		t.Fatalf("ScheduleDeadline: %v", err)
	}

	// Belum jatuh tempo → tak ada eskalasi.
	if n, _ := runner.RunDue(context.Background()); n != 0 {
		t.Fatalf("belum jatuh tempo, tak boleh ada job jalan, dapat %d", n)
	}

	// Majukan waktu melewati deadline → job jalan → eskalasi.
	now = now.Add(25 * time.Hour)
	if _, err := runner.RunDue(context.Background()); err != nil {
		t.Fatalf("RunDue: %v", err)
	}
	if esc.Count() != 1 {
		t.Fatalf("deadline lewat harus memicu 1 eskalasi, dapat %d", esc.Count())
	}
	if esc.Escalated[0].EscalateToRole != "kepala_dinas" {
		t.Errorf("eskalasi ke role salah: %+v", esc.Escalated[0])
	}
}

// TestSchedulerDeadlines_Cancel_TidakFire: batalkan deadline sebelum jatuh tempo → tak fire.
func TestSchedulerDeadlines_Cancel_TidakFire(t *testing.T) {
	now := time.Now()
	clock := func() time.Time { return now }

	reader := testkit.NewMockInstanceStateReader()
	esc := testkit.NewMockEscalator()
	coord := coreWf.NewEscalationCoordinator(reader, esc)
	runner, store := newRunner(t, coord, clock)
	deadlines := infraWf.NewSchedulerDeadlines(runner, store)

	instID := uuid.New()
	reader.Set(instID, "menunggu_validasi")
	key := coreWf.DeadlineKey(instID, "menunggu_validasi")
	d := coreWf.Deadline{
		Key:    key,
		FireAt: now.Add(24 * time.Hour),
		Escalation: coreWf.Escalation{
			InstanceID:     instID,
			State:          "menunggu_validasi",
			EscalateToRole: "kepala_dinas",
		},
	}
	if err := deadlines.ScheduleDeadline(context.Background(), d); err != nil {
		t.Fatalf("ScheduleDeadline: %v", err)
	}
	if err := deadlines.CancelDeadline(context.Background(), key); err != nil {
		t.Fatalf("CancelDeadline: %v", err)
	}

	now = now.Add(25 * time.Hour)
	if _, err := runner.RunDue(context.Background()); err != nil {
		t.Fatalf("RunDue: %v", err)
	}
	if esc.Count() != 0 {
		t.Errorf("deadline yang dibatalkan tak boleh fire, dapat %d eskalasi", esc.Count())
	}
}

// TestSchedulerDeadlines_Cancel_Idempoten: membatalkan key tak dikenal bukan error.
func TestSchedulerDeadlines_Cancel_Idempoten(t *testing.T) {
	reader := testkit.NewMockInstanceStateReader()
	esc := testkit.NewMockEscalator()
	coord := coreWf.NewEscalationCoordinator(reader, esc)
	runner, store := newRunner(t, coord, time.Now)
	deadlines := infraWf.NewSchedulerDeadlines(runner, store)

	if err := deadlines.CancelDeadline(context.Background(), "tidak.pernah.dijadwalkan"); err != nil {
		t.Errorf("cancel key tak dikenal harus no-op, dapat error: %v", err)
	}
}

// TestSchedulerDeadlines_FireTapiSudahPindah_NoOp: deadline fire, tapi guard race menemukan
// instance sudah pindah state → tak ada eskalasi (integrasi coordinator + scheduler).
func TestSchedulerDeadlines_FireTapiSudahPindah_NoOp(t *testing.T) {
	now := time.Now()
	clock := func() time.Time { return now }

	reader := testkit.NewMockInstanceStateReader()
	esc := testkit.NewMockEscalator()
	coord := coreWf.NewEscalationCoordinator(reader, esc)
	runner, store := newRunner(t, coord, clock)
	deadlines := infraWf.NewSchedulerDeadlines(runner, store)

	instID := uuid.New()
	reader.Set(instID, "menunggu_persetujuan") // SUDAH pindah dari state ber-deadline
	d := coreWf.Deadline{
		Key:    coreWf.DeadlineKey(instID, "menunggu_validasi"),
		FireAt: now.Add(1 * time.Hour),
		Escalation: coreWf.Escalation{
			InstanceID:     instID,
			State:          "menunggu_validasi",
			EscalateToRole: "kepala_dinas",
		},
	}
	if err := deadlines.ScheduleDeadline(context.Background(), d); err != nil {
		t.Fatalf("ScheduleDeadline: %v", err)
	}
	now = now.Add(2 * time.Hour)
	if _, err := runner.RunDue(context.Background()); err != nil {
		t.Fatalf("RunDue: %v", err)
	}
	if esc.Count() != 0 {
		t.Errorf("instance sudah pindah → guard race harus no-op, dapat %d", esc.Count())
	}
}

// --- NotifierEscalator mapping ---

// fakeRoleNotify menangkap RoleTarget & template yang dikirim NotifierEscalator.
type fakeRoleNotify struct {
	target      notification.RoleTarget
	templateKey string
	channels    []string
	called      int
}

func (f *fakeRoleNotify) NotifyRole(_ context.Context, t notification.RoleTarget, templateKey string, _ map[string]any, channels ...string) (int, error) {
	f.called++
	f.target = t
	f.templateKey = templateKey
	f.channels = channels
	return 1, nil
}

func TestNotifierEscalator_MemetakanEscalationKeRoleTarget(t *testing.T) {
	fake := &fakeRoleNotify{}
	esc := infraWf.NewNotifierEscalator(fake, "workflow.sla.escalation", "in_app")

	err := esc.Escalate(context.Background(), coreWf.Escalation{
		TenantID:       "pemkot-surabaya",
		InstanceID:     uuid.New(),
		State:          "menunggu_validasi",
		EscalateToRole: "kepala_dinas",
	})
	if err != nil {
		t.Fatalf("Escalate: %v", err)
	}
	if fake.called != 1 {
		t.Fatalf("NotifyRole harus dipanggil sekali, dapat %d", fake.called)
	}
	if fake.target.TenantID != "pemkot-surabaya" || fake.target.Role != "kepala_dinas" {
		t.Errorf("RoleTarget tidak tepat: %+v", fake.target)
	}
	if fake.templateKey != "workflow.sla.escalation" {
		t.Errorf("template key salah: %q", fake.templateKey)
	}
	if len(fake.channels) != 1 || fake.channels[0] != "in_app" {
		t.Errorf("channels salah: %v", fake.channels)
	}
}

// --- NotifierTransition mapping (PR-N2) ---

func TestNotifierTransition_MemetakanNotifySpecKeRoleTarget(t *testing.T) {
	fake := &fakeRoleNotify{}
	notif := infraWf.NewNotifierTransition(fake, "in_app")

	instID := uuid.New()
	err := notif.NotifyTransition(context.Background(), "pemkot-surabaya",
		coreWf.NotifySpec{ToRole: "ppk_opd", Template: "notif_disposisi"},
		coreWf.WorkflowInstance{ID: instID, CurrentState: "didisposisi"})
	if err != nil {
		t.Fatalf("NotifyTransition: %v", err)
	}
	if fake.called != 1 {
		t.Fatalf("NotifyRole harus dipanggil sekali, dapat %d", fake.called)
	}
	if fake.target.TenantID != "pemkot-surabaya" || fake.target.Role != "ppk_opd" {
		t.Errorf("RoleTarget tidak tepat: %+v", fake.target)
	}
	if fake.templateKey != "notif_disposisi" {
		t.Errorf("template key salah: %q", fake.templateKey)
	}
	if len(fake.channels) != 1 || fake.channels[0] != "in_app" {
		t.Errorf("channels salah: %v", fake.channels)
	}
}
