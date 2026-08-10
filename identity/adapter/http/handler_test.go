package http_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/identity/adapter/auth"
	identityhttp "github.com/huda-salam/pamong/identity/adapter/http"
	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/identity/usecase"
	"github.com/huda-salam/pamong/port"
	"github.com/huda-salam/pamong/testkit"
)

// Test lapis adapter: yang dijaga di sini adalah KONTRAK KAWAT (bentuk JSON, status code) dan
// gerbang perakitan — bukan aturan bisnis alur login, yang punya testnya sendiri di
// identity/usecase. Batasnya dijaga sengaja: menduplikasi assertion use case di sini hanya
// menciptakan dua tempat yang harus berubah bersamaan.

// --- Fake seperlunya untuk merakit use case tanpa DB ---

type fakeCreds struct{ byKey map[string]*domain.Credential }

func newFakeCreds() *fakeCreds { return &fakeCreds{byKey: map[string]*domain.Credential{}} }

func (f *fakeCreds) add(c *domain.Credential) {
	f.byKey[string(c.CredType)+"|"+c.CredValue] = c
}
func (f *fakeCreds) Save(_ context.Context, c *domain.Credential) error { f.add(c); return nil }
func (f *fakeCreds) FindByTypeValue(_ context.Context, t domain.CredType, v string) (*domain.Credential, error) {
	if c, ok := f.byKey[string(t)+"|"+v]; ok {
		return c, nil
	}
	return nil, core.ErrNotFound("Credential", string(t))
}
func (f *fakeCreds) ListByPerson(context.Context, uuid.UUID) ([]*domain.Credential, error) {
	return nil, nil
}

type fakePersons struct{ byID map[uuid.UUID]*domain.Person }

func newFakePersons() *fakePersons { return &fakePersons{byID: map[uuid.UUID]*domain.Person{}} }

func (f *fakePersons) Save(_ context.Context, p *domain.Person) error {
	f.byID[p.ID] = p
	return nil
}
func (f *fakePersons) FindByID(_ context.Context, id uuid.UUID) (*domain.Person, error) {
	if p, ok := f.byID[id]; ok {
		return p, nil
	}
	return nil, core.ErrNotFound("Person", id.String())
}
func (f *fakePersons) FindByNIK(context.Context, string) (*domain.Person, error) {
	return nil, core.ErrNotFound("Person", "nik")
}

type fakeEmployments struct {
	byPerson map[uuid.UUID][]*domain.Employment
}

func (f *fakeEmployments) Save(context.Context, *domain.Employment) error { return nil }
func (f *fakeEmployments) FindByID(context.Context, uuid.UUID) (*domain.Employment, error) {
	return nil, core.ErrNotFound("Employment", "id")
}
func (f *fakeEmployments) FindByNIP(context.Context, string) (*domain.Employment, error) {
	return nil, core.ErrNotFound("Employment", "nip")
}
func (f *fakeEmployments) ListByPerson(_ context.Context, id uuid.UUID) ([]*domain.Employment, error) {
	return f.byPerson[id], nil
}

type fakeAssigns struct {
	byEmployment map[uuid.UUID][]*domain.TenantAssignment
}

func (f *fakeAssigns) Save(context.Context, *domain.TenantAssignment) error { return nil }
func (f *fakeAssigns) ListByEmployment(_ context.Context, id uuid.UUID) ([]*domain.TenantAssignment, error) {
	return f.byEmployment[id], nil
}
func (f *fakeAssigns) ListByTenant(context.Context, string) ([]*domain.TenantAssignment, error) {
	return nil, nil
}

type fakeTenants struct{ byID map[string]*domain.Tenant }

