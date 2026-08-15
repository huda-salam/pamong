// workflow.go merakit RUNTIME workflow di composition root (PR-W4a, ADR-022): satu tumpukan
// per tenant (definition store + template store + instance store + engine), dirakit dari pool
// tenant yang di-resolve ULANG pada setiap permintaan.
//
// Kenapa per-tenant dan bukan satu engine proses-lebar: definisi workflow hidup di TENANT DB
// (gov.workflow_definitions), sementara port DefinitionStore/TemplateStore tak membawa ctx
// maupun tenant sama sekali — jadi tak ada nilai apa pun yang bisa dipakai satu store bersama
// untuk memilih DB yang benar. Isolasi di sini karenanya STRUKTURAL: objek yang melayani tenant
// A secara fisik tak terhubung ke DB tenant B. Alternatif (menambah ctx ke empat port core)
// ditimbang dan ditolak di ADR-022.
//
// Presedennya sudah hidup di repo ini: evaluator_factory.go (catalog role per-tenant) dan
// infra/notification.DBRecipientDirectory (pool + tenantID tetap).
package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/huda-salam/pamong/core/domain"
	coreWf "github.com/huda-salam/pamong/core/workflow"
	gatewaywf "github.com/huda-salam/pamong/gateway/workflow"
	"github.com/huda-salam/pamong/infra/db"
	infrawf "github.com/huda-salam/pamong/infra/workflow"
	"github.com/huda-salam/pamong/port"
)

// workflowFactory merakit tumpukan workflow untuk satu tenant.
//
// actions dan guard sengaja DIBAGI semua tenant: keduanya adalah KODE (pemetaan nama→use case,
// dan compiler ekspresi guard), bukan data tenant. Menggandakannya per tenant hanya akan
// menggandakan cache guard tanpa menambah isolasi apa pun.
//
// Yang TIDAK di-cache: tumpukan itu sendiri. Lihat RuntimeFor.
type workflowFactory struct {
	pools   tenantPoolProvider
	actions *coreWf.ActionRegistry
	guard   coreWf.GuardEvaluator
	seeds   []domain.WorkflowRef // definisi baseline seluruh modul terdaftar (FS ter-embed)
	logger  port.Logger

	mu       sync.Mutex
	prepared map[*db.Pool]struct{} // DB yang skemanya sudah dipastikan & seed-nya sudah ditanam
}

// tenantPoolProvider adalah seam ke db.TenantConnManager. Ia interface agar test bisa
// membuktikan sifat yang paling mudah hilang di sini: pool di-MINTA ULANG tiap permintaan.
type tenantPoolProvider interface {
	Tenant(ctx context.Context, tenantID string) (*db.Pool, error)
}

var _ gatewaywf.RuntimeProvider = (*workflowFactory)(nil)

// newWorkflowFactory merakit factory. seeds dikumpulkan dari manifest modul yang SUDAH
// tervalidasi (lihat collectWorkflowSeeds) — modul yang gagal validasi tak boleh menyumbang
// definisi ke DB tenant mana pun.
func newWorkflowFactory(pools tenantPoolProvider, actions *coreWf.ActionRegistry,
	seeds []domain.WorkflowRef, logger port.Logger) *workflowFactory {
	return &workflowFactory{
		pools:    pools,
		actions:  actions,
		guard:    coreWf.NewGuardEvaluator(),
		seeds:    seeds,
		logger:   logger,
		prepared: make(map[*db.Pool]struct{}),
	}
}

// RuntimeFor merakit tumpukan workflow milik satu tenant, dari pool yang di-resolve SAAT ITU.
//
// Tumpukannya sengaja tidak di-cache per tenant. Cache seperti itu tampak jelas menguntungkan —
// dan justru membekukan hal yang paling tidak boleh beku: db.TenantConnManager.Tenant() membaca
// id.tenant_registry pada SETIAP panggilan dan mengunci pool-nya pada (host, nama DB). Saat tenant
// dipindah ke server DB sendiri (Tier 2/3 — operasi yang memang dirancang tanpa perubahan kode,
// CLAUDE.md §Tenant tier), seluruh konsumen lain otomatis mengikuti registry ke DB baru, sementara
// tumpukan yang ter-cache akan terus menulis ke DB LAMA sampai proses di-restart: transisi
// tercatat di tempat yang sudah ditinggalkan, tanpa satu pun error.
//
// Biaya merakit ulang mendekati nol — store-nya struct tipis di atas pool, dan engine hanya
// memegang rujukan. Yang mahal (ensure schema + seed definisi) tetap dilakukan sekali per DB
// lewat prepare.
func (f *workflowFactory) RuntimeFor(ctx context.Context, tenantID string) (gatewaywf.Runtime, error) {
	if tenantID == "" {
		return gatewaywf.Runtime{}, fmt.Errorf("tenant kosong: tumpukan workflow selalu milik satu tenant")
	}
	pool, err := f.pools.Tenant(ctx, tenantID)
	if err != nil {
		return gatewaywf.Runtime{}, fmt.Errorf("pool tenant %q: %w", tenantID, err)
	}

	defs := infrawf.NewDBStore(pool)
	if err := f.prepare(ctx, tenantID, pool, defs); err != nil {
		return gatewaywf.Runtime{}, err
	}

	templates := infrawf.NewDBTemplateStore(pool, defs)
	instances := infrawf.NewDBInstanceStore(pool)
	return gatewaywf.Runtime{
		Engine:    coreWf.New(defs, f.actions, f.guard, coreWf.WithTemplates(templates)),
		Instances: instances,
	}, nil
}

