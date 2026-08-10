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
// terdaftar.
//
// Rate limit BERLAPIS DUA, dan keduanya diperlukan (lihat otpCredRequestKey):
//   - Lapis 1, per nilai mentah, SEBELUM lookup — menjaga enumeration-resistance: nilai yang tak
//     dikenal pun ikut tertahan, sehingga keberadaan akun tak terbaca dari laju.
//   - Lapis 2, per kredensial TER-RESOLVE, SESUDAH lookup — inilah kuota penerbitan yang sebenarnya.
//     Habisnya lapis ini berperilaku sama persis dengan credential tak dikenal (diam, nil), agar
//     tak menjadi orakel keberadaan akun.
type RequestOTP struct {
	creds     domain.CredentialRepository
	persons   domain.PersonRepository
	otps      domain.OTPRepository
	codec     port.OTPCodec
	messaging port.MessagingPort
	limiter   port.RateLimiter
	logger    port.Logger
	policy    OTPPolicy
	now       func() time.Time
}

// NewRequestOTP merakit alur penerbitan OTP. now opsional (nil → time.Now).
//
// logger WAJIB non-nil: sejak kegagalan kirim ditelan demi anti-enumerasi (lihat Execute), log
// adalah SATU-SATUNYA tempat kegagalan itu terlihat. Konstruktor yang menerima nil akan menukar
// orakel keamanan dengan kegagalan yang benar-benar tak terlihat siapa pun.
func NewRequestOTP(
	creds domain.CredentialRepository,
	persons domain.PersonRepository,
	otps domain.OTPRepository,
	codec port.OTPCodec,
	messaging port.MessagingPort,
	limiter port.RateLimiter,
	logger port.Logger,
	policy OTPPolicy,
	now func() time.Time,
) *RequestOTP {
	if now == nil {
		now = time.Now
	}
	if logger == nil {
		panic("usecase.NewRequestOTP: logger nil — kegagalan kirim OTP akan hilang tanpa jejak")
	}
	return &RequestOTP{
		creds: creds, persons: persons, otps: otps, codec: codec, messaging: messaging,
		limiter: limiter, logger: logger, policy: policy, now: now,
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

	// Lapis 1: per nilai mentah, sebelum lookup (probing-by-rate & flooding nilai tak dikenal).
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

	// Lapis 2: kuota penerbitan per kredensial ter-resolve. Diam saat habis — jalur ini HARUS
	// tak bisa dibedakan dari "credential tak dikenal" di atas.
	allowed, err = uc.limiter.Allow(ctx, otpCredRequestKey(cred.ID),
		uc.policy.RequestLimit, uc.policy.RequestWindow)
	if err != nil {
		return err
	}
	if !allowed {
		return nil
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

	// Tujuan kirim diambil dari KREDENSIAL TER-RESOLVE, tak pernah dari nilai permintaan —
	// alasan yang sama persis dengan kunci kuota di atas. Lookup berjalan di atas blind index
	// yang menormalkan lebih dulu, jadi in.CredValue boleh berbeda dari alamat yang sebenarnya
	// terdaftar: "victim@x.id\n" me-resolve ke kredensial "victim@x.id" karena TrimSpace ikut
	// membuang CR/LF. Mengirim ke nilai mentah membuat driver SMTP menolaknya sebagai header
	// injection (errOTPSendFailed → 500) sementara alamat tak dikenal menjawab nil (200) —
	// orakel keberadaan akun, satu probe per target. cred.CredValue adalah alamat kanonik
	// sebagaimana didaftarkan, sehingga jalur ini juga tak lagi patah untuk warga yang
	// menempelkan alamat berspasi.
	// Kegagalan kirim DITELAN (dicatat ke log, respons tetap sukses senyap). Mengembalikannya
	// sebagai 500 menghancurkan enumeration-resistance yang dijaga seluruh fungsi ini: jalur
	// "tak dikenal" menjawab 202 sementara jalur "terdaftar tapi gagal kirim" menjawab 500, jadi
	// selisihnya menjadi orakel keberadaan akun — satu probe per target.
	//
	// Itu bukan bahaya teoretis. Driver produksi satu-satunya (`smtp`) menolak SMS secara
	// PERMANEN, sehingga SETIAP `cred_type=no_hp` yang terdaftar menjawab 500 dan yang tak
	// terdaftar menjawab 202: seluruh basis nomor bisa disaring dengan satu request per nomor.
	// Selama alur ini tak punya handler HTTP hal itu tak terjangkau; PR-W1 memasangnya, jadi
	// penutupannya jatuh tempo di sini. Kanal tanpa driver yang berfungsi tetap masalah
	// konfigurasi tersendiri — REVIEW_BACKLOG A8.
	//
	// ADR-008 §deferred sudah menominasikan "swallow + log" sebagai refinement; ini penerapannya.
	// Harganya disadari: warga yang OTP-nya gagal terkirim melihat 202 dan tak tahu harus menunggu
	// sia-sia. Operator melihatnya di log. Membedakannya di respons = mengembalikan orakel.
	if err := uc.send(ctx, cred.CredType, cred.CredValue, code); err != nil {
		uc.logger.Error(ctx, "pengiriman OTP gagal; respons tetap sukses senyap (anti-enumerasi)",
			port.F("cred_type", string(cred.CredType)),
			port.F("credential_id", cred.ID.String()), // ID, bukan nilai — pengenal tak ke log (ADR-009 §6)
			port.F("err", err.Error()))
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

// otpRequestKey merakit key rate limiter LAPIS 1, ber-scope per (jenis kanal, nilai mentah yang
// DI-HASH). Lapis ini berjalan sebelum lookup, jadi belum ada kredensial untuk dijadikan acuan —
// tugasnya cuma menahan laju nilai yang belum tentu ada. Nilainya di-hash dengan alasan yang sama
// seperti loginRawKey: panjangnya dikendalikan pemanggil anonim, dan ia pengenal (ADR-009 §6).
func otpRequestKey(t domain.CredType, value string) string {
	return "otp:request:" + string(t) + ":" + hashKeyPart(value)
}

// otpCredRequestKey / otpCredVerifyKey merakit key rate limiter LAPIS 2 dari ID kredensial —
// KANONIK by construction, dan itulah intinya.
//
// Nilai mentah TIDAK BOLEH jadi acuan kuota: pencarian kredensial berjalan di atas blind index
// yang menormalkan nilainya lebih dulu (trim untuk semua purpose, case-fold untuk `email` —
// kebijakan framework di infra/crypto). Akibatnya "budi@x.id", "Budi@x.id", dan " budi@x.id "
// menunjuk SATU kredensial yang sama, sementara ketiganya menghasilkan key yang berbeda. Kuota
// yang di-key pada nilai mentah karena itu bisa dilipatgandakan tanpa batas hanya dengan
// mengubah huruf besar/kecil atau menambah spasi — dan yang tersisa cuma cap per-OTP
// (domain.MaxOTPAttempts), yang justru dirancang sebagai SETENGAH dari proteksi.
//
// Menormalkan nilai di sini bukan jalan keluar: itu menyalin tabel kebijakan infra/crypto ke
// lapis use case (yang memang tak boleh menyentuh kripto), menciptakan sumber kebenaran kedua
// yang bisa menyimpang diam-diam. ID kredensial tak punya masalah itu — ia sudah hasil resolusi.
func otpCredRequestKey(credID uuid.UUID) string { return "otp:request:cred:" + credID.String() }

func otpCredVerifyKey(credID uuid.UUID) string { return "otp:verify:cred:" + credID.String() }

// isNotFound true bila err adalah core.FrameworkError NOT_FOUND (credential/person tak ada).
func isNotFound(err error) bool {
	var fe *core.FrameworkError
	return errors.As(err, &fe) && fe.Code == "NOT_FOUND"
}
