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

// --- Fakes khusus jalur OTP ---

type fakeOTPs struct {
	byCredLatest map[uuid.UUID]*domain.OTP // credential_id -> OTP terbaru
	byID         map[uuid.UUID]*domain.OTP
}

func newFakeOTPs() *fakeOTPs {
	return &fakeOTPs{byCredLatest: map[uuid.UUID]*domain.OTP{}, byID: map[uuid.UUID]*domain.OTP{}}
}
func (f *fakeOTPs) Create(_ context.Context, o *domain.OTP) error {
	f.byCredLatest[o.CredentialID] = o
	f.byID[o.ID] = o
	return nil
}
func (f *fakeOTPs) FindLatestByCredential(_ context.Context, credID uuid.UUID) (*domain.OTP, error) {
	if o, ok := f.byCredLatest[credID]; ok {
		return o, nil
	}
	return nil, core.ErrNotFound("OTP", credID.String())
}
func (f *fakeOTPs) RecordAttempt(_ context.Context, id uuid.UUID, attempts int) error {
	if o, ok := f.byID[id]; ok {
		o.Attempts = attempts
	}
	return nil
}
func (f *fakeOTPs) Consume(_ context.Context, id uuid.UUID) error {
	if o, ok := f.byID[id]; ok && o.ConsumedAt == nil {
		t := time.Now()
		o.ConsumedAt = &t
	}
	return nil
}

// fakeCodec: Generate selalu kode "123456" hash "h:123456"; Verify cocok bila hash == "h:"+code.
type fakeCodec struct {
	genErr error
}

func (f fakeCodec) Generate() (string, string, error) {
	if f.genErr != nil {
		return "", "", f.genErr
	}
	return "123456", "h:123456", nil
}
func (f fakeCodec) Verify(hash, code string) error {
	if hash == "h:"+code {
		return nil
	}
	return errors.New("tidak cocok")
}

type sentMessage struct {
	kind string // "sms" | "email"
	to   string
	body string
	subj string
}

type fakeMessaging struct {
	sent    []sentMessage
	failErr error
}

func (f *fakeMessaging) SendSMS(_ context.Context, to, msg string) error {
	if f.failErr != nil {
		return f.failErr
	}
	f.sent = append(f.sent, sentMessage{kind: "sms", to: to, body: msg})
	return nil
}
func (f *fakeMessaging) SendEmail(_ context.Context, to, subj, body string) error {
	if f.failErr != nil {
		return f.failErr
	}
	f.sent = append(f.sent, sentMessage{kind: "email", to: to, subj: subj, body: body})
	return nil
}

// fakeLimiter: izinkan hingga `allowN` percobaan per key; setelahnya tolak. err untuk uji fail-closed.
type fakeLimiter struct {
	allowN map[string]int
	calls  map[string]int
	err    error
}

func newFakeLimiter() *fakeLimiter {
	return &fakeLimiter{allowN: map[string]int{}, calls: map[string]int{}}
}
func (f *fakeLimiter) Allow(_ context.Context, key string, _ int, _ time.Duration) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	f.calls[key]++
	limit, ok := f.allowN[key]
	if !ok {
		return true, nil // default: izinkan semua
	}
	return f.calls[key] <= limit, nil
}

// --- Fixture ---

type otpFixture struct {
	creds     *fakeCreds
	persons   *fakePersons
	otps      *fakeOTPs
	codec     fakeCodec
	messaging *fakeMessaging
	limiter   *fakeLimiter
	issuer    *fakeIssuer
	now       time.Time
}

func newOTPFixture() *otpFixture {
	return &otpFixture{
		creds:     newFakeCreds(),
		persons:   newFakePersons(),
		otps:      newFakeOTPs(),
		messaging: &fakeMessaging{},
		limiter:   newFakeLimiter(),
		issuer:    &fakeIssuer{},
		now:       time.Now(),
	}
}

func (fx *otpFixture) clock() func() time.Time { return func() time.Time { return fx.now } }

