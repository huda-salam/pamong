package usecase

import (
	"time"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/modules/surat_masuk/domain"
	"github.com/huda-salam/pamong/port"
)

// CreateSuratMasuk adalah use case Tier 3: satu transaksi atomik berisi business logic.
// Dependency disuntik lewat port (lihat bootstrap.go) — use case tidak tahu infra konkret.
type CreateSuratMasuk struct {
	repo      domain.SuratRepository
	seq       port.SequenceGenerator
	publisher port.EventPublisher
	metrics   port.MetricsPort
}

// NewCreateSuratMasuk konstruktor; dipanggil di bootstrap.
func NewCreateSuratMasuk(
	repo domain.SuratRepository,
	seq port.SequenceGenerator,
	pub port.EventPublisher,
	m port.MetricsPort,
) *CreateSuratMasuk {
	return &CreateSuratMasuk{repo: repo, seq: seq, publisher: pub, metrics: m}
}

// CreateSuratMasukInput adalah DTO masuk; tanpa field yang dihasilkan sistem
// (nomor_agenda, status di-generate / dikelola engine).
type CreateSuratMasukInput struct {
	NomorSurat    string
	TanggalSurat  time.Time
	TanggalAgenda time.Time
	Pengirim      string
	Perihal       string
	Sifat         string
}

// Execute menjalankan use case. Urutan baku (CODE_CONVENTION #5):
// permission -> validasi -> logic(port) -> persist -> event.
func (uc *CreateSuratMasuk) Execute(ctx port.AuthContext, in CreateSuratMasukInput) (*domain.SuratMasuk, error) {
	// 1. Permission — WAJIB baris pertama [linter: handler-must-check-permission].
	if err := ctx.RequirePermission(domain.PermSuratBuat); err != nil {
		return nil, err
	}

	// 2. Generate nomor agenda (configurable per-tenant, thread-safe) lewat port.
	nomorAgenda, err := uc.seq.Next(ctx, ctx.TenantID(), "{tahun}/AG/{nomor:5}", in.TanggalAgenda.Year())
	if err != nil {
		return nil, err
	}

	// 3. Bentuk entity; status awal "diterima" sesuai initial_state workflow.
	s := &domain.SuratMasuk{
		ID:            uuid.New(), // ID di-generate aplikasi, bukan auto-increment DB.
		NomorAgenda:   nomorAgenda,
		NomorSurat:    in.NomorSurat,
		TanggalSurat:  in.TanggalSurat,
		TanggalAgenda: in.TanggalAgenda,
		Pengirim:      in.Pengirim,
		Perihal:       in.Perihal,
		Sifat:         in.Sifat,
		Status:        "diterima",
	}

	// 4. Validasi invariant domain.
	if err := s.Validate(); err != nil {
		return nil, err
	}

	// 5. Persist. Audit & fiscal-check dijalankan framework otomatis (entity Auditable +
	//    Lockable) — use case tidak memanggilnya manual.
	if err := uc.repo.Save(ctx, s); err != nil {
		return nil, err
	}

	// 6. Publish event (schema terdaftar di manifest, pakai konstanta).
	_ = uc.publisher.Publish(ctx, port.Event{
		Name:     domain.EventSuratDiterima,
		TenantID: ctx.TenantID(),
		CausedBy: ctx.PersonID().String(),
		Payload: domain.SuratDiterimaPayload{
			SuratID: s.ID, NomorAgenda: s.NomorAgenda, Sifat: s.Sifat, DiterimaAt: time.Now(),
		},
	})

	uc.metrics.IncrCounter("surat_masuk.created", map[string]string{"sifat": s.Sifat})
	return s, nil
}
