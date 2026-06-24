package usecase

import (
	"time"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/modules/surat_masuk/domain"
	"github.com/huda-salam/pamong/port"
)

// DisposisiSurat adalah use case yang DIPANGGIL oleh action workflow "disposisi".
// Catatan penting: workflow hanya MEMANGGIL use case ini; seluruh business logic ada di
// sini, bukan di YAML (CODING_PHILOSOPHY #5, linter: workflow-action-no-logic).
type DisposisiSurat struct {
	surat     domain.SuratRepository
	disposisi domain.DisposisiRepository
	pegawai   domain.PegawaiResolver // dependency ke kepegawaian LEWAT PORT
	publisher port.EventPublisher
}

func NewDisposisiSurat(
	s domain.SuratRepository,
	d domain.DisposisiRepository,
	p domain.PegawaiResolver,
	pub port.EventPublisher,
) *DisposisiSurat {
	return &DisposisiSurat{surat: s, disposisi: d, pegawai: p, publisher: pub}
}

type DisposisiSuratInput struct {
	SuratID       uuid.UUID
	KepadaJabatan string // PERAN/jabatan tujuan; resolusi ke orang di layer permission
	Instruksi     string
}

func (uc *DisposisiSurat) Execute(ctx port.AuthContext, in DisposisiSuratInput) (*domain.Disposisi, error) {
	// 1. Permission.
	if err := ctx.RequirePermission(domain.PermSuratDisposisi); err != nil {
		return nil, err
	}

	// 2. Pastikan surat ada.
	s, err := uc.surat.FindByID(ctx, in.SuratID)
	if err != nil {
		return nil, err
	}

	// 3. Jabatan pendisposisi diambil dari profil actor lewat port (bukan query langsung
	//    ke DB/identity kepegawaian).
	prof, err := uc.pegawai.ResolveByID(ctx, ctx.PersonID())
	if err != nil {
		return nil, err
	}

	// 4. Bentuk & simpan disposisi.
	d := &domain.Disposisi{
		ID:            uuid.New(),
		SuratID:       s.ID,
		DariJabatan:   prof.JabatanLokal,
		KepadaJabatan: in.KepadaJabatan,
		Instruksi:     in.Instruksi,
		Tanggal:       time.Now(),
	}
	if err := uc.disposisi.Save(ctx, d); err != nil {
		return nil, err
	}

	// 5. Event; modul notifikasi akan me-routing ke PERAN tujuan (fallback PLT).
	_ = uc.publisher.Publish(ctx, port.Event{
		Name:     domain.EventSuratDidisposisi,
		TenantID: ctx.TenantID(),
		CausedBy: ctx.PersonID().String(),
		Payload: domain.SuratDidisposisiPayload{
			SuratID: s.ID, DisposisiID: d.ID, KepadaJabatan: d.KepadaJabatan,
		},
	})
	return d, nil
}
