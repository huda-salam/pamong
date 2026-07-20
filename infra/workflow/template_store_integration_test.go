//go:build integration

package workflow_test

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	coreWf "github.com/huda-salam/pamong/core/workflow"
	"github.com/huda-salam/pamong/infra/db"
	infraWf "github.com/huda-salam/pamong/infra/workflow"
	"github.com/huda-salam/pamong/port"
	"github.com/huda-salam/pamong/testkit"
)

// newTemplateStoreEnv menyiapkan DBStore (definisi) + DBTemplateStore dengan pool
// mandiri agar cleanup tidak bergantung pada newTestStore.
func newTemplateStoreEnv(t *testing.T) (*infraWf.DBTemplateStore, *infraWf.DBStore, context.Context) {
	t.Helper()
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

	// Bersihkan state sebelumnya agar deterministik.
	_, _ = pool.Exec(ctx,
		`DROP TABLE IF EXISTS gov.tenant_workflow_configs;
		 DROP INDEX  IF EXISTS gov.idx_twc_tenant_slot;
		 DROP TABLE  IF EXISTS gov.workflow_definitions;
		 DROP INDEX  IF EXISTS gov.idx_wfdef_lookup`)

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DROP TABLE IF EXISTS gov.tenant_workflow_configs;
			 DROP INDEX  IF EXISTS gov.idx_twc_tenant_slot;
			 DROP TABLE  IF EXISTS gov.workflow_definitions;
			 DROP INDEX  IF EXISTS gov.idx_wfdef_lookup`)
		pgpool.Close()
	})

	defStore := infraWf.NewDBStore(pool)
	if err := defStore.EnsureSchema(ctx); err != nil {
		t.Fatalf("ensure schema def store: %v", err)
	}

	tplStore := infraWf.NewDBTemplateStore(pool, defStore)
	if err := tplStore.EnsureSchema(ctx); err != nil {
		t.Fatalf("ensure schema template store: %v", err)
	}
	return tplStore, defStore, ctx
}

// testAuthCtx membuat AuthContext minimal untuk integration test engine.
func testAuthCtx(t *testing.T) port.AuthContext {
	t.Helper()
	return testkit.Ctx(t, testkit.WithPersonID(uuid.New()))
}

// intDispatchRecord merekam action yang dipanggil engine — versi lokal
// agar tidak bergantung pada type di paket core/workflow (yang _test).
type intDispatchRecord struct{ called []string }

func (d *intDispatchRecord) Dispatch(_ port.AuthContext, action string, _ coreWf.WorkflowInstance) error {
	d.called = append(d.called, action)
	return nil
}

// intGuardAlwaysTrue evaluator guard yang selalu lolos.
type intGuardAlwaysTrue struct{}

func (intGuardAlwaysTrue) Evaluate(_ string, _ port.AuthContext, _ map[string]any) (bool, error) {
	return true, nil
}

// sampleDefWithStates membuat WorkflowDefinition dengan states & transitions kustom.
func sampleDefWithStates(id string, states []coreWf.State, transitions []coreWf.Transition) coreWf.WorkflowDefinition {
	d := sampleDef(id)
	d.States = states
	d.Transitions = transitions
	return d
}

// ===== Tests =====

// TestDBTemplateStore_RoundTrip: Set → GetTenantConfig mengembalikan config yang sama.
func TestDBTemplateStore_RoundTrip(t *testing.T) {
	tplStore, defStore, _ := newTemplateStoreEnv(t)

	if err := defStore.Register(sampleDef("surat_masuk.disposisi.standar")); err != nil {
		t.Fatalf("register def: %v", err)
	}

	aktorID := uuid.New()
	cfg := coreWf.TenantWorkflowConfig{
		TenantID:   "pemkot-surabaya",
		Slot:       "surat_masuk.disposisi",
		TemplateID: "surat_masuk.disposisi.standar",
		RoleBindings: map[string]string{
			"validator_tahap_1": "ppk_opd",
		},
		SetBy: &aktorID,
	}
	if err := tplStore.SetTenantTemplate(cfg); err != nil {
		t.Fatalf("SetTenantTemplate: %v", err)
	}

	got, err := tplStore.GetTenantConfig("pemkot-surabaya", "surat_masuk.disposisi")
	if err != nil {
		t.Fatalf("GetTenantConfig: %v", err)
	}
	if got.TenantID != cfg.TenantID {
		t.Errorf("TenantID: mau %q, dapat %q", cfg.TenantID, got.TenantID)
	}
	if got.TemplateID != cfg.TemplateID {
		t.Errorf("TemplateID: mau %q, dapat %q", cfg.TemplateID, got.TemplateID)
	}
	if got.RoleBindings["validator_tahap_1"] != "ppk_opd" {
		t.Errorf("RoleBindings: mau ppk_opd, dapat %q", got.RoleBindings["validator_tahap_1"])
	}
	if got.SetBy == nil || *got.SetBy != aktorID {
		t.Errorf("SetBy tidak cocok: dapat %v", got.SetBy)
	}
}

// TestDBTemplateStore_GetConfig_BelumAda: ErrTemplateNotConfigured bila slot belum diset.
func TestDBTemplateStore_GetConfig_BelumAda(t *testing.T) {
	tplStore, _, _ := newTemplateStoreEnv(t)

	_, err := tplStore.GetTenantConfig("tidak-ada", "surat_masuk.disposisi")
	if err == nil {
		t.Fatal("harus error bila config belum ada")
	}
}

// TestDBTemplateStore_Upsert: Set kedua menimpa pilihan pertama (idempoten per slot).
func TestDBTemplateStore_Upsert(t *testing.T) {
	tplStore, defStore, _ := newTemplateStoreEnv(t)

	_ = defStore.Register(sampleDef("surat_masuk.disposisi.standar"))
	_ = defStore.Register(sampleDef("surat_masuk.disposisi.tiga_tahap"))

	_ = tplStore.SetTenantTemplate(coreWf.TenantWorkflowConfig{
		TenantID:   "pemkot-malang",
		Slot:       "surat_masuk.disposisi",
		TemplateID: "surat_masuk.disposisi.standar",
	})
	_ = tplStore.SetTenantTemplate(coreWf.TenantWorkflowConfig{
		TenantID:   "pemkot-malang",
		Slot:       "surat_masuk.disposisi",
		TemplateID: "surat_masuk.disposisi.tiga_tahap",
		RoleBindings: map[string]string{
			"validator_tahap_1": "kabag_umum",
		},
	})

	got, err := tplStore.GetTenantConfig("pemkot-malang", "surat_masuk.disposisi")
	if err != nil {
		t.Fatalf("GetTenantConfig: %v", err)
	}
	if got.TemplateID != "surat_masuk.disposisi.tiga_tahap" {
		t.Errorf("pilihan harus ditimpa ke tiga_tahap, dapat %q", got.TemplateID)
	}
	if got.RoleBindings["validator_tahap_1"] != "kabag_umum" {
		t.Errorf("RoleBindings harus diperbarui, dapat %q", got.RoleBindings["validator_tahap_1"])
	}
}

// TestDBTemplateStore_DuaTenant_TemplateBerbeda — DoD utama integrasi:
// tenant A & B jalan dengan template berbeda, use case identik.
func TestDBTemplateStore_DuaTenant_TemplateBerbeda(t *testing.T) {
	tplStore, defStore, _ := newTemplateStoreEnv(t)

	defStandar := sampleDefWithStates("surat_masuk.disposisi.standar",
		[]coreWf.State{
			{Name: "diterima"},
			{Name: "didisposisi"},
			{Name: "selesai", IsTerminal: true},
		},
		[]coreWf.Transition{
			{From: "diterima", To: "didisposisi", On: "disposisi", Action: "DisposisiSurat"},
			{From: "didisposisi", To: "selesai", On: "selesai"},
		},
	)
	defTigaTahap := sampleDefWithStates("surat_masuk.disposisi.tiga_tahap",
		[]coreWf.State{
			{Name: "diterima"},
			{Name: "validasi"},
			{Name: "persetujuan"},
			{Name: "selesai", IsTerminal: true},
		},
		[]coreWf.Transition{
			{From: "diterima", To: "validasi", On: "validasi", Action: "DisposisiSurat"},
			{From: "validasi", To: "persetujuan", On: "setujui"},
			{From: "persetujuan", To: "selesai", On: "selesai"},
		},
	)
	if err := defStore.Register(defStandar); err != nil {
		t.Fatalf("register standar: %v", err)
	}
	if err := defStore.Register(defTigaTahap); err != nil {
		t.Fatalf("register tiga_tahap: %v", err)
	}

	_ = tplStore.SetTenantTemplate(coreWf.TenantWorkflowConfig{
		TenantID:   "tenant-a",
		Slot:       "surat_masuk.disposisi",
		TemplateID: defStandar.ID,
		RoleBindings: map[string]string{
			"validator_tahap_1": "ppk_opd",
		},
	})
	_ = tplStore.SetTenantTemplate(coreWf.TenantWorkflowConfig{
		TenantID:   "tenant-b",
		Slot:       "surat_masuk.disposisi",
		TemplateID: defTigaTahap.ID,
		RoleBindings: map[string]string{
			"validator_tahap_1": "kabag_umum",
		},
	})

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
	if len(defA.States) != 3 {
		t.Errorf("tenant-a: mau 3 states, dapat %d", len(defA.States))
	}
	if len(defB.States) != 4 {
		t.Errorf("tenant-b: mau 4 states, dapat %d", len(defB.States))
	}

	// Engine yang sama (use case identik) dipakai kedua tenant.
	dispatch := &intDispatchRecord{}
	eng := coreWf.New(defStore, dispatch, intGuardAlwaysTrue{})

	instA, err := eng.Start(testAuthCtx(t), defA.ID, uuid.New())
	if err != nil {
		t.Fatalf("Start tenant-a: %v", err)
	}
	instB, err := eng.Start(testAuthCtx(t), defB.ID, uuid.New())
	if err != nil {
		t.Fatalf("Start tenant-b: %v", err)
	}

	if err := eng.Execute(testAuthCtx(t), instA, "disposisi", nil); err != nil {
		t.Fatalf("Execute tenant-a: %v", err)
	}
	if instA.CurrentState != "didisposisi" {
		t.Errorf("tenant-a state: mau didisposisi, dapat %q", instA.CurrentState)
	}

	if err := eng.Execute(testAuthCtx(t), instB, "validasi", nil); err != nil {
		t.Fatalf("Execute tenant-b: %v", err)
	}
	if instB.CurrentState != "validasi" {
		t.Errorf("tenant-b state: mau validasi, dapat %q", instB.CurrentState)
	}

	// Kedua instance memanggil use case yang sama (DoD: use case identik).
	if len(dispatch.called) != 2 {
		t.Errorf("harus 2 dispatch (satu per tenant), dapat %d", len(dispatch.called))
	}
	for _, action := range dispatch.called {
		if action != "DisposisiSurat" {
			t.Errorf("use case harus DisposisiSurat, dapat %q", action)
		}
	}
}