func (f *fakeTenants) Save(context.Context, *domain.Tenant) error { return nil }
func (f *fakeTenants) FindByID(_ context.Context, id string) (*domain.Tenant, error) {
	if t, ok := f.byID[id]; ok {
		return t, nil
	}
	return nil, core.ErrNotFound("Tenant", id)
}
func (f *fakeTenants) List(context.Context) ([]*domain.Tenant, error) { return nil, nil }
func (f *fakeTenants) SetActive(context.Context, string, bool) error  { return nil }

type fakeOTPs struct{ byCred map[uuid.UUID]*domain.OTP }

func (f *fakeOTPs) Create(_ context.Context, o *domain.OTP) error {
	f.byCred[o.CredentialID] = o
	return nil
}
func (f *fakeOTPs) FindLatestByCredential(_ context.Context, id uuid.UUID) (*domain.OTP, error) {
	if o, ok := f.byCred[id]; ok {
		return o, nil
	}
	return nil, core.ErrNotFound("OTP", id.String())
}
func (f *fakeOTPs) Update(context.Context, *domain.OTP) error           { return nil }
func (f *fakeOTPs) Consume(context.Context, uuid.UUID) error            { return nil }
func (f *fakeOTPs) RecordAttempt(context.Context, uuid.UUID, int) error { return nil }

type fakeRoles struct{}

func (fakeRoles) EffectiveRoles(context.Context, uuid.UUID, string) ([]string, error) {
	return nil, nil
}

type fakeIssuer struct{}

func (fakeIssuer) Issue(_ context.Context, c port.Claims) (string, error) {
	return "token-" + c.Persona, nil
}

type fakeLimiter struct{ deny bool }

func (f *fakeLimiter) Allow(context.Context, string, int, time.Duration) (bool, error) {
	return !f.deny, nil
}

type fakeMessaging struct {
	sent    int
	failErr error
}

func (f *fakeMessaging) SendEmail(context.Context, string, string, string) error {
	if f.failErr != nil {
		return f.failErr
	}
	f.sent++
	return nil
}
func (f *fakeMessaging) SendSMS(context.Context, string, string) error {
	if f.failErr != nil {
		return f.failErr
	}
	f.sent++
	return nil
}

type fakeCodec struct{}

func (fakeCodec) Generate() (string, string, error) { return "123456", "h:123456", nil }
func (fakeCodec) Verify(hash, code string) error {
	if hash == "h:"+code {
		return nil
	}
	return errors.New("kode salah")
}

// --- Fixture ---

type fixture struct {
	creds     *fakeCreds
	persons   *fakePersons
	emps      *fakeEmployments
	assigns   *fakeAssigns
	tenants   *fakeTenants
	otps      *fakeOTPs
	limiter   *fakeLimiter
	messaging *fakeMessaging
	handler   *identityhttp.Handler
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	fx := &fixture{
		creds:     newFakeCreds(),
		persons:   newFakePersons(),
		emps:      &fakeEmployments{byPerson: map[uuid.UUID][]*domain.Employment{}},
		assigns:   &fakeAssigns{byEmployment: map[uuid.UUID][]*domain.TenantAssignment{}},
		tenants:   &fakeTenants{byID: map[string]*domain.Tenant{}},
		otps:      &fakeOTPs{byCred: map[uuid.UUID]*domain.OTP{}},
		limiter:   &fakeLimiter{},
		messaging: &fakeMessaging{},
	}
	emps, assigns, tenants, otps := fx.emps, fx.assigns, fx.tenants, fx.otps
	passwords := auth.NewBcryptVerifier()
	issuer := fakeIssuer{}
	pol := usecase.DefaultLoginPolicy()

	fx.handler = identityhttp.NewHandler(
		usecase.NewLoginEmployee(fx.creds, fx.persons, emps, assigns, tenants, passwords,
			fakeRoles{}, fakeRoles{}, issuer, fx.limiter, pol),
		usecase.NewSelectTenant(emps, assigns, tenants, fakeRoles{}, fakeRoles{}, issuer),
		usecase.NewLoginCitizen(fx.creds, fx.persons, passwords, issuer, fx.limiter, pol),
		usecase.NewRequestOTP(fx.creds, fx.persons, otps, fakeCodec{}, fx.messaging, fx.limiter,
			testkit.NewNoopLogger(), usecase.DefaultOTPPolicy(), nil),
		usecase.NewVerifyOTP(fx.creds, fx.persons, otps, fakeCodec{}, fx.limiter, issuer,
			usecase.DefaultOTPPolicy(), nil),
	)
	return fx
}