// prepare memastikan skema workflow ada dan definisi baseline modul tertanam di DB ini —
// sekali per DB, bukan sekali per request.
//
// Penandanya ber-kunci POOL, bukan tenant: pool identik dengan (host, nama DB) di
// TenantConnManager, jadi tenant yang pindah DB otomatis dianggap belum disiapkan dan DB barunya
// ikut di-seed. Kegagalan TIDAK ditandai — percobaan berikutnya mencoba ulang. Dua request
// bersamaan bisa sama-sama menyiapkan; keduanya idempoten (ensure schema di bawah advisory lock,
// seed lewat SeedIfAbsent).
//
// Engine yang dirakit di RuntimeFor SENGAJA tanpa WithDeadlines & WithNotifier: penjadwalan SLA
// dan notifikasi transisi menuntut scheduler.Runner (goroutine berumur panjang ber-shutdown) dan
// hub notifikasi per-tenant — keduanya lingkup PR-W4b. Konsekuensinya nyata dan harus disebut:
// sampai W4b mendarat, state ber-`sla_hours` tidak menjadwalkan eskalasi apa pun dan `notify:`
// pada transisi tidak mengirim apa pun. Keduanya no-op yang sudah menjadi kontrak engine
// (deadlines/notifier nil), bukan kegagalan diam-diam yang baru diperkenalkan di sini.
// DEFERRED(PR-W4b): WithDeadlines(SchedulerDeadlines) + WithNotifier(NotifierTransition).
func (f *workflowFactory) prepare(ctx context.Context, tenantID string, pool *db.Pool,
	defs *infrawf.DBStore) error {
	f.mu.Lock()
	_, sudah := f.prepared[pool]
	f.mu.Unlock()
	if sudah {
		return nil
	}

	// Satu EnsureSchema cukup untuk SELURUH tabel workflow (definitions, tenant configs,
	// instances, instance locks) — semuanya dari satu set migrasi ter-embed yang sama.
	if err := defs.EnsureSchema(ctx); err != nil {
		return fmt.Errorf("schema workflow tenant %q: %w", tenantID, err)
	}

	// Seed definisi baseline modul ke DB tenant ini. SeedYAML melewati ID yang sudah ada, jadi
	// definisi yang sudah di-override tenant TIDAK ditimpa: file YAML adalah baseline developer,
	// DB adalah yang aktif (CLAUDE.md §Workflow as data).
	for _, ref := range f.seeds {
		if err := coreWf.SeedFS(ref.FS, ref.Path, defs); err != nil {
			return fmt.Errorf("seed workflow tenant %q: %w", tenantID, err)
		}
	}

	f.mu.Lock()
	f.prepared[pool] = struct{}{}
	f.mu.Unlock()
	if f.logger != nil {
		f.logger.Info(ctx, "skema & seed workflow tenant disiapkan",
			port.F("tenant", tenantID), port.F("seed_definisi", len(f.seeds)))
	}
	return nil
}

// collectWorkflowSeeds mengumpulkan definisi baseline dari manifest seluruh modul terdaftar.
//
// WorkflowRef tanpa FS ditolak di BOOT, bukan dibiarkan gagal saat tenant pertama memakai
// workflow: seed yang dibaca dari disk akan lulus di mesin developer (yang menjalankan binary
// dari root repo) lalu gagal di produksi — kelas kegagalan yang paling mahal karena baru muncul
// setelah rilis.
func collectWorkflowSeeds(reg *domain.Registry) ([]domain.WorkflowRef, error) {
	var seeds []domain.WorkflowRef
	for _, m := range reg.Modules() {
		mf := m.Manifest()
		for _, ref := range mf.Workflows {
			if ref.FS == nil {
				return nil, fmt.Errorf("modul %q: WorkflowRef %q tanpa FS ter-embed", mf.Name, ref.Path)
			}
			if ref.Path == "" {
				return nil, fmt.Errorf("modul %q: WorkflowRef tanpa Path", mf.Name)
			}
			seeds = append(seeds, ref)
		}
	}
	return seeds, nil
}
