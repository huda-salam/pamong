package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/identity/usecase"
	"github.com/huda-salam/pamong/port"
)

// --- Fakes khusus alur login (fakePersons & fakeEmployments dipakai test lain) ---

type fakeCreds struct {
	byTypeValue map[string]*domain.Credential
}

func newFakeCreds() *fakeCreds { return &fakeCreds{byTypeValue: map[string]*domain.Credential{}} }

func credKey(t domain.CredType, v string) string { return string(t) + "|" + v }

func (f *fakeCreds) add(c *domain.Credential) { f.byTypeValue[credKey(c.CredType, c.CredValue)] = c }

func (f *fakeCreds) Save(_ context.Context, c *domain.Credential) error { f.add(c); return nil }
func (f *fakeCreds) FindByTypeValue(_ context.Context, t domain.CredType, v string) (*domain.Credential, error) {
	if c, ok := f.byTypeValue[credKey(t, v)]; ok {
		return c, nil
	}
	return nil, core.ErrNotFound("Credential", string(t)+"/"+v)
}
func (f *fakeCreds) ListByPerson(context.Context, uuid.UUID) ([]*domain.Credential, error) {
	return nil, nil
}

// fakeEmpRepo mendukung ListByPerson (berbeda dari fakeEmployments di usecase_test.go).
type fakeEmpRepo struct {
	byPerson map[uuid.UUID][]*domain.Employment
}

func newFakeEmpRepo() *fakeEmpRepo {
	return &fakeEmpRepo{byPerson: map[uuid.UUID][]*domain.Employment{}}
}
func (f *fakeEmpRepo) add(e *domain.Employment) {
	f.byPerson[e.PersonID] = append(f.byPerson[e.PersonID], e)
}
func (f *fakeEmpRepo) Save(_ context.Context, e *domain.Employment) error { f.add(e); return nil }
func (f *fakeEmpRepo) FindByID(context.Context, uuid.UUID) (*domain.Employment, error) {
	return nil, core.ErrNotFound("Employment", "")
}
func (f *fakeEmpRepo) FindByNIP(context.Context, string) (*domain.Employment, error) {
	return nil, core.ErrNotFound("Employment", "")
}
func (f *fakeEmpRepo) ListByPerson(_ context.Context, personID uuid.UUID) ([]*domain.Employment, error) {
	return f.byPerson[personID], nil
}

type fakeAssigns struct {
	byEmployment map[uuid.UUID][]*domain.TenantAssignment
}

func newFakeAssigns() *fakeAssigns {
	return &fakeAssigns{byEmployment: map[uuid.UUID][]*domain.TenantAssignment{}}
}
func (f *fakeAssigns) add(a *domain.TenantAssignment) {
	f.byEmployment[a.EmploymentID] = append(f.byEmployment[a.EmploymentID], a)
}
func (f *fakeAssigns) Save(_ context.Context, a *domain.TenantAssignment) error { f.add(a); return nil }
func (f *fakeAssigns) ListByEmployment(_ context.Context, employmentID uuid.UUID) ([]*domain.TenantAssignment, error) {
	return f.byEmployment[employmentID], nil
}

type fakeTenantRegistry struct {
	byID map[string]*domain.Tenant
}

func newFakeTenantRegistry() *fakeTenantRegistry {
	return &fakeTenantRegistry{byID: map[string]*domain.Tenant{}}
}
func (f *fakeTenantRegistry) add(t *domain.Tenant) { f.byID[t.TenantID] = t }
func (f *fakeTenantRegistry) Save(_ context.Context, t *domain.Tenant) error {
	f.add(t)
	return nil
}
func (f *fakeTenantRegistry) FindByID(_ context.Context, tenantID string) (*domain.Tenant, error) {
	if t, ok := f.byID[tenantID]; ok {
		return t, nil
	}
	return nil, core.ErrNotFound("Tenant", tenantID)
}
func (f *fakeTenantRegistry) List(context.Context) ([]*domain.Tenant, error) { return nil, nil }
func (f *fakeTenantRegistry) SetActive(_ context.Context, tenantID string, active bool) error {
	if t, ok := f.byID[tenantID]; ok {
		t.IsActive = active
	}
	return nil
}

// fakePasswords: Hash = "h:"+plain; Verify cocok bila hash == "h:"+plain.
type fakePasswords struct{}

