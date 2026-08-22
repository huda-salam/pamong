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

	// deadlines DIBAGI semua tenant: jadwal hidup di DB sentral (ADR-023), jadi satu adapter
	// di atas satu Runner melayani seluruh tenant. Tenant dibawa di baris job, bukan di objek.
	deadlines coreWf.DeadlineScheduler
	// notifs per-tenant: inbox & template notifikasi hidup di tenant DB. nil = notifikasi
	// transisi tak dipasang (dipakai unit test yang hanya menguji resolusi pool).
	notifs *notificationFactory

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
// deadlines & notifs sengaja PARAMETER, bukan setter opsional ber-chain: keduanya adalah
// perbedaan antara engine yang menjadwalkan SLA + mengirim notifikasi dan engine yang diam-diam
// tidak melakukan keduanya. Parameter memaksa setiap call site menyatakan pilihannya secara
// terlihat — setter yang boleh dilupakan adalah bentuk yang melahirkan komponen dorman (DoD 11).
func newWorkflowFactory(pools tenantPoolProvider, actions *coreWf.ActionRegistry,
	seeds []domain.WorkflowRef, logger port.Logger,
	deadlines coreWf.DeadlineScheduler, notifs *notificationFactory) *workflowFactory {
	return &workflowFactory{
		pools:     pools,
		actions:   actions,
		guard:     coreWf.NewGuardEvaluator(),
		seeds:     seeds,
		logger:    logger,
		deadlines: deadlines,
		notifs:    notifs,
		prepared:  make(map[*db.Pool]struct{}),
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

	opts := []coreWf.Option{coreWf.WithTemplates(templates)}
	if f.deadlines != nil {
		opts = append(opts, coreWf.WithDeadlines(f.deadlines))
	}

	if f.notifs != nil {
		// Notifier per-tenant: transisi ber-`notify:` mengirim ke inbox tenant INI. Dirakit di
		// sini (bukan sekali saat boot) karena inbox-nya hidup di tenant DB — alasan yang sama
		// dengan tumpukan workflow di atasnya, termasuk soal tenant yang pindah DB.
		//
		// `pool` diteruskan, bukan dibiarkan di-resolve ulang dari tenantID: TenantConnManager
		// membaca id.tenant_registry tiap panggilan, jadi resolve kedua = satu query tambahan ke
		// DB sentral pada SETIAP request workflow, termasuk transisi tanpa `notify:` sama sekali.
		// Perakitannya TERTUNDA sampai ada transisi ber-`notify:` (lihat TransitionNotifierFor):
		// GET riwayat instance tak boleh ikut membayar ensure-schema notifikasi, dan DB notifikasi
		// yang bermasalah tak boleh menjatuhkan seluruh endpoint workflow tenant ini.
		opts = append(opts, coreWf.WithNotifier(f.notifs.TransitionNotifierFor(tenantID, pool)))
	}

	return gatewaywf.Runtime{
		Engine:    coreWf.New(defs, f.actions, f.guard, opts...),
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
// RESOLVED(PR-W4b): engine kini menerima WithDeadlines & WithNotifier bila keduanya disuntikkan
// (lihat newWorkflowFactory). Sebelumnya keduanya nil, dan konsekuensinya adalah no-op yang SAH
// menurut kontrak engine — state ber-`sla_hours` tak menjadwalkan apa pun, `notify:` tak mengirim
// apa pun, tanpa satu pun error. Persis kelas kegagalan yang tak bisa dilihat test per-komponen,
// dan yang dibuktikan tertutup oleh sla_notification_e2e_integration_test.go.
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

	// Periksa definisi yang BENAR-BENAR tersimpan (bukan baseline ter-embed) terhadap template
	// yang tersedia. Pagar boot hanya melihat YAML di dalam binary; yang dieksekusi adalah isi DB,
	// dan keduanya berbeda pada tenant yang di-provision sebelum sebuah key di-rename maupun pada
	// definisi yang dikustomisasi tenant. Melaporkan, tidak menggagalkan — lihat docnya.
	if f.notifs != nil {
		laporStaleNotifyTemplates(ctx, tenantID, defs, f.seeds,
			f.notifs.templateKeysFor(ctx, tenantID, pool), f.logger)
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