func (fx *fixture) post(path, body string, h http.HandlerFunc) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h(w, r)
	return w
}

// --- Test ---

// Body yang tak bisa di-parse → 400, dan use case tak pernah tersentuh.
func TestHandler_BodyTakValid_400(t *testing.T) {
	fx := newFixture(t)
	w := fx.post("/auth/login", `{bukan json`, fx.handler.LoginEmployee)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("body rusak harus 400, dapat %d — body: %s", w.Code, w.Body.String())
	}
}

// Body raksasa ditolak — rute ini dilayani tanpa otentikasi, jadi tanpa batas satu request bisa
// memaksa server mengalokasikan memori sebesar yang klien mau.
func TestHandler_BodyMelebihiBatas_Ditolak(t *testing.T) {
	fx := newFixture(t)
	besar := `{"cred_type":"nip","cred_value":"` + strings.Repeat("a", 128<<10) + `"}`
	w := fx.post("/auth/login", besar, fx.handler.LoginEmployee)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("body > 64 KiB harus ditolak 400, dapat %d", w.Code)
	}
}

// Kredensial salah → 401, dan pesannya tidak mengutip nilai yang dicoba (jalur samping ADR-009 §6:
// body error mengalir ke log klien & proxy).
func TestHandler_LoginGagal_401TanpaMengutipKredensial(t *testing.T) {
	fx := newFixture(t)
	const nip = "199001012015011001"
	w := fx.post("/auth/login", `{"cred_type":"nip","cred_value":"`+nip+`","password":"x"}`,
		fx.handler.LoginEmployee)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("kredensial tak dikenal harus 401, dapat %d", w.Code)
	}
	if strings.Contains(w.Body.String(), nip) {
		t.Fatalf("respons mengutip NIP yang dicoba: %s", w.Body.String())
	}
}

// Login citizen sukses → 200 + token pada field `token`. Mengunci bentuk kawat: klien nyata
// bergantung padanya, dan ia sengaja terpisah dari struct use case.
func TestHandler_LoginCitizen_BentukResponsToken(t *testing.T) {
	fx := newFixture(t)
	hash, err := auth.NewBcryptVerifier().Hash("rahasia")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	person := &domain.Person{ID: uuid.New(), NIK: "3578010101900010", NamaLengkap: "Warga", IsActive: true}
	_ = fx.persons.Save(context.Background(), person)
	fx.creds.add(&domain.Credential{ID: uuid.New(), PersonID: person.ID,
		CredType: domain.CredEmail, CredValue: "warga@example.com", SecretHash: hash})

	w := fx.post("/auth/public/login",
		`{"cred_type":"email","cred_value":"warga@example.com","password":"rahasia"}`,
		fx.handler.LoginCitizen)

	if w.Code != http.StatusOK {
		t.Fatalf("mau 200, dapat %d — body: %s", w.Code, w.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("respons bukan JSON: %v", err)
	}
	if out["token"] != "token-citizen" {
		t.Fatalf("field token = %v, mau token-citizen (persona citizen)", out["token"])
	}
}

// RequestOTP menjawab 202 untuk kredensial TAK DIKENAL maupun TERDAFTAR-TAPI-GAGAL-KIRIM.
// Inilah kontrak anti-enumerasi di lapis HTTP: membedakan keduanya di sini akan membatalkan
// properti yang dijaga use case, berapa pun rapinya use case itu ditulis.
func TestHandler_RequestOTP_SelaluAccepted(t *testing.T) {
	fx := newFixture(t)
	person := &domain.Person{ID: uuid.New(), NIK: "3578010101900010", NamaLengkap: "Warga", IsActive: true}
	_ = fx.persons.Save(context.Background(), person)
	fx.creds.add(&domain.Credential{ID: uuid.New(), PersonID: person.ID,
		CredType: domain.CredEmail, CredValue: "warga@example.com"})
	fx.messaging.failErr = errors.New("provider down")

	for _, nilai := range []string{"warga@example.com", "entah-siapa@example.com"} {
		w := fx.post("/auth/public/otp/request",
			`{"cred_type":"email","cred_value":"`+nilai+`"}`, fx.handler.RequestOTP)
		if w.Code != http.StatusAccepted {
			t.Fatalf("%s: mau 202, dapat %d — body: %s", nilai, w.Code, w.Body.String())
		}
	}
}

// Konstruktor menolak use case nil saat PERAKITAN, bukan pada request pertama di produksi.
func TestNewHandler_UseCaseNil_Panic(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("NewHandler dengan use case nil harus panic saat perakitan — " +
				"kalau tidak, kegagalannya muncul pada pengguna, bukan saat boot")
		}
	}()
	identityhttp.NewHandler(nil, nil, nil, nil, nil)
}