func (fakePasswords) Hash(plain string) (string, error) { return "h:" + plain, nil }
func (fakePasswords) Verify(hash, plain string) error {
	if hash == "h:"+plain {
		return nil
	}
	return errors.New("tidak cocok")
}

// fakeCentralResolver mengembalikan role sentral PER (person, tenant) — meniru penyaringan scope
// di resolver nyata. Inilah yang membuat invariant teruji: use case membakar persis hasil resolver
// untuk tenant yang dipilih.
type fakeCentralResolver struct {
	roles map[string][]string // key: personID|tenantID
}

func newFakeCentralResolver() *fakeCentralResolver {
	return &fakeCentralResolver{roles: map[string][]string{}}
}
func (f *fakeCentralResolver) set(personID uuid.UUID, tenantID string, roles ...string) {
	f.roles[personID.String()+"|"+tenantID] = roles
}
func (f *fakeCentralResolver) EffectiveRoles(_ context.Context, personID uuid.UUID, tenantID string) ([]string, error) {
	return f.roles[personID.String()+"|"+tenantID], nil
}

type fakeTenantRoleResolver struct {
	roles map[string][]string // key: personID|tenantID
}

func newFakeTenantRoleResolver() *fakeTenantRoleResolver {
	return &fakeTenantRoleResolver{roles: map[string][]string{}}
}
func (f *fakeTenantRoleResolver) set(personID uuid.UUID, tenantID string, roles ...string) {
	f.roles[personID.String()+"|"+tenantID] = roles
}
func (f *fakeTenantRoleResolver) EffectiveRoles(_ context.Context, personID uuid.UUID, tenantID string) ([]string, error) {
	return f.roles[personID.String()+"|"+tenantID], nil
}

// fakeIssuer merekam Claims yang diterbitkan agar test bisa memeriksa apa yang dibakar ke token.
type fakeIssuer struct {
	issued []port.Claims
}

func (f *fakeIssuer) Issue(_ context.Context, c port.Claims) (string, error) {
	f.issued = append(f.issued, c)
	return "token-" + c.Persona + "-" + c.TenantID, nil
}
func (f *fakeIssuer) last() port.Claims { return f.issued[len(f.issued)-1] }

// stubAuthCtx adalah AuthContext minimal untuk menguji SelectTenant (TestContext selalu
// persona=employee; di sini kita perlu mengontrol persona & personID).
type stubAuthCtx struct {
	context.Context
	persona  string
	personID uuid.UUID
}

func (c stubAuthCtx) PersonID() uuid.UUID                             { return c.personID }
func (c stubAuthCtx) Persona() string                                 { return c.persona }
func (c stubAuthCtx) EmploymentStatus() string                        { return "" }
func (c stubAuthCtx) TenantID() string                                { return "" }
func (c stubAuthCtx) HasRole(string) bool                             { return false }
func (c stubAuthCtx) HasCentralRole(string) bool                      { return false }
func (c stubAuthCtx) RequirePermission(string) error                  { return nil }
func (c stubAuthCtx) RequirePermissionInUnit(string, uuid.UUID) error { return nil }
func (c stubAuthCtx) IsCitizen() bool                                 { return c.persona == domain.PersonaCitizen }
func (c stubAuthCtx) IsCrossTenant() bool                             { return false }

// --- Helper perakitan ---

type loginFixture struct {
	creds       *fakeCreds
	persons     *fakePersons
	emps        *fakeEmpRepo
	assigns     *fakeAssigns
	tenants     *fakeTenantRegistry
	central     *fakeCentralResolver
	tenantRoles *fakeTenantRoleResolver
	issuer      *fakeIssuer
}

func newLoginFixture() *loginFixture {
	return &loginFixture{
		creds:       newFakeCreds(),
		persons:     newFakePersons(),
		emps:        newFakeEmpRepo(),
		assigns:     newFakeAssigns(),
		tenants:     newFakeTenantRegistry(),
		central:     newFakeCentralResolver(),
		tenantRoles: newFakeTenantRoleResolver(),
		issuer:      &fakeIssuer{},
	}
}

func (fx *loginFixture) loginEmployee() *usecase.LoginEmployee {
	return usecase.NewLoginEmployee(fx.creds, fx.persons, fx.emps, fx.assigns, fx.tenants,
		fakePasswords{}, fx.central, fx.tenantRoles, fx.issuer)
}

