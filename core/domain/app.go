package domain

import (
	"context"

	"github.com/huda-salam/pamong/port"
)

// Module adalah kontrak yang harus diimplementasi setiap modul bisnis.
// Manifest() mendeklarasikan identitas; Bootstrap() melakukan wiring DI.
type Module interface {
	Manifest() Manifest
	Bootstrap(ctx context.Context, app *App) error
}

// WorkflowRegistry memungkinkan modul mendaftarkan use case sebagai action workflow.
// Action HANYA boleh memanggil use case — tidak ada business logic di dalam action
// (linter: workflow-action-no-logic, CLAUDE.md #6.7).
type WorkflowRegistry interface {
	RegisterAction(name string, useCase any)
}

// App adalah DI container framework yang disuntik ke setiap Bootstrap().
// Ia menyediakan akses ke semua port infrastruktur yang sudah terkonfigurasi.
// Modul tidak boleh membuat koneksi sendiri — selalu lewat App.
type App struct {
	db           port.DBConn
	centralDB    port.DBConn
	publisher    port.EventPublisher
	subscriber   port.EventSubscriber
	sequence     port.SequenceGenerator
	metrics      port.MetricsPort
	storage      port.StoragePort
	userResolver port.UserResolver
	workflow     WorkflowRegistry
	router       port.Router
}

// NewApp membuat instance App. Dipanggil oleh binary utama setelah semua
// infrastructure di-init; hasilnya di-pass ke setiap Module.Bootstrap().
func NewApp(
	db port.DBConn,
	centralDB port.DBConn,
	pub port.EventPublisher,
	sub port.EventSubscriber,
	seq port.SequenceGenerator,
	metrics port.MetricsPort,
	storage port.StoragePort,
	users port.UserResolver,
	workflow WorkflowRegistry,
	router port.Router,
) *App {
	return &App{
		db:           db,
		centralDB:    centralDB,
		publisher:    pub,
		subscriber:   sub,
		sequence:     seq,
		metrics:      metrics,
		storage:      storage,
		userResolver: users,
		workflow:     workflow,
		router:       router,
	}
}

func (a *App) DB() port.DBConn { return a.db }

// CentralDB mengembalikan DBConn ke DB sentral (entity Residency=ResidencyCentral, ADR-005).
// Repo entity central WAJIB dibangun di atas ini, BUKAN DB() yang route per-tenant — tanpa
// pemisahan ini repo central diam-diam menyentuh DB tenant (DEFERRED Phase-5.1.2 #1, ditutup
// PR-5.1.2). Modul dengan entity ResidencyTenant (mayoritas) cukup pakai DB().
func (a *App) CentralDB() port.DBConn           { return a.centralDB }
func (a *App) Publisher() port.EventPublisher   { return a.publisher }
func (a *App) Sequence() port.SequenceGenerator { return a.sequence }
func (a *App) Metrics() port.MetricsPort        { return a.metrics }
func (a *App) Storage() port.StoragePort        { return a.storage }
func (a *App) UserResolver() port.UserResolver  { return a.userResolver }
func (a *App) Workflow() WorkflowRegistry       { return a.workflow }
func (a *App) Router() port.Router              { return a.router }

// Subscribe mendaftarkan handler untuk satu event. Dipanggil di Bootstrap modul.
func (a *App) Subscribe(event string, handler port.EventHandler) {
	if a.subscriber != nil {
		_ = a.subscriber.Subscribe(event, handler)
	}
}
