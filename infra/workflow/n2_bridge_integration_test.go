//go:build integration

package workflow_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	coreNotif "github.com/huda-salam/pamong/core/notification"
	"github.com/huda-salam/pamong/core/scheduler"
	coreWf "github.com/huda-salam/pamong/core/workflow"
	"github.com/huda-salam/pamong/infra/db"
	infraNotif "github.com/huda-salam/pamong/infra/notification"
	infraWf "github.com/huda-salam/pamong/infra/workflow"
	"github.com/huda-salam/pamong/port"
	tenantroledb "github.com/huda-salam/pamong/tenantrole/adapter/db"
	"github.com/huda-salam/pamong/tenantrole/domain"
	"github.com/huda-salam/pamong/testkit"
)

// n2Def adalah definisi kecil dipakai membuktikan bridge workflow→notifikasi end-to-end
// (PR-N2): satu transisi ber-Notify DARI initial_state, menuju state ber-SLA+EscalateToRole.
var n2Def = coreWf.WorkflowDefinition{
	ID:              "test.n2demo.standar",
	Entity:          "test.Dok",
	Version:         1,
	EffectiveFrom:   time.Now().UTC().Truncate(time.Microsecond),
	InitialState:    "mulai",
	AuthoringSource: "developer",
	States: []coreWf.State{
		{Name: "mulai", Actions: []string{"lanjut"}},
		{Name: "menunggu", SLAHours: 24, EscalateToRole: "validator_sla", Actions: []string{"selesai"}},
		{Name: "selesai", IsTerminal: true},
	},
	Transitions: []coreWf.Transition{
		{From: "mulai", To: "menunggu", On: "lanjut",
			Notify: &coreWf.NotifySpec{ToRole: "validator_tahap_1", Template: "notif.n2demo.lanjut"}},
		{From: "menunggu", To: "selesai", On: "selesai"},
	},
}

// noopDispatcher: definisi test tak punya action use case (Action kosong di semua transisi).
type noopDispatcher struct{}

func (noopDispatcher) Dispatch(_ port.AuthContext, action string, _ coreWf.WorkflowInstance) error {
	return nil
}

type alwaysTrueGuard struct{}

func (alwaysTrueGuard) Evaluate(_ string, _ port.AuthContext, _ map[string]any) (bool, error) {
	return true, nil
}

// singleInstanceReader mengimplementasikan coreWf.InstanceStateReader di atas satu
// *coreWf.WorkflowInstance in-memory — pengganti storage instance (belum ada, PR-3.2.3
// masih tertunda) khusus untuk test ini.
type singleInstanceReader struct{ inst *coreWf.WorkflowInstance }

func (r *singleInstanceReader) CurrentState(_ context.Context, id uuid.UUID) (string, error) {
	if r.inst.ID != id {
		return "", coreWf.ErrDefinitionNotFound(id.String())
	}
	return r.inst.CurrentState, nil
}