// seedPegawai menyeed pegawai lengkap: person aktif + employment ASN + credential NIP berpassword
// + penugasan ke satu tenant aktif. Cukup untuk menempuh jalur sukses LoginEmployee.
func (fx *fixture) seedPegawai(t *testing.T, nip, password, tenantID string) *domain.Person {
	t.Helper()
	hash, err := auth.NewBcryptVerifier().Hash(password)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	p := &domain.Person{ID: uuid.New(), NIK: "3578010101900001", NamaLengkap: "Budi", IsActive: true}
	_ = fx.persons.Save(context.Background(), p)

	emp := &domain.Employment{ID: uuid.New(), PersonID: p.ID, Status: domain.StatusASN,
		NIP: nip, IsActive: true, ValidFrom: time.Now().Add(-24 * time.Hour)}
	fx.emps.byPerson[p.ID] = []*domain.Employment{emp}

	fx.creds.add(&domain.Credential{ID: uuid.New(), PersonID: p.ID,
		CredType: domain.CredNIP, CredValue: nip, SecretHash: hash})

	fx.tenants.byID[tenantID] = &domain.Tenant{TenantID: tenantID, Nama: tenantID,
		Tier: domain.TierShared, DBHost: "h", DBName: tenantID, IsActive: true}
	fx.assigns.byEmployment[emp.ID] = []*domain.TenantAssignment{{
		ID: uuid.New(), EmploymentID: emp.ID, TenantID: tenantID, IsHomeTenant: true,
		AssignedBy: p.ID, ValidFrom: time.Now().Add(-24 * time.Hour),
	}}
	return p
}

