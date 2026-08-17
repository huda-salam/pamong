package main

import (
	"context"

	"github.com/google/uuid"

	identityauth "github.com/huda-salam/pamong/identity/adapter/auth"
	identitydb "github.com/huda-salam/pamong/identity/adapter/db"
	identityhttp "github.com/huda-salam/pamong/identity/adapter/http"
	"github.com/huda-salam/pamong/identity/usecase"
	"github.com/huda-salam/pamong/infra/db"
	"github.com/huda-salam/pamong/port"
	tenantroledb "github.com/huda-salam/pamong/tenantrole/adapter/db"
)

// wireAuth merakit alur login (PR-W1) — sisi PENERBIT token, pelengkap sisi verifikasi yang
// sudah ter-wire sejak PR-5.1.2. Sebelum ini `LoginEmployee`/`SelectTenant`/`LoginCitizen`/
// `RequestOTP`/`VerifyOTP` lengkap & teruji tapi tak punya satu pun pemanggil produksi, sementara
// `RequireAuth` memagari seluruh rute bisnis: server yang di-boot `run()` tak bisa dipakai klien
// mana pun karena token hanya bisa dicetak di luar sistem.
//
// Beberapa pilihan perakitan yang tidak sembarang:
//
//   - **Repo TANPA dekorator audit.** Alur login hanya MEMBACA identity (kecuali penerbitan OTP
//     yang punya tabelnya sendiri), dan dekorator audit menuntut aktor — yang justru belum ada
//     saat login (chicken-and-egg yang sama dengan `identity_sync.go`). Jejak "siapa mencoba
//     login" adalah kebutuhan berbeda (log/telemetry keamanan), bukan audit mutasi entity.
//   - **`cryptoSvc` yang sama dengan sisa proses.** Pengenal identity tersimpan `_enc`+`_bidx`
//     ber-realm SENTRAL (ADR-017); repo yang dirakit tanpa CryptoPort menolak berdiri, dan yang
//     dirakit dengan realm keliru tidak gagal — ia hanya tak pernah menemukan siapa pun.
//   - **`verifyGate` DITERIMA, bukan dibuat di sini.** Ia membatasi concurrency bcrypt untuk
//     SELURUH proses, jadi ia dirakit sekali di run() dan dibagi dengan permukaan bcrypt lain
//     (kini juga `CreateCredential` di wireAdminIdentity). Gerbang per fungsi wiring akan
//     melipatgandakan batas yang justru ingin ditegakkan.
//   - **`limiter` yang sama dengan middleware rate limit.** Key-nya ber-namespace berbeda
//     ("login:", "otp:", "rl:req:"), jadi berbagi store aman dan justru diinginkan: satu tempat
//     untuk ditukar ke Redis saat multi-instance (titik ekstensi #1).
func wireAuth(
	identityPool *db.Pool,
	connMgr *db.TenantConnManager,
	cryptoSvc port.CryptoPort,
	issuer port.TokenIssuer,
	limiter port.RateLimiter,
	logger port.Logger,
	sender port.MessagingPort,
	verifyGate *usecase.VerifyGate,
) (*identityhttp.Handler, error) {
	creds, err := identitydb.NewCredentialRepo(identityPool, cryptoSvc)
	if err != nil {
		return nil, err
	}
	persons, err := identitydb.NewPersonRepo(identityPool, cryptoSvc)
	if err != nil {
		return nil, err
	}
	employments, err := identitydb.NewEmploymentRepo(identityPool, cryptoSvc)
	if err != nil {
		return nil, err
	}
	assigns := identitydb.NewTenantAssignmentRepo(identityPool)
	tenants := identitydb.NewTenantRepo(identityPool)
	passwords := identityauth.NewBcryptVerifier()
	central := identitydb.NewCentralRoleResolver(identityPool)
	tenantRoles := tenantRoleResolver{connMgr: connMgr}

	loginPolicy := usecase.DefaultLoginPolicy()
	otpPolicy := usecase.DefaultOTPPolicy()
	otps := identitydb.NewOTPRepo(identityPool)
	otpCodec := identityauth.NewOTPCodec()

	return identityhttp.NewHandler(
		usecase.NewLoginEmployee(creds, persons, employments, assigns, tenants, passwords,
			central, tenantRoles, issuer, limiter, loginPolicy, verifyGate),
		usecase.NewSelectTenant(employments, assigns, tenants, central, tenantRoles, issuer),
		usecase.NewLoginCitizen(creds, persons, passwords, issuer, limiter, loginPolicy, verifyGate),
		usecase.NewRequestOTP(creds, persons, otps, otpCodec, sender, limiter, logger, otpPolicy, nil),
		usecase.NewVerifyOTP(creds, persons, otps, otpCodec, limiter, issuer, otpPolicy, nil),
	), nil
}

// tenantRoleResolver memenuhi usecase.TenantRoleResolver dengan memilih pool DB tenant yang
// diminta lebih dulu, lalu mendelegasikan ke resolver tenantrole yang TIDAK menerima tenantID
// (ia hanya melihat gov.* pada koneksi tempat ia berdiri — itulah jaminan struktural "role tenant
// hanya berlaku di tenant-nya"). Pemilihan pool adalah tugas composition root, bukan tugas
// resolver: use case cukup tahu ada seam bernama TenantRoleResolver.
type tenantRoleResolver struct {
	connMgr *db.TenantConnManager
}

func (r tenantRoleResolver) EffectiveRoles(ctx context.Context, personID uuid.UUID, tenantID string) ([]string, error) {
	pool, err := r.connMgr.Tenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return tenantroledb.NewTenantRoleResolver(pool).EffectiveRoles(ctx, personID)
}
