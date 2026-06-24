package usecase_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/modules/surat_masuk/domain"
	"github.com/huda-salam/pamong/modules/surat_masuk/usecase"
	"github.com/huda-salam/pamong/testkit"
)

// stubDisposisiRepo adalah mock lokal DisposisiRepository.
// Mock generik (testkit.MockRepo) tidak bisa memenuhi interface ini karena ListBySurat
// spesifik untuk modul; stub lokal adalah pola yang benar untuk kasus ini.
type stubDisposisiRepo struct{}

func (r *stubDisposisiRepo) Save(_ context.Context, _ *domain.Disposisi) error { return nil }
func (r *stubDisposisiRepo) ListBySurat(_ context.Context, _ uuid.UUID) ([]*domain.Disposisi, error) {
	return nil, nil
}

func TestDisposisiSurat_PermissionDenied(t *testing.T) {
	uc := usecase.NewDisposisiSurat(
		testkit.NewMockRepo[domain.SuratMasuk](),
		&stubDisposisiRepo{},
		testkit.NewMockUserResolver(),
		testkit.NewMockPublisher(),
	)
	ctx := testkit.NewContext(t, testkit.WithTenant("pemkot-a")) // tanpa permission disposisi

	_, err := uc.Execute(ctx, usecase.DisposisiSuratInput{
		SuratID: uuid.New(), KepadaJabatan: "Kabid", Instruksi: "tindak lanjuti",
	})
	if !testkit.IsPermissionDenied(err) {
		t.Fatalf("harus permission denied, dapat: %v", err)
	}
}