func (fx *loginFixture) selectTenant() *usecase.SelectTenant {
	return usecase.NewSelectTenant(fx.emps, fx.assigns, fx.tenants, fx.central, fx.tenantRoles, fx.issuer)
}

func (fx *loginFixture) loginCitizen() *usecase.LoginCitizen {
	return usecase.NewLoginCitizen(fx.creds, fx.persons, fakePasswords{}, fx.issuer)
}

// seedEmployee membuat person aktif + employment ASN aktif + credential NIP berpassword.
func (fx *loginFixture) seedEmployee(t *testing.T) (person *domain.Person, emp *domain.Employment) {
	t.Helper()
	person = &domain.Person{ID: uuid.New(), NIK: "3578010101900001", NamaLengkap: "Budi", IsActive: true}
	_ = fx.persons.Save(context.Background(), person)
	emp = &domain.Employment{
		ID: uuid.New(), PersonID: person.ID, Status: domain.StatusASN,
		NIP: "199001012015011001", IsActive: true, ValidFrom: time.Now().Add(-24 * time.Hour),
	}
	fx.emps.add(emp)
	fx.creds.add(&domain.Credential{
		ID: uuid.New(), PersonID: person.ID, CredType: domain.CredNIP,
		CredValue: emp.NIP, SecretHash: "h:rahasia",
	})
	return person, emp
}

func (fx *loginFixture) assignTenant(emp *domain.Employment, tenantID string, home, active bool) {
	fx.tenants.add(&domain.Tenant{TenantID: tenantID, Nama: tenantID, Tier: domain.TierShared,
		DBHost: "h", DBName: tenantID, IsActive: active})
	fx.assigns.add(&domain.TenantAssignment{
		ID: uuid.New(), EmploymentID: emp.ID, TenantID: tenantID, IsHomeTenant: home,
		AssignedBy: uuid.New(), ValidFrom: time.Now().Add(-24 * time.Hour),
	})
}

func assertUnauthorized(t *testing.T, err error) {
	t.Helper()
	var fe *core.FrameworkError
	if !errors.As(err, &fe) || fe.Code != "UNAUTHORIZED" {
		t.Fatalf("harus UNAUTHORIZED, dapat: %v", err)
	}
}

// --- LoginEmployee ---