func (fx *otpFixture) requestOTP() *usecase.RequestOTP {
	return usecase.NewRequestOTP(fx.creds, fx.persons, fx.otps, fx.codec, fx.messaging,
		fx.limiter, usecase.DefaultOTPPolicy(), fx.clock())
}
func (fx *otpFixture) verifyOTP() *usecase.VerifyOTP {
	return usecase.NewVerifyOTP(fx.creds, fx.persons, fx.otps, fx.codec, fx.limiter,
		fx.issuer, usecase.DefaultOTPPolicy(), fx.clock())
}

// seedCitizen membuat person aktif + credential email (publik, tanpa password — OTP-only).
func (fx *otpFixture) seedCitizen(t *testing.T) (*domain.Person, *domain.Credential) {
	t.Helper()
	p := &domain.Person{ID: uuid.New(), NIK: "3578010101900010", NamaLengkap: "Warga", IsActive: true}
	_ = fx.persons.Save(context.Background(), p)
	c := &domain.Credential{ID: uuid.New(), PersonID: p.ID, CredType: domain.CredEmail,
		CredValue: "warga@example.com"}
	fx.creds.add(c)
	return p, c
}

func assertTooManyRequests(t *testing.T, err error) {
	t.Helper()
	var fe *core.FrameworkError
	if !errors.As(err, &fe) || fe.Code != "TOO_MANY_REQUESTS" {
		t.Fatalf("harus TOO_MANY_REQUESTS, dapat: %v", err)
	}
}

// --- RequestOTP ---

