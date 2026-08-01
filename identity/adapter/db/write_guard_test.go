package db_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/identity/adapter/db"
	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/port"
	"github.com/huda-salam/pamong/testkit"
)

// connTakBolehTersentuh adalah DBConn yang menggagalkan test bila dipakai. Ia menyatakan
// properti yang sebenarnya dijaga: nilai yang melanggar invariant tak sekadar "ditolak
// nanti oleh constraint", melainkan tak pernah sampai ke DB — jadi tak ada seal(),
// tak ada baris setengah jadi, tak ada entri audit.
type connTakBolehTersentuh struct{ t *testing.T }

func (c connTakBolehTersentuh) QueryRow(context.Context, string, ...any) port.Row {
	c.t.Fatal("query dijalankan padahal nilainya melanggar invariant domain")
	return nil
}

func (c connTakBolehTersentuh) Query(context.Context, string, ...any) (port.Rows, error) {
	c.t.Fatal("query dijalankan padahal nilainya melanggar invariant domain")
	return nil, nil
}

func (c connTakBolehTersentuh) Exec(context.Context, string, ...any) (port.CommandTag, error) {
	c.t.Fatal("INSERT dijalankan padahal nilainya melanggar invariant domain")
	return nil, nil
}

// TestRepoIdentity_MenolakNilaiCacatDiPintuTulis mengunci penegakan invariant di lapis repo,
// bukan di use case.
//
// Kenapa ini bukan test yang mubazir atas Validate(): sebelum ini `Credential.Validate` tak
// punya SATU PUN pemanggil di luar test-nya sendiri — aturan bentuk pengenal (PR-3.8.6) hidup
// sebagai dokumentasi, menunggu penulis use case credential pertama mengingat memanggilnya.
// Yang dijaga di sini adalah sambungannya: repo yang menolak, bukan entity yang bisa menolak.
//
// Verifikasi mutasi: buang `Validate()` dari salah satu Save → subtest-nya gagal lewat
// connTakBolehTersentuh (query terlanjur jalan), bukan lewat assertion error.
func TestRepoIdentity_MenolakNilaiCacatDiPintuTulis(t *testing.T) {
	ctx := context.Background()
	conn := connTakBolehTersentuh{t: t}
	cr := testkit.NewMockCrypto()
	pid := uuid.New()

	t.Run("credential ber-CRLF", func(t *testing.T) {
		repo, err := db.NewCredentialRepo(conn, cr)
		if err != nil {
			t.Fatalf("NewCredentialRepo: %v", err)
		}
		// Nilai yang justru BERBAHAYA: ia me-resolve dengan sukses lewat blind index
		// (normalize() mem-trim CR/LF sebelum HMAC) lalu ditolak SMTP sebagai header
		// injection — beda respons itulah orakel keberadaan akun (REVIEW_BACKLOG A7).
		err = repo.Save(ctx, &domain.Credential{
			ID: uuid.New(), PersonID: pid, CredType: domain.CredEmail,
			CredValue: "budi@example.go.id\r\n",
		})
		if !errors.Is(err, domain.ErrCredValueFormat) {
			t.Fatalf("Save = %v, mau %v", err, domain.ErrCredValueFormat)
		}
	})

	t.Run("email person berspasi tepi", func(t *testing.T) {
		repo, err := db.NewPersonRepo(conn, cr)
		if err != nil {
			t.Fatalf("NewPersonRepo: %v", err)
		}
		err = repo.Save(ctx, &domain.Person{
			ID: uuid.New(), NIK: "3578010101900001", NamaLengkap: "Budi",
			Email: " budi@example.go.id",
		})
		if !errors.Is(err, domain.ErrEmailFormat) {
			t.Fatalf("Save = %v, mau %v", err, domain.ErrEmailFormat)
		}
	})

	t.Run("employment non-asn ber-NIP", func(t *testing.T) {
		repo, err := db.NewEmploymentRepo(conn, cr)
		if err != nil {
			t.Fatalf("NewEmploymentRepo: %v", err)
		}
		err = repo.Save(ctx, &domain.Employment{
			ID: uuid.New(), PersonID: pid, Status: domain.StatusNonASN,
			NIP: "199001012015011001",
		})
		if !errors.Is(err, domain.ErrNIPTerisiNonASN) {
			t.Fatalf("Save = %v, mau %v", err, domain.ErrNIPTerisiNonASN)
		}
	})
}
