package surat_masuk

import (
	"context"

	"github.com/huda-salam/pamong/core/domain"
	dbadapter "github.com/huda-salam/pamong/modules/surat_masuk/adapter/db"
	eventadapter "github.com/huda-salam/pamong/modules/surat_masuk/adapter/event"
	httpadapter "github.com/huda-salam/pamong/modules/surat_masuk/adapter/http"
	"github.com/huda-salam/pamong/modules/surat_masuk/usecase"
)

// bootstrap adalah SATU-SATUNYA tempat port di-bind ke adapter konkret untuk modul ini
// (CODING_PHILOSOPHY #2). Di sinilah dependency injection terjadi; tidak di tempat lain.
func bootstrap(ctx context.Context, app *domain.App) error {
	// Adapter persistensi (driven) — mengikat port domain ke implementasi Postgres.
	suratRepo := dbadapter.NewSuratRepo(app.DB())
	disposisiRepo := dbadapter.NewDisposisiRepo(app.DB())

	// Dependency lintas-modul disuntik sebagai PORT framework, bukan import kepegawaian.
	pegawai := app.UserResolver()

	// Use case — menerima port, bukan infra konkret.
	createUC := usecase.NewCreateSuratMasuk(suratRepo, app.Sequence(), app.Publisher(), app.Metrics())
	disposisiUC := usecase.NewDisposisiSurat(suratRepo, disposisiRepo, pegawai, app.Publisher())

	// Daftarkan action workflow: nama "DisposisiSurat" di YAML -> use case ini.
	app.Workflow().RegisterAction("DisposisiSurat", disposisiUC)

	// Driving adapter (HTTP) + registrasi rute.
	h := httpadapter.NewHandler(createUC, disposisiUC)
	app.Router().Post("/surat-masuk", h.CreateSuratMasuk)

	// Consumer event modul lain.
	app.Subscribe("kepegawaian.pegawai.mutasi", eventadapter.NewConsumer().OnPegawaiMutasi)

	return nil
}
