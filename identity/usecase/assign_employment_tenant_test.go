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

// --- Fakes khusus assign (employment FindByID + assignment store) ---

type storeEmployments struct {
	byID map[uuid.UUID]*domain.Employment
}

func newStoreEmployments() *storeEmployments {
	return &storeEmployments{byID: map[uuid.UUID]*domain.Employment{}}
}
func (f *storeEmployments) Save(_ context.Context, e *domain.Employment) error {
	f.byID[e.ID] = e
	return nil
}
func (f *storeEmployments) FindByID(_ context.Context, id uuid.UUID) (*domain.Employment, error) {
	if e, ok := f.byID[id]; ok {
		return e, nil
	}
	return nil, core.ErrNotFound("Employment", id.String())
}
func (f *storeEmployments) FindByNIP(context.Context, string) (*domain.Employment, error) {
	return nil, core.ErrNotFound("Employment", "")
}
func (f *storeEmployments) ListByPerson(context.Context, uuid.UUID) ([]*domain.Employment, error) {
	return nil, nil
}

type fakeAssignments struct{ saved []*domain.TenantAssignment }

func (f *fakeAssignments) Save(_ context.Context, a *domain.TenantAssignment) error {
	f.saved = append(f.saved, a)
	return nil
}
func (f *fakeAssignments) ListByEmployment(_ context.Context, employmentID uuid.UUID) ([]*domain.TenantAssignment, error) {
	var out []*domain.TenantAssignment
	for _, a := range f.saved {
		if a.EmploymentID == employmentID {
			out = append(out, a)
		}
	}
	return out, nil
}

// seedPersonEmployment menyiapkan satu person ASN + employment-nya pada fakes.
func seedPersonEmployment(t *testing.T) (*fakePersons, *storeEmployments, *domain.Person, *domain.Employment) {
	t.Helper()
	persons := newFakePersons()
	emps := newStoreEmployments()
	person := &domain.Person{ID: uuid.New(), NIK: "3578010101900001", NamaLengkap: "Budi", IsActive: true}
	_ = persons.Save(context.Background(), person)
	emp := &domain.Employment{
		ID: uuid.New(), PersonID: person.ID, Status: domain.StatusASN, NIP: "199001012015011001", IsActive: true,
	}
	_ = emps.Save(context.Background(), emp)
	return persons, emps, person, emp
}

func TestAssignEmploymentToTenant_Success(t *testing.T) {
	persons, emps, person, emp := seedPersonEmployment(t)
	assignments := &fakeAssignments{}
	pub := testkit.NewMockPublisher()
	uc := usecase.NewAssignEmploymentToTenant(persons, emps, assignments, pub)
	ctx := testkit.Ctx(t, testkit.WithPermission(domain.PermAssignmentTugaskan))

	a, err := uc.Execute(ctx, usecase.AssignEmploymentToTenantInput{
		EmploymentID: emp.ID, TenantID: "pemkot-surabaya",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !a.IsHomeTenant {
		t.Fatal("default penugasan harus home tenant")
	}
	if len(assignments.saved) != 1 {
		t.Fatalf("assignment harus tersimpan, dapat %d", len(assignments.saved))
	}

	// Event ditugaskan terbit dengan payload fat lengkap (pemicu clone).
	testkit.AssertEventPublished(t, pub, domain.EventEmploymentDitugaskan)
	ev := pub.Published()[0]
	payload, ok := ev.Payload.(domain.EmploymentDitugaskanPayload)
	if !ok {
		t.Fatalf("payload bertipe %T", ev.Payload)
	}
	if payload.PersonID != person.ID || payload.NIK != person.NIK ||
		payload.NIP != emp.NIP || payload.TenantID != "pemkot-surabaya" || payload.IsCrossTenant {
		t.Fatalf("payload tidak sesuai: %+v", payload)
	}
	if ev.TenantID != "pemkot-surabaya" {
		t.Fatalf("event tenant_id salah: %q", ev.TenantID)
	}
}

func TestAssignEmploymentToTenant_PermissionDenied(t *testing.T) {
	persons, emps, _, emp := seedPersonEmployment(t)
	uc := usecase.NewAssignEmploymentToTenant(persons, emps, &fakeAssignments{}, testkit.NewMockPublisher())
	ctx := testkit.Ctx(t) // tanpa permission
	_, err := uc.Execute(ctx, usecase.AssignEmploymentToTenantInput{EmploymentID: emp.ID, TenantID: "pemkot-surabaya"})
	if !testkit.IsPermissionDenied(err) {
		t.Fatalf("harus PERMISSION_DENIED, dapat: %v", err)
	}
}

func TestAssignEmploymentToTenant_CrossTenant_ButuhPermissionEkstra(t *testing.T) {
	persons, emps, _, emp := seedPersonEmployment(t)
	pub := testkit.NewMockPublisher()
	uc := usecase.NewAssignEmploymentToTenant(persons, emps, &fakeAssignments{}, pub)
	// Punya permission tugaskan dasar TAPI bukan cross_tenant.
	ctx := testkit.Ctx(t, testkit.WithPermission(domain.PermAssignmentTugaskan))

	_, err := uc.Execute(ctx, usecase.AssignEmploymentToTenantInput{
		EmploymentID: emp.ID, TenantID: "pemprov-jatim", CrossTenant: true,
	})
	var fe *core.FrameworkError
	if !errors.As(err, &fe) || fe.Code != "PERMISSION_DENIED" {
		t.Fatalf("cross-tenant tanpa permission khusus harus PERMISSION_DENIED, dapat: %v", err)
	}
	if len(pub.Published()) != 0 {
		t.Fatal("event tidak boleh terbit saat otorisasi gagal")
	}
}

func TestAssignEmploymentToTenant_EmploymentTidakAda(t *testing.T) {
	uc := usecase.NewAssignEmploymentToTenant(newFakePersons(), newStoreEmployments(), &fakeAssignments{}, testkit.NewMockPublisher())
	ctx := testkit.Ctx(t, testkit.WithPermission(domain.PermAssignmentTugaskan))
	_, err := uc.Execute(ctx, usecase.AssignEmploymentToTenantInput{EmploymentID: uuid.New(), TenantID: "pemkot-surabaya"})
	var fe *core.FrameworkError
	if !errors.As(err, &fe) || fe.Code != "NOT_FOUND" {
		t.Fatalf("employment tak ada harus NOT_FOUND, dapat: %v", err)
	}
}
