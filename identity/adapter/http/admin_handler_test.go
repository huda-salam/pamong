package http_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/gateway"
	"github.com/huda-salam/pamong/identity/adapter/auth"
	identityhttp "github.com/huda-salam/pamong/identity/adapter/http"
	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/identity/usecase"
	"github.com/huda-salam/pamong/port"
	"github.com/huda-salam/pamong/testkit"
)

// Test lapis adapter untuk grup /admin/identity/* (PR-W2). Yang dijaga di sini: kontrak kawat
// (bentuk JSON, status code), gerbang perakitan, dan satu properti yang tak bisa diuji di
// lapis lain — bahwa permission diperiksa SEBELUM body di-parse (CLAUDE.md aturan #3).
//
// Properti terakhir itu butuh cara membedakannya dari pemeriksaan use case, yang memakai
// permission yang sama: tanpa itu, menghapus gerbang di handler tetap menghasilkan 403 (dari
// use case) dan testnya lulus atas kode yang sudah rusak. Pembedanya = BODY RUSAK. Bila
// gerbang berdiri lebih dulu jawabannya 403; bila ia dihapus, decoder yang menjawab lebih dulu
// dengan 400 — dan bocornya bukan kosmetik: pemanggil tak berizin jadi bisa membedakan body
// yang diterima parser dari yang ditolak, yaitu permukaan probing atas bentuk API internal.

// --- Fakes khusus admin (nama dibedakan dari fakes handler_test.go, satu paket) ---

type adminEmployments struct {
	byID  map[uuid.UUID]*domain.Employment
	saved []*domain.Employment
}

func newAdminEmployments() *adminEmployments {
	return &adminEmployments{byID: map[uuid.UUID]*domain.Employment{}}
}
func (f *adminEmployments) Save(_ context.Context, e *domain.Employment) error {
	f.byID[e.ID] = e
	f.saved = append(f.saved, e)
	return nil
}
func (f *adminEmployments) FindByID(_ context.Context, id uuid.UUID) (*domain.Employment, error) {
	if e, ok := f.byID[id]; ok {
		return e, nil
	}
	return nil, core.ErrNotFound("Employment", id.String())
}
func (f *adminEmployments) FindByNIP(context.Context, string) (*domain.Employment, error) {
	return nil, core.ErrNotFound("Employment", "nip")
}
func (f *adminEmployments) ListByPerson(context.Context, uuid.UUID) ([]*domain.Employment, error) {
	return nil, nil
}

type adminAssignments struct{ saved []*domain.TenantAssignment }

func (f *adminAssignments) Save(_ context.Context, a *domain.TenantAssignment) error {
	f.saved = append(f.saved, a)
	return nil
}
func (f *adminAssignments) ListByEmployment(_ context.Context, id uuid.UUID) ([]*domain.TenantAssignment, error) {
	var out []*domain.TenantAssignment
	for _, a := range f.saved {
		if a.EmploymentID == id {
			out = append(out, a)
		}
	}
	return out, nil
}

type adminRoles struct {
	byID map[uuid.UUID]*domain.CentralRole
}

func (f *adminRoles) Save(_ context.Context, r *domain.CentralRole) error {
	f.byID[r.ID] = r
	return nil
}
func (f *adminRoles) FindByID(_ context.Context, id uuid.UUID) (*domain.CentralRole, error) {
	if r, ok := f.byID[id]; ok {
		return r, nil
	}
	return nil, core.ErrNotFound("CentralRole", id.String())
}
func (f *adminRoles) FindByName(context.Context, string) (*domain.CentralRole, error) {
	return nil, core.ErrNotFound("CentralRole", "name")
}
func (f *adminRoles) List(context.Context) ([]*domain.CentralRole, error) { return nil, nil }

type adminRoleAssignments struct {
	saved []*domain.CentralRoleAssignment
}

func (f *adminRoleAssignments) Save(_ context.Context, a *domain.CentralRoleAssignment) error {
	f.saved = append(f.saved, a)
	return nil
}
func (f *adminRoleAssignments) ListByPerson(context.Context, uuid.UUID) ([]*domain.CentralRoleAssignment, error) {
	return nil, nil
}

type adminTenants struct{ byID map[string]*domain.Tenant }

