package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/identity/usecase"
	"github.com/huda-salam/pamong/testkit"
)

// --- Fake repos in-memory untuk unit test (tanpa DB) ---

type fakePersons struct {
	byID  map[uuid.UUID]*domain.Person
	byNIK map[string]*domain.Person
}

func newFakePersons() *fakePersons {
	return &fakePersons{byID: map[uuid.UUID]*domain.Person{}, byNIK: map[string]*domain.Person{}}
}

func (f *fakePersons) Save(_ context.Context, p *domain.Person) error {
	if _, dup := f.byNIK[p.NIK]; dup {
		return core.ErrConflict("NIK sudah terdaftar: " + p.NIK)
	}
	f.byID[p.ID] = p
	f.byNIK[p.NIK] = p
	return nil
}
func (f *fakePersons) FindByID(_ context.Context, id uuid.UUID) (*domain.Person, error) {
	if p, ok := f.byID[id]; ok {
		return p, nil
	}
	return nil, core.ErrNotFound("Person", id.String())
}
func (f *fakePersons) FindByNIK(_ context.Context, nik string) (*domain.Person, error) {
	if p, ok := f.byNIK[nik]; ok {
		return p, nil
	}
	return nil, core.ErrNotFound("Person", nik)
}

type fakeEmployments struct {
	byNIP map[string]*domain.Employment
}

func newFakeEmployments() *fakeEmployments {
	return &fakeEmployments{byNIP: map[string]*domain.Employment{}}
}

func (f *fakeEmployments) Save(_ context.Context, e *domain.Employment) error {
	if e.NIP != "" {
		f.byNIP[e.NIP] = e
	}
	return nil
}
func (f *fakeEmployments) FindByID(context.Context, uuid.UUID) (*domain.Employment, error) {
	return nil, core.ErrNotFound("Employment", "")
}
func (f *fakeEmployments) FindByNIP(_ context.Context, nip string) (*domain.Employment, error) {
	if e, ok := f.byNIP[nip]; ok {
		return e, nil
	}
	return nil, core.ErrNotFound("Employment", nip)
}
func (f *fakeEmployments) ListByPerson(context.Context, uuid.UUID) ([]*domain.Employment, error) {
	return nil, nil
}

// --- CreatePerson ---

