package usecase

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/port"
)

// RequestOTP menerbitkan kode OTP untuk credential publik (email/no_hp) dan mengirimnya lewat
// MessagingPort. Tidak menerbitkan token — itu tugas VerifyOTP setelah kode dicocokkan.
//
// Enumeration-resistance: untuk credential yang tak dikenal / person non-aktif, Execute
// mengembalikan nil (seolah sukses) TANPA mengirim apa pun — tidak membocorkan apakah email/no_hp
// terdaftar. Rate limit diterapkan SEBELUM lookup sehingga endpoint ini tak bisa dipakai membanjiri
// kanal maupun menyelidiki keberadaan akun lewat laju.
type RequestOTP struct {
	creds     domain.CredentialRepository
	persons   domain.PersonRepository
	otps      domain.OTPRepository
	codec     port.OTPCodec
	messaging port.MessagingPort
	limiter   port.RateLimiter
	policy    OTPPolicy
	now       func() time.Time
}

// NewRequestOTP merakit alur penerbitan OTP. now opsional (nil → time.Now).
func NewRequestOTP(
	creds domain.CredentialRepository,
	persons domain.PersonRepository,
	otps domain.OTPRepository,
	codec port.OTPCodec,
	messaging port.MessagingPort,
	limiter port.RateLimiter,
	policy OTPPolicy,
	now func() time.Time,
) *RequestOTP {
	if now == nil {
		now = time.Now
	}
	return &RequestOTP{
		creds: creds, persons: persons, otps: otps, codec: codec,
		messaging: messaging, limiter: limiter, policy: policy, now: now,
	}
}

// RequestOTPInput DTO masuk dari portal publik.
type RequestOTPInput struct {
	CredType  domain.CredType // email | no_hp
	CredValue string
}

// Execute memvalidasi jenis kanal, menegakkan rate limit, lalu (bila credential dikenal & person
// aktif) membuat OTP, menyimpannya, dan mengirim kodenya. Lihat catatan enumeration-resistance.
func (uc *RequestOTP) Execute(ctx context.Context, in RequestOTPInput) error {
	if !otpCredTypes[in.CredType] {
		// Jenis kanal salah (mis. NIP/NIK) = penyalahgunaan jalur, bukan info spesifik-akun.
		return errInvalidCredential()
	}

	// Rate limit penerbitan per kredensial — sebelum lookup (cegah flooding & probing-by-rate).
	allowed, err := uc.limiter.Allow(ctx, otpRequestKey(in.CredType, in.CredValue),
		uc.policy.RequestLimit, uc.policy.RequestWindow)
	if err != nil {
		return err // fail-closed: aksi tak dilanjutkan (500)
	}
	if !allowed {
		return errTooManyOTP()
	}

	cred, err := uc.creds.FindByTypeValue(ctx, in.CredType, in.CredValue)
	if err != nil {
		if isNotFound(err) {
			return nil // tak dikenal → diam (enumeration-resistant), tak ada yang dikirim
		}
		return err
	}
	person, err := uc.persons.FindByID(ctx, cred.PersonID)
	if err != nil {
		if isNotFound(err) {
			return nil
		}
		return err
	}
	if !person.IsActive {
		return nil // person non-aktif → diam
	}

	code, hash, err := uc.codec.Generate()
	if err != nil {
		return err
	}
	now := uc.now()
	otp := &domain.OTP{
		ID:           uuid.New(),
		CredentialID: cred.ID,
		CodeHash:     hash,
		ExpiresAt:    now.Add(uc.policy.TTL),
		CreatedAt:    now,
	}
	if err := uc.otps.Create(ctx, otp); err != nil {
		return err
	}

	if err := uc.send(ctx, in.CredType, in.CredValue, code); err != nil {
		return errOTPSendFailed()
	}
	return nil
}

// send merakit pesan & mengirim lewat kanal yang sesuai. Konten pesan dirakit di sini (use case),
// bukan di port — MessagingPort hanya transport.
func (uc *RequestOTP) send(ctx context.Context, t domain.CredType, value, code string) error {
	body := fmt.Sprintf("Kode OTP Anda: %s. Berlaku %d menit. JANGAN bagikan kode ini kepada siapa pun.",
		code, int(uc.policy.TTL.Minutes()))
	switch t {
	case domain.CredEmail:
		return uc.messaging.SendEmail(ctx, value, "Kode OTP Pamong", body)
	case domain.CredNoHP:
		return uc.messaging.SendSMS(ctx, value, body)
	default:
		return errInvalidCredential() // tak tercapai (cred type sudah divalidasi)
	}
}

// otpRequestKey merakit key rate limiter penerbitan, ber-scope per (jenis kanal, nilai kredensial).
func otpRequestKey(t domain.CredType, value string) string {
	return "otp:request:" + string(t) + ":" + value
}

// isNotFound true bila err adalah core.FrameworkError NOT_FOUND (credential/person tak ada).
func isNotFound(err error) bool {
	var fe *core.FrameworkError
	return errors.As(err, &fe) && fe.Code == "NOT_FOUND"
}
