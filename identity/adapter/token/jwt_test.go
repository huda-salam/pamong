package token

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/port"
)

// fakeRevoked = denylist jti in-memory untuk unit test (tanpa DB). Precedent identity:
// adapter pakai fake lokal alih-alih mock generik.
type fakeRevoked struct {
	set map[uuid.UUID]bool
	err error
}

func (f *fakeRevoked) Revoke(_ context.Context, jti, _ uuid.UUID, _ time.Time, _ string) error {
	if f.err != nil {
		return f.err
	}
	if f.set == nil {
		f.set = map[uuid.UUID]bool{}
	}
	f.set[jti] = true
	return nil
}

func (f *fakeRevoked) IsRevoked(_ context.Context, jti uuid.UUID) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	return f.set[jti], nil
}

var testSecret = []byte("rahasia-uji-yang-cukup-panjang-32xx")

func sampleClaims() port.Claims {
	return port.Claims{
		PersonID:         uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		Persona:          "employee",
		EmploymentStatus: "asn",
		TenantID:         "pemkot-surabaya",
		CentralRoles:     []string{"platform_helpdesk"},
		TenantRoles:      []string{"verifikator_keuangan", "operator_surat"},
		TenantScope:      []string{"pemkot-surabaya"},
		IsCrossTenant:    true,
	}
}

func assertUnauthorized(t *testing.T, err error) {
	t.Helper()
	var fe *core.FrameworkError
	if !errors.As(err, &fe) {
		t.Fatalf("expect *core.FrameworkError, got %T: %v", err, err)
	}
	if fe.Code != "UNAUTHORIZED" {
		t.Fatalf("expect code UNAUTHORIZED, got %q (%s)", fe.Code, fe.Message)
	}
}

// TestJWTCodec_IssueVerify_RoundTrip — DoD: token valid diverifikasi; SEMUA klaim utuh.
func TestJWTCodec_IssueVerify_RoundTrip(t *testing.T) {
	c := NewJWTCodec(testSecret, time.Hour, &fakeRevoked{})
	now := time.Unix(1_700_000_000, 0).UTC()
	c.now = func() time.Time { return now }

	in := sampleClaims()
	raw, err := c.Issue(context.Background(), in)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	out, err := c.Verify(context.Background(), raw)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}

	if out.PersonID != in.PersonID || out.Persona != in.Persona ||
		out.EmploymentStatus != in.EmploymentStatus || out.TenantID != in.TenantID ||
		out.IsCrossTenant != in.IsCrossTenant {
		t.Fatalf("klaim skalar tak cocok:\n got %+v\nwant %+v", out, in)
	}
	if !reflect.DeepEqual(out.CentralRoles, in.CentralRoles) ||
		!reflect.DeepEqual(out.TenantRoles, in.TenantRoles) ||
		!reflect.DeepEqual(out.TenantScope, in.TenantScope) {
		t.Fatalf("klaim slice tak cocok:\n got %+v\nwant %+v", out, in)
	}
	// Klaim infrastruktur diisi codec.
	if out.IssuedAt.Unix() != now.Unix() {
		t.Fatalf("iat = %d, want %d", out.IssuedAt.Unix(), now.Unix())
	}
	if out.ExpiresAt.Unix() != now.Add(time.Hour).Unix() {
		t.Fatalf("exp = %d, want %d", out.ExpiresAt.Unix(), now.Add(time.Hour).Unix())
	}
	if _, err := uuid.Parse(out.TokenID); err != nil {
		t.Fatalf("jti bukan uuid: %q", out.TokenID)
	}
}

// TestJWTCodec_Issue_UniqueJTI — tiap penerbitan punya jti berbeda (untuk revocation).
func TestJWTCodec_Issue_UniqueJTI(t *testing.T) {
	c := NewJWTCodec(testSecret, time.Hour, &fakeRevoked{})
	raw1, err := c.Issue(context.Background(), sampleClaims())
	if err != nil {
		t.Fatal(err)
	}
	raw2, err := c.Issue(context.Background(), sampleClaims())
	if err != nil {
		t.Fatal(err)
	}
	o1, err := c.Verify(context.Background(), raw1)
	if err != nil {
		t.Fatal(err)
	}
	o2, err := c.Verify(context.Background(), raw2)
	if err != nil {
		t.Fatal(err)
	}
	if o1.TokenID == o2.TokenID {
		t.Fatalf("jti tidak unik: %s", o1.TokenID)
	}
}