func TestRequestOTP_Email_Success(t *testing.T) {
	fx := newOTPFixture()
	_, cred := fx.seedCitizen(t)

	err := fx.requestOTP().Execute(context.Background(), usecase.RequestOTPInput{
		CredType: domain.CredEmail, CredValue: "warga@example.com",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(fx.messaging.sent) != 1 || fx.messaging.sent[0].kind != "email" {
		t.Fatalf("harus kirim 1 email: %+v", fx.messaging.sent)
	}
	if fx.messaging.sent[0].to != "warga@example.com" {
		t.Fatalf("tujuan salah: %+v", fx.messaging.sent[0])
	}
	otp, err := fx.otps.FindLatestByCredential(context.Background(), cred.ID)
	if err != nil {
		t.Fatalf("OTP harus tersimpan: %v", err)
	}
	if otp.CodeHash == "123456" || otp.CodeHash != "h:123456" {
		t.Fatalf("OTP harus disimpan sebagai HASH, bukan plaintext: %q", otp.CodeHash)
	}
}

func TestRequestOTP_NoHP_Success(t *testing.T) {
	fx := newOTPFixture()
	p := &domain.Person{ID: uuid.New(), NIK: "3578010101900011", NamaLengkap: "Warga2", IsActive: true}
	_ = fx.persons.Save(context.Background(), p)
	fx.creds.add(&domain.Credential{ID: uuid.New(), PersonID: p.ID, CredType: domain.CredNoHP,
		CredValue: "08123456789"})

	err := fx.requestOTP().Execute(context.Background(), usecase.RequestOTPInput{
		CredType: domain.CredNoHP, CredValue: "08123456789",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(fx.messaging.sent) != 1 || fx.messaging.sent[0].kind != "sms" {
		t.Fatalf("harus kirim 1 sms: %+v", fx.messaging.sent)
	}
}

// Enumeration-resistance: credential tak dikenal → sukses senyap, tanpa kirim & tanpa OTP.
func TestRequestOTP_UnknownCredential_SilentNoSend(t *testing.T) {
	fx := newOTPFixture()
	err := fx.requestOTP().Execute(context.Background(), usecase.RequestOTPInput{
		CredType: domain.CredEmail, CredValue: "tidak-ada@example.com",
	})
	if err != nil {
		t.Fatalf("harus nil (senyap), dapat: %v", err)
	}
	if len(fx.messaging.sent) != 0 {
		t.Fatalf("tak boleh kirim apa pun: %+v", fx.messaging.sent)
	}
}

func TestRequestOTP_InactivePerson_Silent(t *testing.T) {
	fx := newOTPFixture()
	p := &domain.Person{ID: uuid.New(), NIK: "3578010101900012", NamaLengkap: "NonAktif", IsActive: false}
	_ = fx.persons.Save(context.Background(), p)
	fx.creds.add(&domain.Credential{ID: uuid.New(), PersonID: p.ID, CredType: domain.CredEmail,
		CredValue: "nonaktif@example.com"})

	err := fx.requestOTP().Execute(context.Background(), usecase.RequestOTPInput{
		CredType: domain.CredEmail, CredValue: "nonaktif@example.com",
	})
	if err != nil {
		t.Fatalf("harus nil (senyap), dapat: %v", err)
	}
	if len(fx.messaging.sent) != 0 {
		t.Fatalf("tak boleh kirim untuk person non-aktif: %+v", fx.messaging.sent)
	}
}

func TestRequestOTP_NIPRejected(t *testing.T) {
	fx := newOTPFixture()
	err := fx.requestOTP().Execute(context.Background(), usecase.RequestOTPInput{
		CredType: domain.CredNIP, CredValue: "199001012015011001",
	})
	assertUnauthorized(t, err)
}

func TestRequestOTP_RateLimited(t *testing.T) {
	fx := newOTPFixture()
	fx.seedCitizen(t)
	fx.limiter.allowN[usecase.OTPRequestKeyForTest(domain.CredEmail, "warga@example.com")] = 0

	err := fx.requestOTP().Execute(context.Background(), usecase.RequestOTPInput{
		CredType: domain.CredEmail, CredValue: "warga@example.com",
	})
	assertTooManyRequests(t, err)
	if len(fx.messaging.sent) != 0 {
		t.Fatal("rate-limited tak boleh kirim")
	}
}

func TestRequestOTP_LimiterError_FailClosed(t *testing.T) {
	fx := newOTPFixture()
	fx.seedCitizen(t)
	fx.limiter.err = errors.New("store down")

	err := fx.requestOTP().Execute(context.Background(), usecase.RequestOTPInput{
		CredType: domain.CredEmail, CredValue: "warga@example.com",
	})
	if err == nil {
		t.Fatal("limiter error harus fail-closed (aksi tak lanjut)")
	}
	if len(fx.messaging.sent) != 0 {
		t.Fatal("fail-closed tak boleh kirim")
	}
}

func TestRequestOTP_MessagingFails_ReturnsError(t *testing.T) {
	fx := newOTPFixture()
	_, cred := fx.seedCitizen(t)
	fx.messaging.failErr = errors.New("provider down")

	err := fx.requestOTP().Execute(context.Background(), usecase.RequestOTPInput{
		CredType: domain.CredEmail, CredValue: "warga@example.com",
	})
	if err == nil {
		t.Fatal("kegagalan kirim harus mengembalikan error")
	}
	// OTP tetap tersimpan (penerbitan sukses sebelum kirim).
	if _, e := fx.otps.FindLatestByCredential(context.Background(), cred.ID); e != nil {
		t.Fatalf("OTP harus tetap tersimpan: %v", e)
	}
}

// --- VerifyOTP ---

// seedOTP menambahkan OTP aktif untuk credential dengan hash kode "123456".
func (fx *otpFixture) seedOTP(cred *domain.Credential) *domain.OTP {
	o := &domain.OTP{
		ID: uuid.New(), CredentialID: cred.ID, CodeHash: "h:123456",
		ExpiresAt: fx.now.Add(5 * time.Minute), CreatedAt: fx.now,
	}
	_ = fx.otps.Create(context.Background(), o)
	return o
}

// INVARIANT: ASN (punya employment + role sentral) login publik via OTP → token citizen tanpa role.
func TestVerifyOTP_Success_IssuesCitizenToken_NoInternalRoles(t *testing.T) {
	fx := newOTPFixture()
	p, cred := fx.seedCitizen(t)
	fx.seedOTP(cred)

	token, err := fx.verifyOTP().Execute(context.Background(), usecase.VerifyOTPInput{
		CredType: domain.CredEmail, CredValue: "warga@example.com", Code: "123456",
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
	if c.PersonID != p.ID {
		t.Fatalf("person_id salah: %+v", c)
	}
	if c.TenantID != "" || c.EmploymentStatus != "" || len(c.CentralRoles) != 0 || len(c.TenantRoles) != 0 {
		t.Fatalf("token citizen bocor data internal: %+v", c)
	}
}

func TestVerifyOTP_WrongCode_IncrementsAttempts(t *testing.T) {
	fx := newOTPFixture()
	_, cred := fx.seedCitizen(t)
	otp := fx.seedOTP(cred)

	_, err := fx.verifyOTP().Execute(context.Background(), usecase.VerifyOTPInput{
		CredType: domain.CredEmail, CredValue: "warga@example.com", Code: "000000",
	})
	assertUnauthorized(t, err)
	if otp.Attempts != 1 {
		t.Fatalf("attempts harus 1, dapat %d", otp.Attempts)
	}
}

func TestVerifyOTP_AttemptsExhausted_Consumes(t *testing.T) {
	fx := newOTPFixture()
	_, cred := fx.seedCitizen(t)
	otp := fx.seedOTP(cred)
	otp.Attempts = domain.MaxOTPAttempts - 1 // satu percobaan lagi mencapai batas

	_, err := fx.verifyOTP().Execute(context.Background(), usecase.VerifyOTPInput{
		CredType: domain.CredEmail, CredValue: "warga@example.com", Code: "000000",
	})
	assertUnauthorized(t, err)
	if !otp.IsConsumed() {
		t.Fatal("OTP harus dihanguskan saat attempts habis")
	}
	// Bahkan kode benar setelah hangus harus ditolak.
	_, err = fx.verifyOTP().Execute(context.Background(), usecase.VerifyOTPInput{
		CredType: domain.CredEmail, CredValue: "warga@example.com", Code: "123456",
	})
	assertUnauthorized(t, err)
}

func TestVerifyOTP_Expired_Rejected(t *testing.T) {
	fx := newOTPFixture()
	_, cred := fx.seedCitizen(t)
	o := fx.seedOTP(cred)
	o.ExpiresAt = fx.now.Add(-time.Second) // sudah lewat

	_, err := fx.verifyOTP().Execute(context.Background(), usecase.VerifyOTPInput{
		CredType: domain.CredEmail, CredValue: "warga@example.com", Code: "123456",
	})
	assertUnauthorized(t, err)
}

func TestVerifyOTP_NoOTP_Rejected(t *testing.T) {
	fx := newOTPFixture()
	fx.seedCitizen(t) // tanpa seedOTP
	_, err := fx.verifyOTP().Execute(context.Background(), usecase.VerifyOTPInput{
		CredType: domain.CredEmail, CredValue: "warga@example.com", Code: "123456",
	})
	assertUnauthorized(t, err)
}

func TestVerifyOTP_ReplayAfterSuccess_Rejected(t *testing.T) {
	fx := newOTPFixture()
	_, cred := fx.seedCitizen(t)
	fx.seedOTP(cred)

	if _, err := fx.verifyOTP().Execute(context.Background(), usecase.VerifyOTPInput{
		CredType: domain.CredEmail, CredValue: "warga@example.com", Code: "123456",
	}); err != nil {
		t.Fatalf("verifikasi pertama harus sukses: %v", err)
	}
	// Replay kode yang sama → ditolak (OTP sudah dihanguskan).
	_, err := fx.verifyOTP().Execute(context.Background(), usecase.VerifyOTPInput{
		CredType: domain.CredEmail, CredValue: "warga@example.com", Code: "123456",
	})
	assertUnauthorized(t, err)
}

func TestVerifyOTP_NIPRejected(t *testing.T) {
	fx := newOTPFixture()
	_, err := fx.verifyOTP().Execute(context.Background(), usecase.VerifyOTPInput{
		CredType: domain.CredNIP, CredValue: "199001012015011001", Code: "123456",
	})
	assertUnauthorized(t, err)
}

func TestVerifyOTP_RateLimited(t *testing.T) {
	fx := newOTPFixture()
	_, cred := fx.seedCitizen(t)
	fx.seedOTP(cred)
	fx.limiter.allowN[usecase.OTPVerifyKeyForTest(domain.CredEmail, "warga@example.com")] = 0

	_, err := fx.verifyOTP().Execute(context.Background(), usecase.VerifyOTPInput{
		CredType: domain.CredEmail, CredValue: "warga@example.com", Code: "123456",
	})
	assertTooManyRequests(t, err)
}

var _ port.OTPCodec = fakeCodec{}
var _ port.MessagingPort = (*fakeMessaging)(nil)
var _ port.RateLimiter = (*fakeLimiter)(nil)
var _ domain.OTPRepository = (*fakeOTPs)(nil)
