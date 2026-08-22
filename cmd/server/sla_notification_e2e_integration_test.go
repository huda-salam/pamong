//go:build integration

// sla_notification_e2e_integration_test.go membuktikan DoD PR-W4b: transisi memicu NOTIFIKASI ke
// inbox pemegang role KONKRET, dan SLA yang lewat memicu ESKALASI — keduanya lewat tumpukan yang
// dirakit persis seperti run() (wireScheduler + newNotificationFactory + newWorkflowFactory di
// belakang buildServerHandler), dengan definisi workflow NYATA milik modul referensi
// (modules/surat_masuk/workflows/disposisi.yaml: sla_hours 72 → sekretaris_daerah; transisi
// selesai → notify agendaris).
//
// Yang tak bisa dibuktikan test per-komponen mana pun:
//
//  1. Engine per-tenant benar-benar menerima WithDeadlines & WithNotifier. Sebelum PR-W4b
//     keduanya nil, dan konsekuensinya adalah no-op yang SAH menurut kontrak engine — state
//     ber-sla_hours tidak menjadwalkan apa pun dan `notify:` tidak mengirim apa pun, tanpa satu
//     pun error di mana pun.
//  2. Baris jadwal mendarat di DB SENTRAL dengan tenant_id terisi (ADR-023), lalu tenant itu
//     kembali dari baris job ke ctx handler dan menentukan tenant DB mana yang dibaca.
//  3. Runner berhenti bersih: goroutine selesai sesudah siklus berjalan tuntas, bukan saat ctx
//     dibatalkan.
//
// CATATAN JUJUR soal isolasi: DSN test tunggal berarti "DB sentral" dan "DB tenant" di sini
// adalah database FISIK yang sama. Yang dibuktikan test ini adalah JALUR perakitannya (store
// scheduler dibangun di atas pool sentral; store notifikasi & instance di atas pool tenant),
// bukan pemisahan fisiknya. Pemisahan itu properti deployment, dan gerbangnya ada di
// infra/schema (TestResidensi_*) plus `pamongctl migrate --central`.
package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/huda-salam/pamong/core/config"
	"github.com/huda-salam/pamong/core/domain"
	"github.com/huda-salam/pamong/core/scheduler"
	coreWf "github.com/huda-salam/pamong/core/workflow"
	"github.com/huda-salam/pamong/gateway"
	gatewaywf "github.com/huda-salam/pamong/gateway/workflow"
	identitydb "github.com/huda-salam/pamong/identity/adapter/db"
	identitytoken "github.com/huda-salam/pamong/identity/adapter/token"
	identitysync "github.com/huda-salam/pamong/identity/sync"
	"github.com/huda-salam/pamong/infra/crypto"
	"github.com/huda-salam/pamong/infra/db"
	"github.com/huda-salam/pamong/infra/eventbus"
	infraNotif "github.com/huda-salam/pamong/infra/notification"
	"github.com/huda-salam/pamong/infra/observability"
	"github.com/huda-salam/pamong/infra/ratelimit"
	"github.com/huda-salam/pamong/infra/sequence"
	"github.com/huda-salam/pamong/infra/storage"
	infrauser "github.com/huda-salam/pamong/infra/user"
	infrawf "github.com/huda-salam/pamong/infra/workflow"
	"github.com/huda-salam/pamong/modules"
	smdomain "github.com/huda-salam/pamong/modules/surat_masuk/domain"
	"github.com/huda-salam/pamong/port"
	tenantroledb "github.com/huda-salam/pamong/tenantrole/adapter/db"
	tenantroledomain "github.com/huda-salam/pamong/tenantrole/domain"
)