// TestJWTCodec_Verify_Revoked — DoD: token revoked ditolak.
func TestJWTCodec_Verify_Revoked(t *testing.T) {
	store := &fakeRevoked{}
	c := NewJWTCodec(testSecret, time.Hour, store)

	raw, err := c.Issue(context.Background(), sampleClaims())
	if err != nil {
		t.Fatal(err)
	}
	out, err := c.Verify(context.Background(), raw)
	if err != nil {
		t.Fatalf("sebelum dicabut harus valid: %v", err)
	}

	if err := store.Revoke(context.Background(), uuid.MustParse(out.TokenID), out.PersonID, out.ExpiresAt, "uji"); err != nil {
		t.Fatal(err)
	}
	_, err = c.Verify(context.Background(), raw)
	assertUnauthorized(t, err)
}

// TestJWTCodec_Verify_Expired — token kedaluwarsa ditolak (clock dimajukan melewati exp).
func TestJWTCodec_Verify_Expired(t *testing.T) {
	c := NewJWTCodec(testSecret, time.Hour, &fakeRevoked{})
	base := time.Unix(1_700_000_000, 0).UTC()
	c.now = func() time.Time { return base }

	raw, err := c.Issue(context.Background(), sampleClaims())
	if err != nil {
		t.Fatal(err)
	}
	c.now = func() time.Time { return base.Add(2 * time.Hour) } // lewati exp
	_, err = c.Verify(context.Background(), raw)
	assertUnauthorized(t, err)
}

// TestJWTCodec_Verify_TamperedSignature — tanda tangan diubah → ditolak.
func TestJWTCodec_Verify_TamperedSignature(t *testing.T) {
	c := NewJWTCodec(testSecret, time.Hour, &fakeRevoked{})
	raw, err := c.Issue(context.Background(), sampleClaims())
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		t.Fatalf("token bukan 3 segmen: %q", raw)
	}
	last := parts[2][len(parts[2])-1]
	repl := byte('A')
	if last == 'A' {
		repl = 'B'
	}
	parts[2] = parts[2][:len(parts[2])-1] + string(repl)
	_, err = c.Verify(context.Background(), strings.Join(parts, "."))
	assertUnauthorized(t, err)
}

// TestJWTCodec_Verify_WrongSecret — token sah tapi diverifikasi dengan secret lain → ditolak.
func TestJWTCodec_Verify_WrongSecret(t *testing.T) {
	issuer := NewJWTCodec(testSecret, time.Hour, &fakeRevoked{})
	raw, err := issuer.Issue(context.Background(), sampleClaims())
	if err != nil {
		t.Fatal(err)
	}
	verifier := NewJWTCodec([]byte("secret-lain-yang-juga-cukup-panjang"), time.Hour, &fakeRevoked{})
	_, err = verifier.Verify(context.Background(), raw)
	assertUnauthorized(t, err)
}

// TestJWTCodec_Verify_AlgNoneRejected — token alg=none (tanpa tanda tangan) → ditolak
// (pin algoritma mencegah alg=none / alg-confusion).
func TestJWTCodec_Verify_AlgNoneRejected(t *testing.T) {
	c := NewJWTCodec(testSecret, time.Hour, &fakeRevoked{})
	raw, err := jwt.NewWithClaims(jwt.SigningMethodNone, validJWTClaims()).
		SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Verify(context.Background(), raw)
	assertUnauthorized(t, err)
}

// TestJWTCodec_Verify_WrongIssuerRejected — issuer asing, walau tanda tangan benar → ditolak.
func TestJWTCodec_Verify_WrongIssuerRejected(t *testing.T) {
	c := NewJWTCodec(testSecret, time.Hour, &fakeRevoked{})
	claims := validJWTClaims()
	claims.Issuer = "penipu" // bukan internalIssuer
	raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(testSecret)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Verify(context.Background(), raw)
	assertUnauthorized(t, err)
}

// TestJWTCodec_Verify_StoreError_FailClosed — bila store revocation gagal, Verify menolak
// (fail-closed) dengan error internal (BUKAN 401, agar terlihat sebagai kegagalan sistem).
func TestJWTCodec_Verify_StoreError_FailClosed(t *testing.T) {
	c := NewJWTCodec(testSecret, time.Hour, &fakeRevoked{err: errors.New("db mati")})
	raw, err := c.Issue(context.Background(), sampleClaims()) // Issue tak menyentuh store
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Verify(context.Background(), raw)
	if err == nil {
		t.Fatal("expect error saat store gagal (fail-closed)")
	}
	var fe *core.FrameworkError
	if errors.As(err, &fe) && fe.Code == "UNAUTHORIZED" {
		t.Fatalf("kegagalan store seharusnya internal (500), bukan 401: %v", err)
	}
}

// validJWTClaims membangun jwtClaims sah (iss/aud/exp benar) untuk uji negatif yang
// memalsukan algoritma/issuer dengan menandatangani manual.
func validJWTClaims() jwtClaims {
	now := time.Now()
	return jwtClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    internalIssuer,
			Subject:   uuid.NewString(),
			Audience:  jwt.ClaimStrings{internalAudience},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
			ID:        uuid.NewString(),
		},
		Persona: "employee",
	}
}
