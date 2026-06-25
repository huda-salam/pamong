package surat_masuk

import (
	"context"

	"github.com/huda-salam/pamong/core/domain"
	smdomain "github.com/huda-salam/pamong/modules/surat_masuk/domain"
)

// Module mengimplementasi domain.Module. Compile-time assertion memastikan kontrak.
var _ domain.Module = (*Module)(nil)

// Module adalah modul persuratan: surat masuk dan disposisi berjenjang.
type Module struct{}

// Manifest mendeklarasikan identitas, entity, event, permission, dan dependency modul.
// Ini titik tunggal yang dibaca registry; tidak ada penemuan implisit.
func (m *Module) Manifest() domain.Manifest {
	return domain.Manifest{
		Name:    "surat_masuk",
		Version: "1.0.0",
		Domain:  "persuratan",

		// Tidak ada dependency MODUL keras. Data pegawai diakses lewat UserResolver
		// (port framework), dan event kepegawaian dikonsumsi secara loose (lihat
		// Events.Consumes) — keduanya tidak mensyaratkan modul kepegawaian ikut
		// di-load. DependsOn hanya untuk dependency keras (divalidasi DAG saat boot):
		// dep ke modul tak terdaftar = panic. Karena itu dibiarkan kosong.
		DependsOn: nil,

		// Persuratan ditutup per tahun: surat tahun lalu tidak dimutasi, tidak
		// dibutuhkan saat proses harian, tidak berdampak operasional → cutoff.
		DataLifecycle: "annual_cutoff",

		Entities: []domain.EntityDef{
			smdomain.EntitySuratMasuk,
			smdomain.EntityDisposisi,
		},

		Events: domain.EventManifest{
			Produces: []domain.EventDef{
				{Name: smdomain.EventSuratDiterima, Schema: smdomain.SuratDiterimaPayload{}},
				{Name: smdomain.EventSuratDidisposisi, Schema: smdomain.SuratDidisposisiPayload{}},
			},
			// Contoh: modul lain memublikasikan event yang relevan bagi persuratan.
			Consumes: []domain.EventSubscription{
				{Event: "kepegawaian.pegawai.mutasi", Handler: "OnPegawaiMutasi"},
			},
		},

		Permissions: domain.PermissionManifest{
			Groups: []domain.PermissionGroup{
				{
					Name: "agendaris",
					Permissions: []domain.PermissionDef{
						{Name: smdomain.PermSuratBuat},
						{Name: smdomain.PermSuratBaca},
					},
				},
				{
					Name: "pimpinan",
					Permissions: []domain.PermissionDef{
						{Name: smdomain.PermSuratDisposisi},
						{Name: smdomain.PermSuratBaca},
					},
				},
			},
			// Permission yang boleh dirujuk modul lain (mis. dalam guard workflow).
			Exports: []string{smdomain.PermSuratBaca},
		},

		Workflows: []domain.WorkflowRef{
			{Path: "workflows/disposisi.yaml"},
		},
	}
}

// Bootstrap melakukan wiring DI modul. Satu-satunya tempat port di-bind ke adapter.
func (m *Module) Bootstrap(ctx context.Context, app *domain.App) error {
	return bootstrap(ctx, app)
}