// TestN2Bridge_TemplateBerbinding_NotifyDanSLA_SampaiInbox membuktikan end-to-end: instance
// dimulai dari template ter-binding (StartFromTemplate/GetForTenant) → transisi ber-Notify
// mengirim in-app ke role KONKRET tenant → SLA state berikutnya lewat → EscalationCoordinator
// mengeskalasi ke role KONKRET tenant lainnya → in-app masuk inbox holder masing-masing.
func TestN2Bridge_TemplateBerbinding_NotifyDanSLA_SampaiInbox(t *testing.T) {
	dsn := os.Getenv("PAMONG_TEST_DB_DSN")
	if dsn == "" {
		t.Skip("PAMONG_TEST_DB_DSN tidak diset — lewati integration test")
	}
	ctx := context.Background()
	pgpool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("buka pool: %v", err)
	}
	pool := db.NewPool(pgpool)
	reset := func() { _, _ = pool.Exec(context.Background(), `DROP SCHEMA IF EXISTS gov CASCADE`) }
	reset()
	t.Cleanup(func() {
		reset()
		pgpool.Close()
	})

	// --- Definisi + template store (DB) ---
	defStore := infraWf.NewDBStore(pool)
	if err := defStore.EnsureSchema(ctx); err != nil {
		t.Fatalf("ensure schema def: %v", err)
	}
	if err := defStore.Register(n2Def); err != nil {
		t.Fatalf("register n2Def: %v", err)
	}
	tplStore := infraWf.NewDBTemplateStore(pool, defStore)
	if err := tplStore.EnsureSchema(ctx); err != nil {
		t.Fatalf("ensure schema template: %v", err)
	}
	const tenantID = "pemkot-n2"
	if err := tplStore.SetTenantTemplate(coreWf.TenantWorkflowConfig{
		TenantID:   tenantID,
		Slot:       "test.n2demo",
		TemplateID: n2Def.ID,
		RoleBindings: map[string]string{
			"validator_tahap_1": "ppk_opd",
			"validator_sla":     "kepala_dinas",
		},
	}); err != nil {
		t.Fatalf("SetTenantTemplate: %v", err)
	}

	// --- Role tenant + holder (gov.tenant_roles / gov.user_role_assignments) ---
	roleRepo := tenantroledb.NewTenantRoleRepo(pool)
	ppkRoleID := uuid.New()
	if err := roleRepo.Save(ctx, &domain.TenantRole{ID: ppkRoleID, Name: "ppk_opd", Label: "PPK OPD"}); err != nil {
		t.Fatalf("seed role ppk_opd: %v", err)
	}
	kadisRoleID := uuid.New()
	if err := roleRepo.Save(ctx, &domain.TenantRole{ID: kadisRoleID, Name: "kepala_dinas", Label: "Kepala Dinas"}); err != nil {
		t.Fatalf("seed role kepala_dinas: %v", err)
	}
	ppkHolder, kadisHolder := uuid.New(), uuid.New()
	assignRepo := tenantroledb.NewTenantRoleAssignmentRepo(pool)
	if err := assignRepo.Save(ctx, &domain.TenantRoleAssignment{
		ID: uuid.New(), UserID: ppkHolder, RoleID: ppkRoleID,
		AssignedBy: uuid.New(), ValidFrom: time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatalf("seed assignment ppk_opd: %v", err)
	}
	if err := assignRepo.Save(ctx, &domain.TenantRoleAssignment{
		ID: uuid.New(), UserID: kadisHolder, RoleID: kadisRoleID,
		AssignedBy: uuid.New(), ValidFrom: time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatalf("seed assignment kepala_dinas: %v", err)
	}

	// --- Stack notifikasi: RecipientDirectory nyata (N1) + Hub in-app in-memory ---
	notifTemplates := coreNotif.NewMemoryTemplateStore()
	for _, key := range []string{"notif.n2demo.lanjut", "workflow.sla.escalation"} {
		if err := notifTemplates.Upsert(ctx, coreNotif.Template{Key: key, Locale: "id", Subject: key, Body: "isi"}); err != nil {
			t.Fatalf("upsert template %q: %v", key, err)
		}
	}
	inbox := coreNotif.NewMemoryInAppInbox()
	channels := coreNotif.NewChannelRegistry()
	if err := channels.Register(coreNotif.NewInAppChannel(inbox)); err != nil {
		t.Fatalf("register channel: %v", err)
	}
	hub := coreNotif.NewHub(channels, coreNotif.NewTemplateEngine(notifTemplates), coreNotif.NewMemoryDeliveryRecorder())
	dir := infraNotif.NewDBRecipientDirectory(pool)
	roleNotifier := coreNotif.NewRoleNotifier(coreNotif.NewRouter(dir), hub)

	// --- Bridge PR-N2: Escalator (SLA) + TransitionNotifier (transisi) di atas RoleNotifier ---
	escalator := infraWf.NewNotifierEscalator(roleNotifier, "workflow.sla.escalation", coreNotif.ChannelInApp)
	transitionNotifier := infraWf.NewNotifierTransition(roleNotifier, coreNotif.ChannelInApp)

	// --- Scheduler SLA dengan clock terkendali (deterministik, tanpa menunggu nyata) ---
	now := time.Now()
	clock := func() time.Time { return now }

	reader := &singleInstanceReader{}
	coord := coreWf.NewEscalationCoordinator(reader, escalator)
	reg := scheduler.NewRegistry()
	if err := reg.Register(infraWf.EscalationJobKey, infraWf.EscalationJob(coord)); err != nil {
		t.Fatalf("register handler eskalasi: %v", err)
	}
	jobStore := scheduler.NewMemoryJobStore()
	runner := scheduler.NewRunner(reg, jobStore, time.Minute).WithClock(clock)
	deadlines := infraWf.NewSchedulerDeadlines(runner, jobStore)

	engine := coreWf.New(defStore, noopDispatcher{}, alwaysTrueGuard{},
		coreWf.WithTemplates(tplStore), coreWf.WithDeadlines(deadlines), coreWf.WithNotifier(transitionNotifier))

	actorCtx := testkit.Ctx(t, testkit.WithTenant(tenantID), testkit.WithPersonID(uuid.New()))

	inst, err := engine.StartFromTemplate(actorCtx, "test.n2demo", uuid.New())
	if err != nil {
		t.Fatalf("StartFromTemplate: %v", err)
	}
	reader.inst = inst // instance "tersimpan" untuk guard race eskalasi

	// Transisi "lanjut": mulai -> menunggu. Punya Notify (ToRole generik "validator_tahap_1"
	// -> harus terkirim ke role KONKRET "ppk_opd") DAN masuk state ber-SLA ("menunggu").
	if err := engine.Execute(actorCtx, inst, "lanjut", nil); err != nil {
		t.Fatalf("Execute lanjut: %v", err)
	}
	if inst.CurrentState != "menunggu" {
		t.Fatalf("state setelah lanjut: mau menunggu, dapat %q", inst.CurrentState)
	}

	// --- Assert: notifikasi transisi sampai ke holder ppk_opd (in-app) ---
	items, err := inbox.List(ctx, tenantID, ppkHolder.String(), 0)
	if err != nil {
		t.Fatalf("list inbox ppk_opd: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("inbox ppk_opd = %d item, mau 1 (Notify transisi ter-binding ke role konkret)", len(items))
	}

	// --- Lewati deadline SLA state "menunggu" (24 jam) → RunDue memicu eskalasi ---
	now = now.Add(25 * time.Hour)
	if _, err := runner.RunDue(context.Background()); err != nil {
		t.Fatalf("RunDue: %v", err)
	}

	// --- Assert: eskalasi SLA sampai ke holder kepala_dinas (in-app) ---
	items, err = inbox.List(ctx, tenantID, kadisHolder.String(), 0)
	if err != nil {
		t.Fatalf("list inbox kepala_dinas: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("inbox kepala_dinas = %d item, mau 1 (eskalasi SLA ter-binding ke role konkret)", len(items))
	}
}
