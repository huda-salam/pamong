package usecase

import (
	"context"
	"time"

	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/port"
)

// VerifyOTP memverifikasi kode OTP untuk credential publik (email/no_hp) lalu menerbitkan token
// persona=citizen — TANPA role internal (resolver role tak pernah dipanggil; invariant 2.4.3).
//
// Dua lapis proteksi tebakan: (1) cap percobaan per-OTP (domain.MaxOTPAttempts) yang menghanguskan
// OTP saat habis; (2) rate limit verifikasi per-kredensial (lintas OTP) untuk mencegah rapid-fire.
// Semua kegagalan dikembalikan SERAGAM (errInvalidOTP, 401) agar tak membocorkan tahap yang gagal.
type VerifyOTP struct {
	creds   domain.CredentialRepository
	persons domain.PersonRepository
	otps    domain.OTPRepository
	codec   port.OTPCodec
	limiter port.RateLimiter
	issuer  port.TokenIssuer
	policy  OTPPolicy
	now     func() time.Time
}

// NewVerifyOTP merakit alur verifikasi OTP. Tidak menerima resolver role apa pun — disengaja
// (token citizen mustahil membawa role internal). now opsional (nil → time.Now).
func NewVerifyOTP(
	creds domain.CredentialRepository,
	persons domain.PersonRepository,
	otps domain.OTPRepository,
	codec port.OTPCodec,
	limiter port.RateLimiter,
	issuer port.TokenIssuer,
	policy OTPPolicy,
	now func() time.Time,
) *VerifyOTP {
	if now == nil {
		now = time.Now
	}
	return &VerifyOTP{
		creds: creds, persons: persons, otps: otps, codec: codec,
		limiter: limiter, issuer: issuer, policy: policy, now: now,
	}
}

// VerifyOTPInput DTO masuk dari portal publik.
type VerifyOTPInput struct {
	CredType  domain.CredType // email | no_hp
	CredValue string
	Code      string // kode OTP yang diketik warga
}

// Execute mencocokkan kode dengan OTP terbaru milik credential lalu menerbitkan token citizen.
func (uc *VerifyOTP) Execute(ctx context.Context, in VerifyOTPInput) (string, error) {
	if !otpCredTypes[in.CredType] {
		return "", errInvalidOTP()
	}

	// Rate limit verifikasi per kredensial (lintas OTP) — sebelum lookup.
	allowed, err := uc.limiter.Allow(ctx, otpVerifyKey(in.CredType, in.CredValue),
		uc.policy.VerifyLimit, uc.policy.VerifyWindow)
	if err != nil {
		return "", err // fail-closed (500)
	}
	if !allowed {
		return "", errTooManyOTP()
	}

	cred, err := uc.creds.FindByTypeValue(ctx, in.CredType, in.CredValue)
	if err != nil {
		if isNotFound(err) {
			return "", errInvalidOTP()
		}
		return "", err
	}

	otp, err := uc.otps.FindLatestByCredential(ctx, cred.ID)
	if err != nil {
		if isNotFound(err) {
			return "", errInvalidOTP()
		}
		return "", err
	}
	if !otp.IsUsable(uc.now()) {
		return "", errInvalidOTP()
	}

	if err := uc.codec.Verify(otp.CodeHash, in.Code); err != nil {
		// Tebakan salah: catat percobaan; hanguskan OTP bila batas tercapai.
		otp.Attempts++
		if rerr := uc.otps.RecordAttempt(ctx, otp.ID, otp.Attempts); rerr != nil {
			return "", rerr
		}
		if otp.AttemptsExhausted() {
			if cerr := uc.otps.Consume(ctx, otp.ID); cerr != nil {
				return "", cerr
			}
		}
		return "", errInvalidOTP()
	}

	// Kode benar → OTP sekali pakai: hanguskan sebelum menerbitkan token (cegah replay).
	if err := uc.otps.Consume(ctx, otp.ID); err != nil {
		return "", err
	}

	person, err := uc.persons.FindByID(ctx, cred.PersonID)
	if err != nil {
		if isNotFound(err) {
			return "", errInvalidOTP()
		}
		return "", err
	}
	if !person.IsActive {
		return "", errInvalidOTP()
	}

	// Persona citizen: tanpa tenant, tanpa employment_status, tanpa role internal.
	return uc.issuer.Issue(ctx, port.Claims{
		PersonID: person.ID,
		Persona:  domain.PersonaCitizen,
	})
}

// otpVerifyKey merakit key rate limiter verifikasi, ber-scope per (jenis kanal, nilai kredensial).
func otpVerifyKey(t domain.CredType, value string) string {
	return "otp:verify:" + string(t) + ":" + value
}
