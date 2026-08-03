package sync_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/identity/sync"
)

// repoPersons/repoEmployments adalah repo identity minimal untuk menguji RepoCloneSource.
// Hanya FindByID yang dipakai; metode lain memenuhi interface dan gagal bila tersentuh —
// yang dibuktikan test bukan "hasilnya benar" saja, tapi juga jalur mana yang ditempuh.
type repoPersons struct {
	byID map[uuid.UUID]*domain.Person
	err  error
}

func (r *repoPersons) FindByID(_ context.Context, id uuid.UUID) (*domain.Person, error) {
	if r.err != nil {
		return nil, r.err
	}
	p, ok := r.byID[id]
	if !ok {
		return nil, errors.New("person tidak ditemukan")
	}
	return p, nil
}
func (r *repoPersons) Save(context.Context, *domain.Person) error { panic("tak boleh dipanggil") }
func (r *repoPersons) FindByNIK(context.Context, string) (*domain.Person, error) {
	panic("tak boleh dipanggil: lookup clone memakai id, bukan pengenal")
}

type repoEmployments struct {
	byID map[uuid.UUID]*domain.Employment
	err  error
}

func (r *repoEmployments) FindByID(_ context.Context, id uuid.UUID) (*domain.Employment, error) {
	if r.err != nil {
		return nil, r.err
	}
	e, ok := r.byID[id]
	if !ok {
		return nil, errors.New("employment tidak ditemukan")
	}
	return e, nil
}
func (r *repoEmployments) Save(context.Context, *domain.Employment) error {
	panic("tak boleh dipanggil")
}
func (r *repoEmployments) FindByNIP(context.Context, string) (*domain.Employment, error) {
	panic("tak boleh dipanggil: lookup clone memakai id, bukan pengenal")
}
func (r *repoEmployments) ListByPerson(context.Context, uuid.UUID) ([]*domain.Employment, error) {
	panic("tak boleh dipanggil")
}

func seedSource(t *testing.T) (*sync.RepoCloneSource, *domain.Person, *domain.Employment) {
	t.Helper()
	person := &domain.Person{
		ID: uuid.New(), NIK: "3578010101900001", NamaLengkap: "Budi", IsActive: true,
		Email: "budi@example.test", NoHP: "0812340001",
	}
	emp := &domain.Employment{
		ID: uuid.New(), PersonID: person.ID, Status: domain.StatusASN,
		NIP: "199001012015011001", IsActive: true,
	}
	src, err := sync.NewRepoCloneSource(
		&repoPersons{byID: map[uuid.UUID]*domain.Person{person.ID: person}},
		&repoEmployments{byID: map[uuid.UUID]*domain.Employment{emp.ID: emp}},
	)
	if err != nil {
		t.Fatalf("NewRepoCloneSource: %v", err)
	}
	return src, person, emp
}

func TestRepoCloneSource_MembacaPengenalPersonDanEmployment(t *testing.T) {
	src, person, emp := seedSource(t)

	ids, err := src.Identifiers(context.Background(), person.ID, emp.ID)
	if err != nil {
		t.Fatalf("Identifiers: %v", err)
	}
	if ids.NIK != person.NIK || ids.Email != person.Email || ids.NoHP != person.NoHP {
		t.Fatalf("pengenal person tidak sesuai: %+v", ids)
	}
	if ids.NIP != emp.NIP {
		t.Fatalf("NIP tidak diambil dari employment: %+v", ids)
	}
}

// Person tanpa employment (uuid.Nil) sah: persona citizen & person yang belum berkepegawaian
// tak punya NIP. Yang TIDAK boleh terjadi: mencoba membaca employment nil lalu gagal.
func TestRepoCloneSource_EmploymentNilMenghasilkanNIPKosong(t *testing.T) {
	src, person, _ := seedSource(t)

	ids, err := src.Identifiers(context.Background(), person.ID, uuid.Nil)
	if err != nil {
		t.Fatalf("Identifiers: %v", err)
	}
	if ids.NIP != "" {
		t.Fatalf("NIP harus kosong tanpa employment, dapat %q", ids.NIP)
	}
	if ids.NIK != person.NIK {
		t.Fatalf("pengenal person tetap harus terisi: %+v", ids)
	}
}

// Person/employment tak ditemukan HARUS error — bukan Identifiers kosong. Clone berpengenal
// kosong tak bisa dibedakan dari non-ASN, dan ia melumpuhkan ResolveByNIK tanpa gejala.
func TestRepoCloneSource_TakDitemukanMenghasilkanError(t *testing.T) {
	src, person, emp := seedSource(t)

	if _, err := src.Identifiers(context.Background(), uuid.New(), emp.ID); err == nil {
		t.Fatal("person tak ditemukan harus error")
	}
	if _, err := src.Identifiers(context.Background(), person.ID, uuid.New()); err == nil {
		t.Fatal("employment tak ditemukan harus error")
	}
}

// Pesan error tak boleh mengutip pengenal: ia mengalir ke log — jalur samping yang sama
// dengan yang sedang ditutup (ADR-009 §6, cermin ErrNotFound di infra/user).
func TestRepoCloneSource_ErrorTakMengutipPengenal(t *testing.T) {
	person := &domain.Person{
		ID: uuid.New(), NIK: "3578010101900001", NamaLengkap: "Budi", IsActive: true,
		Email: "budi@example.test", NoHP: "0812340001",
	}
	src, err := sync.NewRepoCloneSource(
		&repoPersons{err: errors.New("dekripsi nik gagal")},
		&repoEmployments{byID: map[uuid.UUID]*domain.Employment{}},
	)
	if err != nil {
		t.Fatalf("NewRepoCloneSource: %v", err)
	}

	_, gotErr := src.Identifiers(context.Background(), person.ID, uuid.Nil)
	if gotErr == nil {
		t.Fatal("error repo harus diteruskan")
	}
	for _, nilai := range []string{person.NIK, person.Email, person.NoHP} {
		if strings.Contains(gotErr.Error(), nilai) {
			t.Fatalf("pesan error memuat pengenal: %v", gotErr)
		}
	}
}

func TestRepoCloneSource_RepoNilDitolak(t *testing.T) {
	if _, err := sync.NewRepoCloneSource(nil, &repoEmployments{}); err == nil {
		t.Fatal("PersonRepository nil harus ditolak")
	}
	if _, err := sync.NewRepoCloneSource(&repoPersons{}, nil); err == nil {
		t.Fatal("EmploymentRepository nil harus ditolak")
	}
}