func TestLoginEmployee_SingleTenant_Success(t *testing.T) {
	fx := newLoginFixture()
	person, emp := fx.seedEmployee(t)
	fx.assignTenant(emp, "pemkot-surabaya", true, true)
	fx.central.set(person.ID, "pemkot-surabaya", "platform_helpdesk")
	fx.tenantRoles.set(person.ID, "pemkot-surabaya", "operator_surat")

	res, err := fx.loginEmployee().Execute(context.Background(), usecase.LoginEmployeeInput{
		CredType: domain.CredNIP, CredValue: emp.NIP, Password: "rahasia",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.NeedTenantSelection {
		t.Fatal("tenant tunggal tak boleh butuh pemilihan")
	}
	if res.Token == "" {
		t.Fatal("token kosong")
	}
	c := fx.issuer.last()
	if c.Persona != domain.PersonaEmployee || c.TenantID != "pemkot-surabaya" {
		t.Fatalf("klaim salah: %+v", c)
	}
	if c.EmploymentStatus != "asn" {
		t.Fatalf("employment_status harus asn: %+v", c)
	}
	if len(c.CentralRoles) != 1 || c.CentralRoles[0] != "platform_helpdesk" {
		t.Fatalf("central roles salah: %+v", c.CentralRoles)
	}
	if len(c.TenantRoles) != 1 || c.TenantRoles[0] != "operator_surat" {
		t.Fatalf("tenant roles salah: %+v", c.TenantRoles)
	}
}

func TestLoginEmployee_MultiTenant_NeedsSelection_ThenSelect(t *testing.T) {
	fx := newLoginFixture()
	person, emp := fx.seedEmployee(t)
	fx.assignTenant(emp, "pemkot-surabaya", true, true)
	fx.assignTenant(emp, "pemkot-malang", false, true)
	fx.central.set(person.ID, "pemkot-malang", "regional_helpdesk")

	res, err := fx.loginEmployee().Execute(context.Background(), usecase.LoginEmployeeInput{
		CredType: domain.CredNIP, CredValue: emp.NIP, Password: "rahasia",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !res.NeedTenantSelection || len(res.Tenants) != 2 {
		t.Fatalf("harus butuh pemilihan dari 2 tenant: %+v", res)
	}
	// Token sementara: persona employee, tanpa tenant & tanpa role.
	temp := fx.issuer.last()
	if temp.TenantID != "" || len(temp.CentralRoles) != 0 || len(temp.TenantRoles) != 0 {
		t.Fatalf("token sementara tak boleh bawa tenant/role: %+v", temp)
	}

	// Pilih pemkot-malang lewat token sementara (persona employee).
	ctx := stubAuthCtx{Context: context.Background(), persona: domain.PersonaEmployee, personID: person.ID}
	token, err := fx.selectTenant().Execute(ctx, "pemkot-malang")
	if err != nil {
		t.Fatalf("SelectTenant: %v", err)
	}
	if token == "" {
		t.Fatal("token final kosong")
	}
	c := fx.issuer.last()
	if c.TenantID != "pemkot-malang" || !c.IsCrossTenant {
		t.Fatalf("token final salah (cross-tenant non-home): %+v", c)
	}
	if len(c.CentralRoles) != 1 || c.CentralRoles[0] != "regional_helpdesk" {
		t.Fatalf("central role tenant malang salah: %+v", c.CentralRoles)
	}
}

// INVARIANT: role sentral scoped tenant lain TIDAK ikut saat login ke tenant berbeda. Resolver
// hanya mengembalikan role yang berlaku untuk (person, tenant) → use case membakar persis itu.
func TestLoginEmployee_ScopeFiltered_NoCrossTenantRoleLeak(t *testing.T) {
	fx := newLoginFixture()
	person, emp := fx.seedEmployee(t)
	fx.assignTenant(emp, "pemkot-surabaya", true, true)
	// Role hanya berlaku di tenant LAIN (malang), bukan surabaya yang sedang dimasuki.
	fx.central.set(person.ID, "pemkot-malang", "regional_helpdesk")

	if _, err := fx.loginEmployee().Execute(context.Background(), usecase.LoginEmployeeInput{
		CredType: domain.CredNIP, CredValue: emp.NIP, Password: "rahasia",
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	c := fx.issuer.last()
	if c.TenantID != "pemkot-surabaya" {
		t.Fatalf("tenant salah: %+v", c)
	}
	if len(c.CentralRoles) != 0 {
		t.Fatalf("role scoped tenant lain bocor ke token: %+v", c.CentralRoles)
	}
}

func TestLoginEmployee_WrongPassword(t *testing.T) {
	fx := newLoginFixture()
	_, emp := fx.seedEmployee(t)
	fx.assignTenant(emp, "pemkot-surabaya", true, true)
	_, err := fx.loginEmployee().Execute(context.Background(), usecase.LoginEmployeeInput{
		CredType: domain.CredNIP, CredValue: emp.NIP, Password: "salah",
	})
	assertUnauthorized(t, err)
}

func TestLoginEmployee_NoActiveEmployment(t *testing.T) {
	fx := newLoginFixture()
	person := &domain.Person{ID: uuid.New(), NIK: "3578010101900002", NamaLengkap: "Sari", IsActive: true}
	_ = fx.persons.Save(context.Background(), person)
	// Employment ada tapi NON-AKTIF.
	emp := &domain.Employment{ID: uuid.New(), PersonID: person.ID, Status: domain.StatusASN,
		NIP: "199001012015011002", IsActive: false, ValidFrom: time.Now().Add(-24 * time.Hour)}
	fx.emps.add(emp)
	fx.creds.add(&domain.Credential{ID: uuid.New(), PersonID: person.ID, CredType: domain.CredNIP,
		CredValue: emp.NIP, SecretHash: "h:rahasia"})

	_, err := fx.loginEmployee().Execute(context.Background(), usecase.LoginEmployeeInput{
		CredType: domain.CredNIP, CredValue: emp.NIP, Password: "rahasia",
	})
	assertUnauthorized(t, err)
}

func TestLoginEmployee_EmailCredRejected(t *testing.T) {
	fx := newLoginFixture()
	_, _ = fx.seedEmployee(t)
	_, err := fx.loginEmployee().Execute(context.Background(), usecase.LoginEmployeeInput{
		CredType: domain.CredEmail, CredValue: "budi@example.com", Password: "rahasia",
	})
	assertUnauthorized(t, err)
}

func TestLoginEmployee_InactiveTenantNotOffered(t *testing.T) {
	fx := newLoginFixture()
	_, emp := fx.seedEmployee(t)
	fx.assignTenant(emp, "pemkot-nonaktif", true, false) // tenant non-aktif
	_, err := fx.loginEmployee().Execute(context.Background(), usecase.LoginEmployeeInput{
		CredType: domain.CredNIP, CredValue: emp.NIP, Password: "rahasia",
	})
	// Pegawai aktif tapi tak ada tenant aktif → ditolak.
	assertUnauthorized(t, err)
}

// --- SelectTenant ---

func TestSelectTenant_NotAssignedRejected(t *testing.T) {
	fx := newLoginFixture()
	person, emp := fx.seedEmployee(t)
	fx.assignTenant(emp, "pemkot-surabaya", true, true)
	ctx := stubAuthCtx{Context: context.Background(), persona: domain.PersonaEmployee, personID: person.ID}
	_, err := fx.selectTenant().Execute(ctx, "pemkot-malang") // tak ditugaskan
	assertUnauthorized(t, err)
}

func TestSelectTenant_WrongPersonaRejected(t *testing.T) {
	fx := newLoginFixture()
	person, emp := fx.seedEmployee(t)
	fx.assignTenant(emp, "pemkot-surabaya", true, true)
	ctx := stubAuthCtx{Context: context.Background(), persona: domain.PersonaCitizen, personID: person.ID}
	_, err := fx.selectTenant().Execute(ctx, "pemkot-surabaya")
	assertUnauthorized(t, err)
}

// --- LoginCitizen ---

func TestLoginCitizen_Success_NoInternalRoles(t *testing.T) {
	fx := newLoginFixture()
	// Person ASN (punya employment + role sentral) — login PUBLIK harus tetap tanpa role internal.
	person, emp := fx.seedEmployee(t)
	fx.assignTenant(emp, "pemkot-surabaya", true, true)
	fx.central.set(person.ID, "pemkot-surabaya", "platform_helpdesk")
	// Credential publik via email.
	fx.creds.add(&domain.Credential{ID: uuid.New(), PersonID: person.ID, CredType: domain.CredEmail,
		CredValue: "budi@example.com", SecretHash: "h:publik"})

	token, err := fx.loginCitizen().Execute(context.Background(), usecase.LoginCitizenInput{
		CredType: domain.CredEmail, CredValue: "budi@example.com", Password: "publik",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if token == "" {
		t.Fatal("token kosong")
	}
	c := fx.issuer.last()
	if c.Persona != domain.PersonaCitizen {
		t.Fatalf("persona harus citizen: %+v", c)
	}
	if c.TenantID != "" || c.EmploymentStatus != "" {
		t.Fatalf("citizen tak boleh punya tenant/employment_status: %+v", c)
	}
	if len(c.CentralRoles) != 0 || len(c.TenantRoles) != 0 {
		t.Fatalf("ASN login publik bocor role internal: %+v", c)
	}
}

func TestLoginCitizen_NIPRejected(t *testing.T) {
	fx := newLoginFixture()
	_, emp := fx.seedEmployee(t)
	_, err := fx.loginCitizen().Execute(context.Background(), usecase.LoginCitizenInput{
		CredType: domain.CredNIP, CredValue: emp.NIP, Password: "rahasia",
	})
	assertUnauthorized(t, err)
}

func TestLoginCitizen_WrongPassword(t *testing.T) {
	fx := newLoginFixture()
	person, _ := fx.seedEmployee(t)
	fx.creds.add(&domain.Credential{ID: uuid.New(), PersonID: person.ID, CredType: domain.CredNIK,
		CredValue: person.NIK, SecretHash: "h:benar"})
	_, err := fx.loginCitizen().Execute(context.Background(), usecase.LoginCitizenInput{
		CredType: domain.CredNIK, CredValue: person.NIK, Password: "salah",
	})
	assertUnauthorized(t, err)
}