// Login employee dengan SATU tenant → token final + need_tenant_selection=false + daftar tenant.
// Mengunci bentuk kawat loginResponse, yang sengaja terpisah dari LoginResult use case.
func TestHandler_LoginEmployee_TenantTunggal_BentukRespons(t *testing.T) {
	fx := newFixture(t)
	const nip, tenantID = "199001012015011001", "pemkot-a"
	fx.seedPegawai(t, nip, "rahasia", tenantID)

	w := fx.post("/auth/login",
		`{"cred_type":"nip","cred_value":"`+nip+`","password":"rahasia"}`, fx.handler.LoginEmployee)

	if w.Code != http.StatusOK {
		t.Fatalf("mau 200, dapat %d — body: %s", w.Code, w.Body.String())
	}
	var out struct {
		Token               string `json:"token"`
		NeedTenantSelection bool   `json:"need_tenant_selection"`
		Tenants             []struct {
			TenantID     string `json:"tenant_id"`
			IsHomeTenant bool   `json:"is_home_tenant"`
		} `json:"tenants"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("respons bukan JSON yang diharapkan: %v — %s", err, w.Body.String())
	}
	if out.Token != "token-employee" {
		t.Errorf("token = %q, mau token-employee", out.Token)
	}
	if out.NeedTenantSelection {
		t.Error("tenant tunggal tak boleh menuntut pemilihan")
	}
	if len(out.Tenants) != 1 || out.Tenants[0].TenantID != tenantID || !out.Tenants[0].IsHomeTenant {
		t.Errorf("daftar tenant = %+v, mau [{%s true}]", out.Tenants, tenantID)
	}
}

// SelectTenant pada request TANPA gateway.Context (anonim) → 401, bukan panic dan bukan sukses.
// Di produksi RequireAuth sudah menyaring lebih dulu; ini penjaga lapis kedua, sebab use case
// mengambil person_id dari AuthContext yang pada request anonim bernilai uuid.Nil.
func TestHandler_SelectTenant_Anonim_401(t *testing.T) {
	fx := newFixture(t)
	w := fx.post("/auth/select-tenant", `{"tenant_id":"pemkot-a"}`, fx.handler.SelectTenant)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("select-tenant anonim harus 401, dapat %d — body: %s", w.Code, w.Body.String())
	}
}

// VerifyOTP dengan kode benar → 200 + token persona citizen (tanpa role internal).
func TestHandler_VerifyOTP_KodeBenar_TokenCitizen(t *testing.T) {
	fx := newFixture(t)
	p := &domain.Person{ID: uuid.New(), NIK: "3578010101900010", NamaLengkap: "Warga", IsActive: true}
	_ = fx.persons.Save(context.Background(), p)
	cred := &domain.Credential{ID: uuid.New(), PersonID: p.ID,
		CredType: domain.CredEmail, CredValue: "warga@example.com"}
	fx.creds.add(cred)

	// Terbitkan OTP lewat handler request (sekaligus menutupi jalur sukses RequestOTP).
	if w := fx.post("/auth/public/otp/request",
		`{"cred_type":"email","cred_value":"warga@example.com"}`, fx.handler.RequestOTP); w.Code != http.StatusAccepted {
		t.Fatalf("request OTP: mau 202, dapat %d", w.Code)
	}
	if fx.messaging.sent != 1 {
		t.Fatalf("OTP harus terkirim sekali, terkirim %d kali", fx.messaging.sent)
	}

	w := fx.post("/auth/public/otp/verify",
		`{"cred_type":"email","cred_value":"warga@example.com","code":"123456"}`, fx.handler.VerifyOTP)
	if w.Code != http.StatusOK {
		t.Fatalf("verify OTP: mau 200, dapat %d — body: %s", w.Code, w.Body.String())
	}
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if out["token"] != "token-citizen" {
		t.Fatalf("token = %v, mau token-citizen — jalur OTP tak boleh menerbitkan persona lain", out["token"])
	}
}

// Kode OTP salah → 401 seragam.
func TestHandler_VerifyOTP_KodeSalah_401(t *testing.T) {
	fx := newFixture(t)
	p := &domain.Person{ID: uuid.New(), NIK: "3578010101900010", NamaLengkap: "Warga", IsActive: true}
	_ = fx.persons.Save(context.Background(), p)
	fx.creds.add(&domain.Credential{ID: uuid.New(), PersonID: p.ID,
		CredType: domain.CredEmail, CredValue: "warga@example.com"})
	_ = fx.post("/auth/public/otp/request",
		`{"cred_type":"email","cred_value":"warga@example.com"}`, fx.handler.RequestOTP)

	w := fx.post("/auth/public/otp/verify",
		`{"cred_type":"email","cred_value":"warga@example.com","code":"999999"}`, fx.handler.VerifyOTP)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("kode salah harus 401, dapat %d", w.Code)
	}
}
