// workflow.go merakit RUNTIME workflow di composition root (PR-W4a, ADR-022): satu tumpukan
// per tenant (definition store + template store + instance store + engine), dibangun lazy saat
// tenant pertama dipakai lalu di-cache selama umur proses.
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

// workflowFactory membangun & meng-cache tumpukan workflow per tenant.
//
// actions dan guard sengaja DIBAGI semua tenant: keduanya adalah KODE (pemetaan nama→use case,
// dan compiler ekspresi guard), bukan data tenant. Menggandakannya per tenant hanya akan
// menggandakan cache guard tanpa menambah isolasi apa pun.
type workflowFactory struct {
	connMgr *db.TenantConnManager
	actions *coreWf.ActionRegistry
	guard   coreWf.GuardEvaluator
	seeds   []domain.WorkflowRef // definisi baseline seluruh modul terdaftar (FS ter-embed)
	logger  port.Logger

	mu    sync.RWMutex
	cache map[string]gatewaywf.Runtime
}

var _ gatewaywf.RuntimeProvider = (*workflowFactory)(nil)

// newWorkflowFactory merakit factory. seeds dikumpulkan dari manifest modul yang SUDAH
// tervalidasi (lihat collectWorkflowSeeds) — modul yang gagal validasi tak boleh menyumbang
// definisi ke DB tenant mana pun.
func newWorkflowFactory(connMgr *db.TenantConnManager, actions *coreWf.ActionRegistry,
	seeds []domain.WorkflowRef, logger port.Logger) *workflowFactory {
	return &workflowFactory{
		connMgr: connMgr,
		actions: actions,
		guard:   coreWf.NewGuardEvaluator(),
		seeds:   seeds,
		logger:  logger,
		cache:   make(map[string]gatewaywf.Runtime),
	}
}

// RuntimeFor mengembalikan (get-or-build) tumpukan workflow milik satu tenant.
//
// Build dilakukan DI LUAR lock agar cold-start satu tenant (yang menyentuh DB: ensure schema +
// seed) tak memblokir tenant lain; double-check saat menyimpan menjaga satu entri per tenant.
// Build ganda yang jarang untuk tenant yang sama tetap benar — ensure schema & seed keduanya
// idempoten. Kegagalan TIDAK di-cache: percobaan berikutnya mencoba ulang.
func (f *workflowFactory) RuntimeFor(ctx context.Context, tenantID string) (gatewaywf.Runtime, error) {
	if tenantID == "" {
		return gatewaywf.Runtime{}, fmt.Errorf("tenant kosong: tumpukan workflow selalu milik satu tenant")
	}
	f.mu.RLock()
	rt, ok := f.cache[tenantID]
	f.mu.RUnlock()
	if ok {
		return rt, nil
	}

	built, err := f.build(ctx, tenantID)
	if err != nil {
		return gatewaywf.Runtime{}, err
	}

	f.mu.Lock()
	if existing, ok := f.cache[tenantID]; ok {
		f.mu.Unlock()
		return existing, nil
	}
	f.cache[tenantID] = built
	f.mu.Unlock()
	return built, nil
}

// build merakit tumpukan untuk satu tenant: pool tenant → schema → seed definisi → store →
// engine.
//
// Engine di sini SENGAJA tanpa WithDeadlines & WithNotifier: penjadwalan SLA dan notifikasi
// transisi menuntut scheduler.Runner (goroutine berumur panjang ber-shutdown) dan hub notifikasi
// per-tenant — keduanya lingkup PR-W4b. Konsekuensinya nyata dan harus disebut: sampai W4b
// mendarat, state ber-`sla_hours` tidak menjadwalkan eskalasi apa pun dan `notify:` pada transisi
// tidak mengirim apa pun. Keduanya no-op yang sudah menjadi kontrak engine (deadlines/notifier
// nil), bukan kegagalan diam-diam yang baru diperkenalkan di sini.
// DEFERRED(PR-W4b): WithDeadlines(SchedulerDeadlines) + WithNotifier(NotifierTransition).
func (f *workflowFactory) build(ctx context.Context, tenantID string) (gatewaywf.Runtime, error) {
	pool, err := f.connMgr.Tenant(ctx, tenantID)
	if err != nil {
		return gatewaywf.Runtime{}, fmt.Errorf("pool tenant %q: %w", tenantID, err)
	}

	defs := infrawf.NewDBStore(pool)
	// Satu EnsureSchema cukup untuk SELURUH tabel workflow (definitions, tenant configs,
	// instances) — semuanya berasal dari satu set migrasi ter-embed yang sama, diterapkan di
	// bawah advisory lock & memo per-DB.
	if err := defs.EnsureSchema(ctx); err != nil {
		return gatewaywf.Runtime{}, fmt.Errorf("schema workflow tenant %q: %w", tenantID, err)
	}

	// Seed definisi baseline modul ke DB tenant ini. SeedYAML melewati ID yang sudah ada, jadi
	// definisi yang sudah di-override tenant TIDAK ditimpa: file YAML adalah baseline developer,
	// DB adalah yang aktif (CLAUDE.md §Workflow as data).
	for _, ref := range f.seeds {
		if err := coreWf.SeedFS(ref.FS, ref.Path, defs); err != nil {
			return gatewaywf.Runtime{}, fmt.Errorf("seed workflow tenant %q: %w", tenantID, err)
		}
	}

	templates := infrawf.NewDBTemplateStore(pool, defs)
	instances := infrawf.NewDBInstanceStore(pool)

	engine := coreWf.New(defs, f.actions, f.guard, coreWf.WithTemplates(templates))
	if f.logger != nil {
		f.logger.Info(ctx, "tumpukan workflow tenant dirakit",
			port.F("tenant", tenantID), port.F("seed_definisi", len(f.seeds)))
	}
	return gatewaywf.Runtime{Engine: engine, Instances: instances}, nil
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