func (f *adminTenants) Save(context.Context, *domain.Tenant) error { return nil }
func (f *adminTenants) FindByID(_ context.Context, id string) (*domain.Tenant, error) {
	if t, ok := f.byID[id]; ok {
		return t, nil
	}
	return nil, core.ErrNotFound("Tenant", id)
}
func (f *adminTenants) List(context.Context) ([]*domain.Tenant, error) { return nil, nil }
func (f *adminTenants) SetActive(context.Context, string, bool) error  { return nil }

// permEval memenuhi port.PermissionEvaluator dari sebuah himpunan permission.
type permEval map[string]bool

func (p permEval) Allows(_ []string, perm string) bool { return p[perm] }

// --- Fixture ---

type adminFixture struct {
	persons *fakePersons
	emps    *adminEmployments
	creds   *fakeCreds
	assigns *adminAssignments
	roles   *adminRoles
	roleAss *adminRoleAssignments
	tenants *adminTenants
	pub     *testkit.MockPublisher
	handler *identityhttp.AdminHandler
}

func newAdminFixture(t *testing.T) *adminFixture {
	t.Helper()
	fx := &adminFixture{
		persons: newFakePersons(),
		emps:    newAdminEmployments(),
		creds:   newFakeCreds(),
		assigns: &adminAssignments{},
		roles:   &adminRoles{byID: map[uuid.UUID]*domain.CentralRole{}},
		roleAss: &adminRoleAssignments{},
		tenants: &adminTenants{byID: map[string]*domain.Tenant{}},
		pub:     testkit.NewMockPublisher(),
	}
	fx.handler = identityhttp.NewAdminHandler(
		usecase.NewCreatePerson(fx.persons, fx.pub),
		usecase.NewAttachEmployment(fx.persons, fx.emps, fx.pub),
		usecase.NewCreateCredential(fx.persons, fx.creds, auth.NewBcryptVerifier(), usecase.NewVerifyGate(0, 0)),
		usecase.NewAssignEmploymentToTenant(fx.persons, fx.emps, fx.assigns, fx.tenants, fx.pub),
		usecase.NewAssignCentralRole(fx.roles, fx.roleAss),
	)
	return fx
}

// post menjalankan handler dengan gateway.Context yang membawa persis permission yang diberikan.
// Evaluator disuntik eksplisit: gateway.Context tanpa evaluator bersifat PERMISIF (default lama),
// jadi test permission-denied yang lupa memasangnya akan hijau tanpa menguji apa pun.
func (fx *adminFixture) post(t *testing.T, body string, h http.HandlerFunc, perms ...string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/admin/identity/x", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")

	set := permEval{}
	for _, p := range perms {
		set[p] = true
	}
	c := gateway.NewContextFromClaims(r.Context(), &port.Claims{
		PersonID: uuid.New(), Persona: "employee", TenantID: "pemkot-a",
		CentralRoles: []string{"platform_admin"},
	})
	c.SetPermissionEvaluator(set)

	w := httptest.NewRecorder()
	h(w, gateway.WithContext(r, c))
	return w
}

// seedPerson menaruh satu person aktif di fake repo dan mengembalikan id-nya.
func (fx *adminFixture) seedPerson(t *testing.T) uuid.UUID {
	t.Helper()
	p := &domain.Person{ID: uuid.New(), NIK: "3578010101900031", NamaLengkap: "Dewi", IsActive: true}
	if err := fx.persons.Save(context.Background(), p); err != nil {
		t.Fatalf("seed person: %v", err)
	}
	return p.ID
}

// seedEmployment menaruh person + employment ASN aktif, mengembalikan id employment.
func (fx *adminFixture) seedEmployment(t *testing.T) uuid.UUID {
	t.Helper()
	personID := fx.seedPerson(t)
	e := &domain.Employment{
		ID: uuid.New(), PersonID: personID, Status: domain.StatusASN,
		NIP: "199001012015011031", IsActive: true, ValidFrom: time.Now().Add(-time.Hour),
	}
	if err := fx.emps.Save(context.Background(), e); err != nil {
		t.Fatalf("seed employment: %v", err)
	}
	return e.ID
}

// --- Happy path ---

