package usecase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/google/uuid"

	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/port"
)

// Proteksi brute-force jalur password (PR-W1) — separuh terakhir dari REVIEW_BACKLOG A5.
// Jalur OTP sudah dilindungi sejak PR-2.4.4; jalur password belum, dan selama alur login tak
// punya handler HTTP kelalaian itu tak terjangkau dari luar. PR-W1 memasang handler-nya, jadi
// proteksinya harus mendarat di PR yang sama — kalau tidak, wiring justru menjadikan kelemahan
// yang selama ini dorman sebagai permukaan serang yang nyata.

// LoginPolicy mengumpulkan ambang proteksi brute-force jalur password. Struct ter-inject (bukan
// const tersebar) mengikuti pola OTPPolicy: memindahkannya ke core/config kelak = isi struct saat
// wiring, tanpa mengubah signature use case.
type LoginPolicy struct {
	Limit  int // maksimum percobaan per Window per kredensial
	Window time.Duration
}

// DefaultLoginPolicy mengembalikan ambang yang longgar untuk manusia, ketat untuk mesin:
// 10 percobaan per 15 menit. Kuota dihitung per KREDENSIAL, bukan per IP — satu penyerang di
// banyak IP tetap menabrak kuota yang sama, dan satu kantor di balik satu NAT tidak saling
// mengunci (alasan yang sama dengan port.RateLimiter §"Pola pemakaian").
func DefaultLoginPolicy() LoginPolicy {
	return LoginPolicy{Limit: 10, Window: 15 * time.Minute}
}

// passwordAuthenticator memverifikasi (cred_type, cred_value, password) → person aktif. SATU
// implementasi dipakai jalur employee & citizen: kalau aturannya disalin, salah satu salinan akan
// menyimpang diam-diam saat yang lain diperbaiki (doktrin yang sama dengan crypto.FieldSealer).
//
// Rate limit BERLAPIS DUA, meniru RequestOTP — dan kedua lapis punya tugas berbeda:
//   - Lapis 1, per nilai MENTAH, SEBELUM lookup: menahan laju nilai yang belum tentu ada,
//     sehingga keberadaan akun tak terbaca dari perbedaan laju.
//   - Lapis 2, per kredensial TER-RESOLVE, SESUDAH lookup: kuota yang sebenarnya. Nilai mentah
//     tak boleh jadi acuan karena lookup berjalan di atas blind index yang menormalkan lebih dulu
//     (trim; case-fold untuk email) — "budi@x.id", "Budi@x.id", dan " budi@x.id " adalah SATU
//     kredensial dengan tiga key mentah, jadi kuota ber-key mentah bisa dilipatgandakan tanpa
//     batas. Ini pelajaran REVIEW_BACKLOG A7 yang sudah dibayar sekali di jalur OTP.
type passwordAuthenticator struct {
	creds     domain.CredentialRepository
	persons   domain.PersonRepository
	passwords port.PasswordVerifier
	limiter   port.RateLimiter
	policy    LoginPolicy
}

// authenticate mengembalikan person yang aktif bila kredensial & password cocok. SEMUA kegagalan
// spesifik-akun dipetakan ke errInvalidCredential (401 seragam) agar tak membocorkan tahap mana
// yang gagal.
func (a passwordAuthenticator) authenticate(ctx context.Context, t domain.CredType, value, password string) (*domain.Person, error) {
	// Lapis 1 — berjalan untuk nilai dikenal maupun tidak, jadi 429 di sini bukan orakel.
	allowed, err := a.limiter.Allow(ctx, loginRawKey(t, value), a.policy.Limit, a.policy.Window)
	if err != nil {
		return nil, err // fail-closed: percobaan tak dilanjutkan (500)
	}
	if !allowed {
		return nil, errTooManyLogin()
	}

	cred, err := a.creds.FindByTypeValue(ctx, t, value)
	if err != nil {
		return nil, errInvalidCredential()
	}

	// Lapis 2 — habisnya kuota menjawab 401 SERAGAM, bukan 429. Lapis ini hanya tercapai untuk
	// kredensial yang BENAR-BENAR ADA, jadi 429 di sini akan memberi tahu penyerang bahwa
	// nilai yang ia tebak terdaftar — orakel keberadaan akun satu-probe-per-target. Cermin dari
	// "diam saat kuota habis" di RequestOTP, disesuaikan dengan jalur yang responsnya 401.
	allowed, err = a.limiter.Allow(ctx, loginCredKey(cred.ID), a.policy.Limit, a.policy.Window)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, errInvalidCredential()
	}

	if cred.SecretHash == "" {
		// Credential tanpa password (SSO/OTP-only) tak bisa login lewat jalur password.
		return nil, errInvalidCredential()
	}
	if err := a.passwords.Verify(cred.SecretHash, password); err != nil {
		return nil, errInvalidCredential()
	}
	person, err := a.persons.FindByID(ctx, cred.PersonID)
	if err != nil || !person.IsActive {
		return nil, errInvalidCredential()
	}
	return person, nil
}

// loginRawKey / loginCredKey merakit key kedua lapis. Prefix "login:" memisahkan namespace dari
// pemakaian limiter lain yang berbagi store (mis. "otp:", "rl:req:").
//
// Nilai mentah masuk sebagai HASH, bukan apa adanya. Dua sebabnya, keduanya konkret:
//   - **Panjangnya dikendalikan penyerang.** Rute /auth/* dilayani tanpa otentikasi dan body
//     dibatasi 64 KiB, jadi key mentah bisa sepanjang itu — dikalikan jumlah nilai unik yang
//     dikirim, ia menjadi jalur menumbuhkan memori limiter dengan murah.
//   - **Nilai mentah adalah pengenal** (NIP/NIK/email/no_hp, kelas personal_id). Key limiter
//     mengalir ke store yang kelak Redis — mengirim pengenal apa adadanya ke sana adalah jalur
//     samping ADR-009 §6 yang sama dengan log & pesan error.
//
// Hash bukan rahasia (nilai bisa ditebak dari ruang yang kecil); tugasnya membatasi panjang &
// menghindari pengenal polos, bukan menyembunyikan. Determinisme itulah yang dibutuhkan limiter.
//
// KETERBATASAN YANG DISENGAJA: port.RateLimiter hanya menghitung (tak punya Reset), jadi login
// yang BERHASIL pun memakai kuota. Dengan ambang default (10/15 menit) itu tak mengganggu manusia,
// dan menambah Reset ke port demi kenyamanan akan memberi jalur "nolkan penghitung" yang justru
// menarik untuk disalahgunakan. Bila kelak ambang terasa sempit, naikkan Limit — jangan tambahkan
// Reset tanpa ADR.
func loginRawKey(t domain.CredType, value string) string {
	return "login:raw:" + string(t) + ":" + hashKeyPart(value)
}

// hashKeyPart memetakan nilai sembarang-panjang ke 32 hex char yang stabil.
func hashKeyPart(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:16])
}

func loginCredKey(credID uuid.UUID) string { return "login:cred:" + credID.String() }

// errTooManyLogin dipakai saat lapis 1 terlampaui → HTTP 429.
func errTooManyLogin() error {
	return core.ErrTooManyRequests("terlalu banyak percobaan login, silakan coba lagi nanti")
}
