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
	// dummyHash adalah hash ber-cost SAMA dengan hash asli, dipakai untuk verifikasi tiruan
	// pada jalur yang tak punya hash asli — lihat newPasswordAuthenticator & authenticate.
	dummyHash string
}

// dummyPassword adalah plaintext yang di-hash sekali saat konstruksi. Nilainya tidak rahasia
// (ia bukan kredensial siapa pun) dan tak perlu rahasia: kegagalan tetap dipagari flag `eligible`
// di authenticate, sehingga seseorang yang mengirim persis string ini tetap ditolak.
const dummyPassword = "pamong:hash-tiruan-penyeragam-biaya-login"

// newPasswordAuthenticator merakit authenticator dan menyiapkan hash tiruan SEKALI di muka.
//
// Hash tiruan dibuat lewat port.PasswordVerifier yang sama dengan yang memverifikasi hash asli —
// bukan konstanta hash yang ditulis tangan. Bedanya menentukan: dengan konstanta, menaikkan cost
// bcrypt kelak akan diam-diam membuat jalur tiruan lebih murah daripada jalur asli dan membuka
// kembali orakel timing yang ditutup di sini, tanpa satu pun test atau linter yang mengeluh.
// Lewat verifier, cost mengikuti dengan sendirinya.
//
// Kegagalan Hash → panic. Ini konstruksi (boot/wiring), bukan runtime request: bcrypt hanya gagal
// pada cost tak sah atau plaintext >72 byte, dua-duanya bug kode yang harus terlihat seketika.
// Alternatifnya — menyimpan hash kosong — membuat verifikasi tiruan selesai instan dan mematikan
// kontrol ini tanpa jejak apa pun. Cermin `NewRequestOTP` yang panic saat logger nil.
func newPasswordAuthenticator(
	creds domain.CredentialRepository,
	persons domain.PersonRepository,
	passwords port.PasswordVerifier,
	limiter port.RateLimiter,
	policy LoginPolicy,
) passwordAuthenticator {
	dummy, err := passwords.Hash(dummyPassword)
	if err != nil {
		panic("usecase.newPasswordAuthenticator: gagal menyiapkan hash tiruan — " +
			"tanpa hash tiruan, kegagalan login menjadi orakel keberadaan akun lewat timing: " + err.Error())
	}
	return passwordAuthenticator{
		creds: creds, persons: persons, passwords: passwords,
		limiter: limiter, policy: policy, dummyHash: dummy,
	}
}

// authenticate mengembalikan person yang aktif bila kredensial & password cocok. SEMUA kegagalan
// spesifik-akun dipetakan ke errInvalidCredential (401 seragam) agar tak membocorkan tahap mana
// yang gagal — DAN menempuh biaya kerja yang sama (satu verifikasi bcrypt).
//
// BIAYA KERJA SERAGAM (temuan /security-review PR-W1). Body 401 sudah seragam, kontrak RequestOTP
// sudah senyap, dan habisnya kuota lapis 2 sudah sengaja menjawab 401 alih-alih 429 — ketiganya
// dibayar khusus untuk menutup orakel keberadaan akun. Semuanya sia-sia bila jalur "kredensial tak
// ada" (~2-5 ms: blind index + indexed read) bisa dibedakan dari jalur "kredensial ada, password
// salah" (~50-100 ms: bcrypt cost 10) hanya dengan stopwatch: satu request per target sudah cukup
// memastikan sebuah NIK/NIP/email/no_hp terdaftar — pengenal kelas personal_id (ADR-009). Rate
// limit tidak menutupnya; lapis 1 memberi 10 percobaan per nilai per 15 menit sementara penyerang
// cuma butuh 1-3 sampel.
//
// Karena itu SEMUA jalur kegagalan spesifik-akun melewati SATU titik panggil Verify di bawah —
// dengan hash asli bila ada, hash tiruan bila tidak. Bentuk "satu titik panggil" dipilih dengan
// sengaja alih-alih menyelipkan verifikasi tiruan sebelum tiap `return`: yang terakhir mengundang
// early-return baru yang lupa membayarnya, persis cacat yang sedang diperbaiki. Diuji secara
// STRUKTURAL (hitungan panggilan Verify = 1 di tiap jalur), bukan dengan mengukur waktu — test
// berbasis waktu flaky di CI dan tak membuktikan properti yang dimaksud.
func (a passwordAuthenticator) authenticate(ctx context.Context, t domain.CredType, value, password string) (*domain.Person, error) {
	// Lapis 1 — berjalan untuk nilai dikenal maupun tidak, jadi 429 di sini bukan orakel.
	allowed, err := a.limiter.Allow(ctx, loginRawKey(t, value), a.policy.Limit, a.policy.Window)
	if err != nil {
		return nil, err // fail-closed: percobaan tak dilanjutkan (500)
	}
	if !allowed {
		return nil, errTooManyLogin()
	}

	// Mulai di sini, jangan pernah `return errInvalidCredential()` lebih awal: setiap kegagalan
	// spesifik-akun harus jatuh ke titik Verify tunggal di bawah. hash & eligible adalah caranya
	// — hash menentukan APA yang diverifikasi, eligible menentukan apakah hasilnya boleh dipercaya.
	hash, eligible := a.dummyHash, false

	cred, credErr := a.creds.FindByTypeValue(ctx, t, value)
	if credErr == nil {
		// Lapis 2 — habisnya kuota menjawab 401 SERAGAM, bukan 429. Lapis ini hanya tercapai untuk
		// kredensial yang BENAR-BENAR ADA, jadi 429 di sini akan memberi tahu penyerang bahwa
		// nilai yang ia tebak terdaftar — orakel keberadaan akun satu-probe-per-target. Cermin dari
		// "diam saat kuota habis" di RequestOTP, disesuaikan dengan jalur yang responsnya 401.
		allowed, err = a.limiter.Allow(ctx, loginCredKey(cred.ID), a.policy.Limit, a.policy.Window)
		if err != nil {
			// Error store → 500 di kedua lapis, untuk nilai dikenal maupun tidak (lapis 1 memakai
			// store yang sama & gagal lebih dulu), jadi bukan orakel — dan status yang berbeda
			// sudah membedakannya tanpa perlu timing.
			return nil, err
		}
		// Kuota habis & credential tanpa password (SSO/OTP-only) TIDAK berhenti di sini: keduanya
		// jatuh ke Verify tiruan, sama seperti kredensial yang tak ada.
		if allowed && cred.SecretHash != "" {
			hash, eligible = cred.SecretHash, true
		}
	}

	// SATU-SATUNYA titik panggil Verify di alur ini. `!eligible` menjaga kasus tepi hash tiruan:
	// password yang kebetulan sama dengan dummyPassword akan lolos Verify, dan tetap harus ditolak.
	if verifyErr := a.passwords.Verify(hash, password); verifyErr != nil || !eligible {
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