func TestAdminHandler_CreatePerson_201(t *testing.T) {
	fx := newAdminFixture(t)
	w := fx.post(t, `{"nik":"3578010101900041","nama_lengkap":"Andi"}`,
		fx.handler.CreatePerson, domain.PermPersonBuat)

	if w.Code != http.StatusCreated {
		t.Fatalf("mau 201, dapat %d — body: %s", w.Code, w.Body.String())
	}
	var out struct {
		ID uuid.UUID `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || out.ID == uuid.Nil {
		t.Fatalf("respons tak memuat id: %s (%v)", w.Body.String(), err)
	}
	// Pengenal tak dipantulkan kembali: badan respons mengalir ke log akses & cache idempotency.
	if strings.Contains(w.Body.String(), "3578010101900041") {
		t.Fatalf("respons memuat NIK: %s", w.Body.String())
	}
}

func TestAdminHandler_AttachEmployment_201(t *testing.T) {
	fx := newAdminFixture(t)
	personID := fx.seedPerson(t)

	w := fx.post(t, `{"person_id":"`+personID.String()+`","status":"asn","nip":"199001012015011041"}`,
		fx.handler.AttachEmployment, domain.PermEmploymentLampir)

	if w.Code != http.StatusCreated {
		t.Fatalf("mau 201, dapat %d — body: %s", w.Code, w.Body.String())
	}
	if len(fx.emps.saved) != 1 {
		t.Fatalf("employment harus tersimpan, dapat %d", len(fx.emps.saved))
	}
	if strings.Contains(w.Body.String(), "199001012015011041") {
		t.Fatalf("respons memuat NIP: %s", w.Body.String())
	}
}

func TestAdminHandler_CreateCredential_201(t *testing.T) {
	fx := newAdminFixture(t)
	personID := fx.seedPerson(t)

	w := fx.post(t, `{"person_id":"`+personID.String()+`","cred_type":"nip",`+
		`"cred_value":"199001012015011041","password":"kata-sandi-panjang","is_primary":true}`,
		fx.handler.CreateCredential, domain.PermCredentialBuat)

	if w.Code != http.StatusCreated {
		t.Fatalf("mau 201, dapat %d — body: %s", w.Code, w.Body.String())
	}
	// Respons tak boleh memuat password MAUPUN hash-nya.
	body := w.Body.String()
	if strings.Contains(body, "kata-sandi-panjang") || strings.Contains(body, "$2a$") ||
		strings.Contains(body, "199001012015011041") {
		t.Fatalf("respons membocorkan kredensial: %s", body)
	}
}

func TestAdminHandler_AssignEmploymentToTenant_201_MenerbitkanEventClone(t *testing.T) {
	fx := newAdminFixture(t)
	employmentID := fx.seedEmployment(t)
	fx.tenants.byID["pemkot-a"] = &domain.Tenant{
		TenantID: "pemkot-a", Nama: "Pemkot A", Tier: domain.TierShared,
		DBHost: "db", DBName: "gov_pemkot_a", IsActive: true,
	}

	w := fx.post(t, `{"employment_id":"`+employmentID.String()+`","tenant_id":"pemkot-a"}`,
		fx.handler.AssignEmploymentToTenant, domain.PermAssignmentTugaskan)

	if w.Code != http.StatusCreated {
		t.Fatalf("mau 201, dapat %d — body: %s", w.Code, w.Body.String())
	}
	// Inilah alasan endpoint ini ada: ia produsen event pemicu clone identity→tenant.
	testkit.AssertEventPublished(t, fx.pub, domain.EventEmploymentDitugaskan)
}

// Cross-tenant menuntut permission KEDUA, dan penegakannya ada di use case — handler hanya
// memeriksa permission dasar karena sifat cross-tenant baru diketahui setelah body di-parse.
func TestAdminHandler_AssignCrossTenant_TanpaPermissionEkstra_403(t *testing.T) {
	fx := newAdminFixture(t)
	employmentID := fx.seedEmployment(t)
	fx.tenants.byID["pemprov-jatim"] = &domain.Tenant{
		TenantID: "pemprov-jatim", Nama: "Pemprov", Tier: domain.TierShared,
		DBHost: "db", DBName: "gov_pemprov", IsActive: true,
	}

	w := fx.post(t,
		`{"employment_id":"`+employmentID.String()+`","tenant_id":"pemprov-jatim","cross_tenant":true}`,
		fx.handler.AssignEmploymentToTenant, domain.PermAssignmentTugaskan)

	if w.Code != http.StatusForbidden {
		t.Fatalf("cross-tenant tanpa identity:assignment:cross_tenant harus 403, dapat %d — body: %s",
			w.Code, w.Body.String())
	}
	if len(fx.assigns.saved) != 0 || len(fx.pub.Published()) != 0 {
		t.Fatal("otorisasi gagal tak boleh menyisakan assignment atau event")
	}
}

func TestAdminHandler_AssignCentralRole_201(t *testing.T) {
	fx := newAdminFixture(t)
	personID := fx.seedPerson(t)
	role := &domain.CentralRole{
		ID: uuid.New(), Name: "platform_helpdesk", Label: "Helpdesk", ScopeType: domain.ScopeGlobal,
	}
	fx.roles.byID[role.ID] = role

	w := fx.post(t, `{"person_id":"`+personID.String()+`","role_id":"`+role.ID.String()+`"}`,
		fx.handler.AssignCentralRole, domain.PermCentralRoleAssign)

	if w.Code != http.StatusCreated {
		t.Fatalf("mau 201, dapat %d — body: %s", w.Code, w.Body.String())
	}
	if len(fx.roleAss.saved) != 1 {
		t.Fatalf("assignment role sentral harus tersimpan, dapat %d", len(fx.roleAss.saved))
	}
}

// --- Permission denied untuk SETIAP endpoint ---

// endpoints memetakan tiap handler ke permission yang menggerbanginya. Tabel ini juga yang
// membuat endpoint baru tanpa test permission terlihat: menambah handler tanpa menambah baris
// di sini berarti ia tak pernah diuji ditolak.
func adminEndpoints(fx *adminFixture) []struct {
	nama    string
	handler http.HandlerFunc
	perm    string
	body    string
} {
	return []struct {
		nama    string
		handler http.HandlerFunc
		perm    string
		body    string
	}{
		{"persons", fx.handler.CreatePerson, domain.PermPersonBuat,
			`{"nik":"3578010101900041","nama_lengkap":"Andi"}`},
		{"employments", fx.handler.AttachEmployment, domain.PermEmploymentLampir,
			`{"person_id":"` + uuid.New().String() + `","status":"asn","nip":"199001012015011041"}`},
		{"credentials", fx.handler.CreateCredential, domain.PermCredentialBuat,
			`{"person_id":"` + uuid.New().String() + `","cred_type":"nip","cred_value":"199001012015011041"}`},
		{"assignments", fx.handler.AssignEmploymentToTenant, domain.PermAssignmentTugaskan,
			`{"employment_id":"` + uuid.New().String() + `","tenant_id":"pemkot-a"}`},
		{"central-role-assignments", fx.handler.AssignCentralRole, domain.PermCentralRoleAssign,
			`{"person_id":"` + uuid.New().String() + `","role_id":"` + uuid.New().String() + `"}`},
	}
}

// Ketiga test di bawah memakai SATU fixture: semua request di dalamnya ditolak sebelum
// menyentuh repo, jadi tak ada state yang bisa bocor antar sub-test.

func TestAdminHandler_TanpaPermission_403(t *testing.T) {
	fx := newAdminFixture(t)
	for _, ep := range adminEndpoints(fx) {
		t.Run(ep.nama, func(t *testing.T) {
			w := fx.post(t, ep.body, ep.handler) // tanpa permission apa pun
			if w.Code != http.StatusForbidden {
				t.Fatalf("mau 403, dapat %d — body: %s", w.Code, w.Body.String())
			}
		})
	}
}

// Permission SATU endpoint tidak boleh membuka endpoint lain. Tanpa test ini, gerbang yang
// keliru menyebut konstanta permission tetangga (copy-paste antar handler yang bentuknya
// nyaris identik) akan lolos seluruh test di atas.
func TestAdminHandler_PermissionEndpointLain_TidakMembuka(t *testing.T) {
	fx := newAdminFixture(t)
	eps := adminEndpoints(fx)
	for i, ep := range eps {
		t.Run(ep.nama, func(t *testing.T) {
			var lain []string
			for j, e := range eps {
				if i != j {
					lain = append(lain, e.perm)
				}
			}
			w := fx.post(t, ep.body, ep.handler, lain...) // semua permission KECUALI miliknya
			if w.Code != http.StatusForbidden {
				t.Fatalf("mau 403 (permission endpoint lain tak boleh membuka), dapat %d — body: %s",
					w.Code, w.Body.String())
			}
		})
	}
}

// Gerbang permission berdiri SEBELUM parse: body rusak + tanpa izin → 403, bukan 400.
//
// Inilah test yang mati bila `RequirePermission` di handler dihapus — pemeriksaan use case
// tak bisa menggantikannya, karena ia baru tercapai setelah decoder menjawab 400.
func TestAdminHandler_PermissionDiperiksaSebelumParse(t *testing.T) {
	fx := newAdminFixture(t)
	for _, ep := range adminEndpoints(fx) {
		t.Run(ep.nama, func(t *testing.T) {
			w := fx.post(t, `{bukan json`, ep.handler) // body rusak DAN tanpa permission
			if w.Code != http.StatusForbidden {
				t.Fatalf("mau 403 (gerbang sebelum parse), dapat %d — body: %s; "+
					"400 berarti decoder menjawab lebih dulu, yaitu gerbang handler hilang",
					w.Code, w.Body.String())
			}
		})
	}
}

// --- Validasi & bentuk kawat ---

func TestAdminHandler_BodyTakValid_400(t *testing.T) {
	fx := newAdminFixture(t)
	w := fx.post(t, `{bukan json`, fx.handler.CreatePerson, domain.PermPersonBuat)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("body rusak harus 400, dapat %d — body: %s", w.Code, w.Body.String())
	}
}

// Field wajib divalidasi DOMAIN dan muncul sebagai 422 (VALIDATION_ERROR), bukan 500 —
// artinya kegagalannya adalah masukan yang ditolak aturan, bukan kesalahan server.
func TestAdminHandler_ValidasiFieldWajib(t *testing.T) {
	kasus := []struct {
		nama    string
		perm    string
		siapkan func(*adminFixture) (http.HandlerFunc, string)
	}{
		{"nik tak valid", domain.PermPersonBuat, func(fx *adminFixture) (http.HandlerFunc, string) {
			return fx.handler.CreatePerson, `{"nik":"123","nama_lengkap":"Andi"}`
		}},
		{"nama kosong", domain.PermPersonBuat, func(fx *adminFixture) (http.HandlerFunc, string) {
			return fx.handler.CreatePerson, `{"nik":"3578010101900041","nama_lengkap":""}`
		}},
		{"nip kosong untuk ASN", domain.PermEmploymentLampir, func(fx *adminFixture) (http.HandlerFunc, string) {
			personID := fx.seedPerson(t)
			return fx.handler.AttachEmployment, `{"person_id":"` + personID.String() + `","status":"asn"}`
		}},
		{"cred_value kosong", domain.PermCredentialBuat, func(fx *adminFixture) (http.HandlerFunc, string) {
			personID := fx.seedPerson(t)
			return fx.handler.CreateCredential, `{"person_id":"` + personID.String() + `","cred_type":"nip"}`
		}},
		{"tenant tak terdaftar", domain.PermAssignmentTugaskan, func(fx *adminFixture) (http.HandlerFunc, string) {
			employmentID := fx.seedEmployment(t)
			return fx.handler.AssignEmploymentToTenant,
				`{"employment_id":"` + employmentID.String() + `","tenant_id":""}`
		}},
	}
	for _, k := range kasus {
		t.Run(k.nama, func(t *testing.T) {
			fx := newAdminFixture(t)
			h, body := k.siapkan(fx)
			w := fx.post(t, body, h, k.perm)
			if w.Code != http.StatusUnprocessableEntity {
				t.Fatalf("mau 422 VALIDATION_ERROR, dapat %d — body: %s", w.Code, w.Body.String())
			}
		})
	}
}

// UUID yang tak berbentuk gagal saat DECODE (400), tak pernah diam-diam menjadi uuid.Nil yang
// lalu dicari ke repo.
func TestAdminHandler_UUIDTakBerbentuk_400(t *testing.T) {
	fx := newAdminFixture(t)
	w := fx.post(t, `{"person_id":"bukan-uuid","status":"asn","nip":"199001012015011041"}`,
		fx.handler.AttachEmployment, domain.PermEmploymentLampir)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("person_id tak berbentuk UUID harus 400, dapat %d — body: %s", w.Code, w.Body.String())
	}
	if len(fx.emps.saved) != 0 {
		t.Fatal("tak ada yang boleh tersimpan saat decode gagal")
	}
}

// Konstruktor menolak use case nil saat PERAKITAN, bukan saat request pertama.
func TestNewAdminHandler_MenolakUseCaseNil(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("NewAdminHandler dengan use case nil harus panic saat perakitan")
		}
	}()
	identityhttp.NewAdminHandler(nil, nil, nil, nil, nil)
}