func TestE2E_SLAEskalasiDanNotifikasiTransisi(t *testing.T) {
	dsn := os.Getenv("PAMONG_TEST_DB_DSN")
	if dsn == "" {
		t.Skip("PAMONG_TEST_DB_DSN tidak diset — lewati e2e integration test")
	}
	ctx := context.Background()

	pgcfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse DSN: %v", err)
	}
	cp := pgcfg.ConnConfig

	rawPool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("buka pool: %v", err)
	}
	pool := db.NewPool(rawPool)
	dropSchemas := func() {
		_, _ = pool.Exec(context.Background(), `DROP SCHEMA IF EXISTS id, gov, surat_masuk CASCADE`)
	}
	dropSchemas()
	t.Cleanup(func() { dropSchemas(); rawPool.Close() })

	applyUpMigrations(t, ctx, pool, repoPath("identity/migrations"))
	applyUpMigrations(t, ctx, pool, repoPath("modules/surat_masuk/migrations"))

	const (
		tenantID = "pemkot-sla"
		slot     = "surat_masuk.disposisi"
	)
	if _, err := pool.Exec(ctx, `
		INSERT INTO id.tenant_registry (tenant_id, nama, tier, db_host, db_name, is_active)
		VALUES ($1, $2, 1, '', $3, true)`, tenantID, "Pemkot SLA", cp.Database); err != nil {
		t.Fatalf("seed tenant_registry: %v", err)
	}

	// --- Dependency seperti run() ---
	idCfg := config.IdentityDBConfig{
		Host: cp.Host, Port: int(cp.Port), Name: cp.Database, User: cp.User, Password: cp.Password,
		PoolMax: 5, PoolIdle: 1,
	}
	identityPool, err := db.NewIdentity(ctx, idCfg)
	if err != nil {
		t.Fatalf("identity pool: %v", err)
	}
	t.Cleanup(identityPool.Close)

	sharedCfg := config.DBConfig{
		Host: cp.Host, Port: int(cp.Port), Name: cp.Database, User: cp.User, Password: cp.Password,
		PoolMax: 5, PoolIdle: 1,
	}
	// Central DIISI eksplisit (beda dari workflow_e2e): scheduler menuntut pool sentral, dan
	// connMgr.Central() tak punya fallback ke pool tenant — persis seperti di produksi.
	centralCfg := config.CentralDBConfig{
		Host: cp.Host, Port: int(cp.Port), Name: cp.Database, User: cp.User, Password: cp.Password,
		PoolMax: 5, PoolIdle: 1,
	}
	resolver := identitydb.NewTenantResolver(identitydb.NewTenantRepo(identityPool))
	connMgr := db.NewTenantConnManager(resolver, sharedCfg, centralCfg)
	t.Cleanup(connMgr.Close)
	tenantDB := db.NewTenantRoutingConn(connMgr)
	centralDB := db.NewCentralRoutingConn(connMgr)

	tenantPool, err := connMgr.Tenant(ctx, tenantID)
	if err != nil {
		t.Fatalf("tenant pool: %v", err)
	}
	centralPool, err := connMgr.Central(ctx)
	if err != nil {
		t.Fatalf("central pool: %v", err)
	}

	// --- Role tenant: aktor + dua penerima yang HARUS bisa dibedakan ---
	roleRepo := tenantroledb.NewTenantRoleRepo(tenantPool)
	aktorRoleID := uuid.New()
	if err := roleRepo.Save(ctx, &tenantroledomain.TenantRole{
		ID: aktorRoleID, Name: "operator_surat", Label: "Operator Surat",
		Permissions: []string{
			smdomain.PermSuratBuat, smdomain.PermSuratDisposisi,
			coreWf.PermInstanceMulai, coreWf.PermInstanceTransisi, coreWf.PermInstanceBaca,
		},
	}); err != nil {
		t.Fatalf("seed role operator_surat: %v", err)
	}
	pimpinanRoleID := uuid.New()
	if err := roleRepo.Save(ctx, &tenantroledomain.TenantRole{
		ID: pimpinanRoleID, Name: "pimpinan_opd", Label: "Pimpinan OPD",
	}); err != nil {
		t.Fatalf("seed role pimpinan_opd: %v", err)
	}
	agendarisRoleID := uuid.New()
	if err := roleRepo.Save(ctx, &tenantroledomain.TenantRole{
		ID: agendarisRoleID, Name: "agendaris_surat", Label: "Agendaris",
	}); err != nil {
		t.Fatalf("seed role agendaris_surat: %v", err)
	}

	personID := uuid.New()    // aktor
	pimpinanID := uuid.New()  // penerima ESKALASI SLA
	agendarisID := uuid.New() // penerima NOTIFIKASI transisi
	assignRepo := tenantroledb.NewTenantRoleAssignmentRepo(tenantPool)
	for _, a := range []struct {
		user uuid.UUID
		role uuid.UUID
	}{{personID, aktorRoleID}, {pimpinanID, pimpinanRoleID}, {agendarisID, agendarisRoleID}} {
		if err := assignRepo.Save(ctx, &tenantroledomain.TenantRoleAssignment{
			ID: uuid.New(), UserID: a.user, RoleID: a.role,
			AssignedBy: uuid.New(), ValidFrom: time.Now().Add(-time.Hour),
		}); err != nil {
			t.Fatalf("seed assignment: %v", err)
		}
	}

	bus, err := eventbus.NewFromConfig(
		config.EventBusConfig{Driver: "memory"}, eventbus.NewSchemaRegistry())
	if err != nil {
		t.Fatalf("event bus: %v", err)
	}
	store, err := storage.NewFromConfig(config.StorageConfig{
		Driver: "local", Endpoint: t.TempDir(), Bucket: "test"})
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	metrics := observability.NewPrometheusMetrics()
	logger := observability.NewLogger(observability.LogOptions{Level: "error", Format: "text"})
	sequenceGen := sequence.NewDBGenerator(connMgr)

	cryptoSvc, err := crypto.NewFromConfig(&config.AppConfig{
		Env: "production",
		Crypto: config.CryptoConfig{
			KMSDriver:   crypto.DriverStatic,
			MasterKey:   base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x5A}, 32)),
			DEKCacheTTL: time.Minute,
		},
	}, identityPool)
	if err != nil {
		t.Fatalf("crypto: %v", err)
	}
	userResolver, err := infrauser.NewDBResolver(connMgr, cryptoSvc)
	if err != nil {
		t.Fatalf("NewDBResolver: %v", err)
	}
	cloneWriter, err := identitysync.NewTenantDBWriter(connMgr, cryptoSvc)
	if err != nil {
		t.Fatalf("clone writer: %v", err)
	}
	if err := cloneWriter.Upsert(ctx, tenantID, identitysync.UserProfileClone{
		PersonID: personID, AssignmentID: uuid.New(),
		NIK: "3578010101900002", NIP: "199001012015031002",
		NamaLengkap: "Sri Wahyuni", EmploymentStatus: "asn",
	}); err != nil {
		t.Fatalf("seed clone user: %v", err)
	}

	// --- Perakitan yang diuji: urutan SAMA dengan run() ---
	router := gateway.NewRouter()
	workflowActions := coreWf.NewActionRegistry()
	app := domain.NewApp(tenantDB, centralDB, bus, bus, sequenceGen, metrics, store,
		userResolver, workflowActions, router)

	registry := domain.NewRegistry()
	registry.Register(modules.All()...)
	if err := registry.Validate(); err != nil {
		t.Fatalf("validate modul: %v", err)
	}
	if err := wireModuleEventSchemas(ctx, registry, bus, logger); err != nil {
		t.Fatalf("wireModuleEventSchemas: %v", err)
	}
	for _, m := range registry.Modules() {
		if err := m.Bootstrap(ctx, app); err != nil {
			t.Fatalf("bootstrap modul %q: %v", m.Manifest().Name, err)
		}
	}
	workflowSeeds, err := collectWorkflowSeeds(registry)
	if err != nil {
		t.Fatalf("collectWorkflowSeeds: %v", err)
	}

	notifSeeds, err := collectNotificationSeeds(registry, EscalationTemplateKey)
	if err != nil {
		t.Fatalf("collectNotificationSeeds: %v", err)
	}
	notifRuntimes := newNotificationFactory(connMgr, cryptoSvc, testMessageSender(t), notifSeeds,
		metrics, logger)
	sched, err := wireScheduler(ctx, centralPool, connMgr, notifRuntimes,
		20*time.Millisecond, time.Minute, metrics, logger)
	if err != nil {
		t.Fatalf("wireScheduler: %v", err)
	}
	runtimes := newWorkflowFactory(connMgr, workflowActions, workflowSeeds, logger,
		sched.deadlines, notifRuntimes)
	gatewaywf.MountRoutes(router, gatewaywf.NewHandler(runtimes))

	// Bangun tumpukan tenant sekali di muka: skema WORKFLOW ada dan definisi ter-seed. Tumpukan
	// notifikasi TIDAK ikut disiapkan di sini — ia sengaja lazy (lihat TransitionNotifierFor),
	// jadi skema & seed templatenya baru dibuat saat ada transisi yang benar-benar ber-`notify:`.
	// Itu justru bagian dari yang dibuktikan test ini.
	if _, err := runtimes.RuntimeFor(port.WithTenant(ctx, tenantID), tenantID); err != nil {
		t.Fatalf("bangun tumpukan workflow tenant: %v", err)
	}

	// TIDAK ADA template yang diseed manual di sini — itu disengaja, dan itu setengah dari yang
	// dibuktikan test ini.
	//
	// Dua-duanya datang dari jalur produksi saat tumpukan notifikasi tenant disiapkan:
	// template eskalasi (milik FRAMEWORK) dari seedFrameworkTemplates, dan
	// `surat_masuk.surat_selesai` (milik MODUL) dari NotificationRef di manifest →
	// collectNotificationSeeds → seedTemplates. Menyeed salah satunya di sini akan menutupi
	// kegagalan seeder produksi: hijau di test, gagal di instalasi baru mana pun — persis
	// keadaan yang berlaku sebelum jalur seeding modul ada.

	// Pilihan template tenant + binding peran GENERIK definisi → role KONKRET tenant.
	templates := infrawf.NewDBTemplateStore(tenantPool, infrawf.NewDBStore(tenantPool))
	if err := templates.SetTenantTemplate(coreWf.TenantWorkflowConfig{
		TenantID: tenantID, Slot: slot, TemplateID: "surat_masuk.disposisi.standar",
		RoleBindings: map[string]string{
			"sekretaris_daerah": "pimpinan_opd",    // tujuan eskalasi SLA
			"agendaris":         "agendaris_surat", // tujuan notifikasi transisi
		},
	}); err != nil {
		t.Fatalf("set pilihan template tenant: %v", err)
	}

	// --- Stack HTTP produksi ---
	secret := []byte("e2e-sla-secret-0123456789-abcdefg")
	codec := identitytoken.NewJWTCodec(identitytoken.Options{
		Secret: secret, TTL: time.Hour, Revoked: identitydb.NewRevokedTokenStore(identityPool)})
	centralCatalog, err := identitydb.NewCentralRoleCatalog(ctx, identitydb.NewCentralRoleRepo(identityPool))
	if err != nil {
		t.Fatalf("central catalog: %v", err)
	}
	handler := buildServerHandler(serverDeps{
		router:         router,
		verifier:       codec,
		evalFactory:    newEvaluatorFactory(centralCatalog, connMgr, registry.StrictPermissions(), 0),
		tenantResolver: resolver,
		rateLimiter:    ratelimit.NewMemory(nil),
		rateLimit:      config.RateLimitConfig{Enabled: false},
		logger:         logger,
	})
	token, err := codec.Issue(ctx, port.Claims{
		PersonID: personID, Persona: "employee", EmploymentStatus: "asn",
		TenantID: tenantID, TenantRoles: []string{"operator_surat"},
	})
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	call := func(method, target, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	// 1. Surat + instance + transisi "disposisi" → masuk state "didisposisi" (sla_hours 72).
	rec := call(http.MethodPost, "/surat-masuk", `{
		"NomorSurat":"020/IN/2025",
		"TanggalSurat":"2025-04-01T00:00:00Z",
		"TanggalAgenda":"2025-04-01T00:00:00Z",
		"Pengirim":"Dinas Z",
		"Perihal":"Permohonan izin",
		"Sifat":"biasa"
	}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /surat-masuk: %d — %s", rec.Code, rec.Body.String())
	}
	var surat struct {
		ID uuid.UUID `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &surat); err != nil || surat.ID == uuid.Nil {
		t.Fatalf("id surat tak terbaca: %v — %s", err, rec.Body.String())
	}

	rec = call(http.MethodPost, "/workflow/instances",
		`{"slot":"`+slot+`","entity_id":"`+surat.ID.String()+`"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /workflow/instances: %d — %s", rec.Code, rec.Body.String())
	}
	var started struct {
		ID uuid.UUID `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &started); err != nil {
		t.Fatalf("respons start tak terbaca: %v — %s", err, rec.Body.String())
	}

	rec = call(http.MethodPost, "/workflow/instances/"+started.ID.String()+"/transitions",
		`{"action":"disposisi","params":{"kepada_jabatan":"kabag_umum","instruksi":"tindak lanjuti"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST transitions disposisi: %d — %s", rec.Code, rec.Body.String())
	}

	// 2. BUKTI: deadline SLA terjadwal di DB SENTRAL, ber-tenant, lewat jalur request biasa.
	var jobID uuid.UUID
	var jobTenant, jobKey string
	var nextRun time.Time
	if err := centralPool.QueryRow(ctx, `
		SELECT id, tenant_id, job_key, next_run_at FROM gov.scheduled_jobs
		WHERE job_key = $1 AND enabled`, infrawf.EscalationJobKey,
	).Scan(&jobID, &jobTenant, &jobKey, &nextRun); err != nil {
		t.Fatalf("deadline SLA tak terjadwal di DB sentral: %v — engine tak menerima WithDeadlines", err)
	}
	if jobTenant != tenantID {
		t.Fatalf("tenant_id baris job = %q, mau %q — handler tak akan bisa merutekan ke tenant DB", jobTenant, tenantID)
	}
	if sisa := time.Until(nextRun); sisa < 71*time.Hour {
		t.Fatalf("next_run_at hanya %v dari sekarang, mau ~72 jam (sla_hours definisi)", sisa)
	}

	// 3. Majukan deadline ke masa lalu lalu jalankan satu siklus — eskalasi harus mendarat di
	// inbox pemegang KONKRET "pimpinan_opd" (binding dari peran generik "sekretaris_daerah").
	if _, err := centralPool.Exec(ctx,
		`UPDATE gov.scheduled_jobs SET next_run_at = now() - interval '1 minute' WHERE id = $1`,
		jobID); err != nil {
		t.Fatalf("majukan deadline: %v", err)
	}
	n, err := sched.runner.RunDue(ctx)
	if err != nil {
		t.Fatalf("RunDue: %v", err)
	}
	if n != 1 {
		t.Fatalf("RunDue menjalankan %d job, mau 1", n)
	}
	// Riwayat eksekusi harus SUKSES; job yang gagal tetap dihitung oleh RunDue.
	var status, jobErr string
	if err := centralPool.QueryRow(ctx,
		`SELECT status, error FROM gov.job_runs WHERE schedule_id = $1 ORDER BY started_at DESC LIMIT 1`,
		jobID).Scan(&status, &jobErr); err != nil {
		t.Fatalf("riwayat job tak tercatat: %v", err)
	}
	if status != string(scheduler.StatusSuccess) {
		t.Fatalf("job eskalasi gagal: status=%q error=%q", status, jobErr)
	}

	inbox := infraNotif.NewDBInAppInbox(tenantPool)
	items, err := inbox.List(ctx, tenantID, pimpinanID.String(), 0)
	if err != nil {
		t.Fatalf("list inbox pimpinan: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("inbox pimpinan_opd = %d item, mau 1 (eskalasi SLA ke role konkret)", len(items))
	}
	if items[0].TemplateKey != EscalationTemplateKey {
		t.Errorf("template eskalasi = %q, mau %q", items[0].TemplateKey, EscalationTemplateKey)
	}
	// Kontrol negatif: eskalasi TIDAK boleh menyasar aktor maupun agendaris.
	for nama, id := range map[string]uuid.UUID{"aktor": personID, "agendaris": agendarisID} {
		lain, err := inbox.List(ctx, tenantID, id.String(), 0)
		if err != nil {
			t.Fatalf("list inbox %s: %v", nama, err)
		}
		if len(lain) != 0 {
			t.Fatalf("inbox %s berisi %d item setelah eskalasi — binding peran bocor", nama, len(lain))
		}
	}

	// 4. Transisi "selesai" → notify agendaris (jalur NOTIFIKASI TRANSISI, bukan scheduler).
	rec = call(http.MethodPost, "/workflow/instances/"+started.ID.String()+"/transitions",
		`{"action":"selesai"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST transitions selesai: %d — %s", rec.Code, rec.Body.String())
	}
	items, err = inbox.List(ctx, tenantID, agendarisID.String(), 0)
	if err != nil {
		t.Fatalf("list inbox agendaris: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("inbox agendaris_surat = %d item, mau 1 (notify transisi ke role konkret)", len(items))
	}
	if items[0].TemplateKey != "surat_masuk.surat_selesai" {
		t.Errorf("template notifikasi transisi = %q, mau surat_masuk.surat_selesai", items[0].TemplateKey)
	}

	// 5. Loop scheduler benar-benar berjalan & berhenti bersih lewat jalur yang dipakai run().
	schedCtx, stopScheduler := context.WithCancel(context.Background())
	done := sched.runner.Start(schedCtx)
	time.Sleep(100 * time.Millisecond) // beberapa tick pada interval 20ms
	stopScheduler()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("runner tak berhenti setelah ctx dibatalkan — run() akan menutup pool di bawah kaki job")
	}
}