func TestCreatePerson_Success(t *testing.T) {
	persons := newFakePersons()
	pub := testkit.NewMockPublisher()
	uc := usecase.NewCreatePerson(persons, pub)
	ctx := testkit.Ctx(t, testkit.WithPermission(domain.PermPersonBuat))

	p, err := uc.Execute(ctx, usecase.CreatePersonInput{NIK: "3578010101900001", NamaLengkap: "Budi"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if p.ID == uuid.Nil || !p.IsActive {
		t.Fatalf("person tidak lengkap: %+v", p)
	}
	if _, err := persons.FindByNIK(ctx, "3578010101900001"); err != nil {
		t.Fatalf("person tidak tersimpan: %v", err)
	}
	// Sukses harus menerbitkan event untuk sync clone.
	testkit.AssertEventPublished(t, pub, domain.EventPersonDibuat)
}

func TestCreatePerson_PermissionDenied(t *testing.T) {
	uc := usecase.NewCreatePerson(newFakePersons(), testkit.NewMockPublisher())
	ctx := testkit.Ctx(t) // tanpa permission
	_, err := uc.Execute(ctx, usecase.CreatePersonInput{NIK: "3578010101900001", NamaLengkap: "Budi"})
	var fe *core.FrameworkError
	if !errors.As(err, &fe) || fe.Code != "PERMISSION_DENIED" {
		t.Fatalf("harus PERMISSION_DENIED, dapat: %v", err)
	}
}

func TestCreatePerson_NIKInvalid(t *testing.T) {
	uc := usecase.NewCreatePerson(newFakePersons(), testkit.NewMockPublisher())
	ctx := testkit.Ctx(t, testkit.WithPermission(domain.PermPersonBuat))
	_, err := uc.Execute(ctx, usecase.CreatePersonInput{NIK: "123", NamaLengkap: "Budi"})
	if !errors.Is(err, domain.ErrNIKInvalid) {
		t.Fatalf("harus ErrNIKInvalid, dapat: %v", err)
	}
}

// --- AttachEmployment ---

func TestAttachEmployment_ASN_Success(t *testing.T) {
	persons := newFakePersons()
	emps := newFakeEmployments()
	person := &domain.Person{ID: uuid.New(), NIK: "3578010101900001", NamaLengkap: "Budi", IsActive: true}
	_ = persons.Save(context.Background(), person)

	pub := testkit.NewMockPublisher()
	uc := usecase.NewAttachEmployment(persons, emps, pub)
	ctx := testkit.Ctx(t, testkit.WithPermission(domain.PermEmploymentLampir))

	e, err := uc.Execute(ctx, usecase.AttachEmploymentInput{
		PersonID: person.ID, Status: domain.StatusASN, NIP: "199001012015011001",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if e.ValidFrom.IsZero() {
		t.Fatal("valid_from harus terisi default now()")
	}
	testkit.AssertEventPublished(t, pub, domain.EventEmploymentDibuat)
}

func TestAttachEmployment_ASN_TanpaNIP_Ditolak(t *testing.T) {
	persons := newFakePersons()
	person := &domain.Person{ID: uuid.New(), NIK: "3578010101900001", NamaLengkap: "Budi", IsActive: true}
	_ = persons.Save(context.Background(), person)

	uc := usecase.NewAttachEmployment(persons, newFakeEmployments(), testkit.NewMockPublisher())
	ctx := testkit.Ctx(t, testkit.WithPermission(domain.PermEmploymentLampir))

	_, err := uc.Execute(ctx, usecase.AttachEmploymentInput{PersonID: person.ID, Status: domain.StatusASN})
	if !errors.Is(err, domain.ErrNIPWajibASN) {
		t.Fatalf("ASN tanpa NIP harus ErrNIPWajibASN, dapat: %v", err)
	}
}

func TestAttachEmployment_PersonTidakAda(t *testing.T) {
	uc := usecase.NewAttachEmployment(newFakePersons(), newFakeEmployments(), testkit.NewMockPublisher())
	ctx := testkit.Ctx(t, testkit.WithPermission(domain.PermEmploymentLampir))
	_, err := uc.Execute(ctx, usecase.AttachEmploymentInput{
		PersonID: uuid.New(), Status: domain.StatusNonASN,
	})
	var fe *core.FrameworkError
	if !errors.As(err, &fe) || fe.Code != "NOT_FOUND" {
		t.Fatalf("person tak ada harus NOT_FOUND, dapat: %v", err)
	}
}

// --- Resolver ---

func TestResolver_ByNIK_dan_ByNIP(t *testing.T) {
	persons := newFakePersons()
	emps := newFakeEmployments()
	person := &domain.Person{ID: uuid.New(), NIK: "3578010101900001", NamaLengkap: "Budi", IsActive: true}
	_ = persons.Save(context.Background(), person)
	_ = emps.Save(context.Background(), &domain.Employment{
		ID: uuid.New(), PersonID: person.ID, Status: domain.StatusASN, NIP: "199001012015011001",
	})

	r := usecase.NewResolver(persons, emps)
	ctx := context.Background()

	byNIK, err := r.ByNIK(ctx, "3578010101900001")
	if err != nil || byNIK.ID != person.ID {
		t.Fatalf("ByNIK salah: %v / %+v", err, byNIK)
	}
	byNIP, err := r.ByNIP(ctx, "199001012015011001")
	if err != nil || byNIP.ID != person.ID {
		t.Fatalf("ByNIP harus resolve ke person yang sama: %v / %+v", err, byNIP)
	}
}

func TestResolver_ByNIP_NotFound(t *testing.T) {
	r := usecase.NewResolver(newFakePersons(), newFakeEmployments())
	_, err := r.ByNIP(context.Background(), "199001012015011001")
	var fe *core.FrameworkError
	if !errors.As(err, &fe) || fe.Code != "NOT_FOUND" {
		t.Fatalf("NIP tak ada harus NOT_FOUND, dapat: %v", err)
	}
}
