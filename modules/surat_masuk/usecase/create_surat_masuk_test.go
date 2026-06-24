package usecase_test

import (
	"testing"
	"time"

	"github.com/huda-salam/pamong/modules/surat_masuk/domain"
	"github.com/huda-salam/pamong/modules/surat_masuk/usecase"
	"github.com/huda-salam/pamong/testkit"
)

// Tiga skenario WAJIB per use case (CODING_PHILOSOPHY #9):
// happy path, permission-denied, validasi gagal.

func newUC(t *testing.T) (*usecase.CreateSuratMasuk, *testkit.MockPublisher) {
	t.Helper()
	repo := testkit.NewMockRepo[domain.SuratMasuk]()
	seq := testkit.NewMockSequence("2025/AG/00001")
	pub := testkit.NewMockPublisher()
	uc := usecase.NewCreateSuratMasuk(repo, seq, pub, testkit.NewMockMetrics())
	return uc, pub
}

func validInput() usecase.CreateSuratMasukInput {
	return usecase.CreateSuratMasukInput{
		NomorSurat:    "005/X/2025",
		TanggalSurat:  time.Date(2025, 10, 1, 0, 0, 0, 0, time.UTC),
		TanggalAgenda: time.Date(2025, 10, 2, 0, 0, 0, 0, time.UTC),
		Pengirim:      "Dinas A",
		Perihal:       "Undangan rapat",
		Sifat:         "biasa",
	}
}

func TestCreateSuratMasuk_HappyPath(t *testing.T) {
	uc, pub := newUC(t)
	ctx := testkit.NewContext(t,
		testkit.WithTenant("pemkot-a"),
		testkit.WithPermission(domain.PermSuratBuat),
	)

	s, err := uc.Execute(ctx, validInput())
	if err != nil {
		t.Fatalf("tak terduga: %v", err)
	}
	if s.NomorAgenda == "" {
		t.Error("nomor agenda harus ter-generate")
	}
	if s.Status != "diterima" {
		t.Errorf("status awal = %q, mau \"diterima\"", s.Status)
	}
	testkit.AssertEventPublished(t, pub, domain.EventSuratDiterima)
}

func TestCreateSuratMasuk_PermissionDenied(t *testing.T) {
	uc, _ := newUC(t)
	// Konteks TANPA permission buat.
	ctx := testkit.NewContext(t, testkit.WithTenant("pemkot-a"))

	_, err := uc.Execute(ctx, validInput())
	if !testkit.IsPermissionDenied(err) {
		t.Fatalf("harus permission denied, dapat: %v", err)
	}
}

func TestCreateSuratMasuk_ValidasiGagal(t *testing.T) {
	uc, _ := newUC(t)
	ctx := testkit.NewContext(t,
		testkit.WithTenant("pemkot-a"),
		testkit.WithPermission(domain.PermSuratBuat),
	)

	in := validInput()
	in.TanggalAgenda = time.Date(2025, 9, 30, 0, 0, 0, 0, time.UTC) // sebelum tanggal surat
	_, err := uc.Execute(ctx, in)
	if err == nil {
		t.Fatal("harus gagal validasi tanggal agenda < tanggal surat")
	}
}
