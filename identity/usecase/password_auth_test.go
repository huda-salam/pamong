package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/identity/usecase"
)

// Proteksi brute-force jalur password (PR-W1). Sebelum PR ini alur login tak punya handler HTTP,
// jadi ketiadaan proteksi tak terjangkau dari luar; begitu /auth/login terpasang, ia menjadi
// permukaan serang nyata. Test di sini mengunci DUA hal berbeda: bahwa kuota ditegakkan, dan
// bahwa penegakannya tidak menciptakan orakel keberadaan akun.

// credOf mengambil kredensial yang diseed seedEmployee (yang hanya mengembalikan person &
// employment) — dibutuhkan untuk menyusun key limiter LAPIS 2 yang ber-ID kredensial.
func credOf(t *testing.T, fx *loginFixture, ct domain.CredType, value string) *domain.Credential {
	t.Helper()
	c, err := fx.creds.FindByTypeValue(context.Background(), ct, value)
	if err != nil {
		t.Fatalf("kredensial seed tak ditemukan: %v", err)
	}
	return c
}

// Lapis 1 (nilai MENTAH, sebelum lookup) menjawab 429. Aman: lapis ini berjalan untuk nilai yang
// terdaftar maupun tidak, jadi 429-nya tak menceritakan apa pun tentang keberadaan akun.
func TestLoginEmployee_LapisMentahHabis_429(t *testing.T) {
	fx := newLoginFixture()
	_, emp := fx.seedEmployee(t)
	fx.assignTenant(emp, "pemkot-surabaya", true, true)
	fx.limiter.allowN[usecase.LoginRawKeyForTest(domain.CredNIP, emp.NIP)] = 0

	_, err := fx.loginEmployee().Execute(context.Background(), usecase.LoginEmployeeInput{
		CredType: domain.CredNIP, CredValue: emp.NIP, Password: "rahasia",
	})
	assertTooManyRequests(t, err)
}

// Lapis 2 (kredensial TER-RESOLVE, sesudah lookup) menjawab 401 SERAGAM — BUKAN 429.
//
// Inilah test yang menjaga properti terpenting di file ini. Lapis 2 hanya tercapai bila lookup
// berhasil, jadi status yang berbeda dari jalur "kredensial tak dikenal" akan memberi tahu
// penyerang bahwa nilai yang ia tebak terdaftar — orakel keberadaan akun satu-probe-per-target.
// "Perbaikan" yang tampak masuk akal (mengembalikan 429 agar pengguna sah tahu ia terkena limit)
// membuat test ini gagal, dan memang harus.
func TestLoginEmployee_KuotaKredensialHabis_401BukanOrakel(t *testing.T) {
	fx := newLoginFixture()
	_, emp := fx.seedEmployee(t)
	fx.assignTenant(emp, "pemkot-surabaya", true, true)
	cred := credOf(t, fx, domain.CredNIP, emp.NIP)
	fx.limiter.allowN[usecase.LoginCredKeyForTest(cred.ID)] = 0

	_, err := fx.loginEmployee().Execute(context.Background(), usecase.LoginEmployeeInput{
		CredType: domain.CredNIP, CredValue: emp.NIP, Password: "rahasia",
	})
	assertUnauthorized(t, err)

	// Bandingkan dengan kredensial yang memang tak ada: keduanya harus tak bisa dibedakan.
	_, errTakDikenal := fx.loginEmployee().Execute(context.Background(), usecase.LoginEmployeeInput{
		CredType: domain.CredNIP, CredValue: "199001012015011999", Password: "rahasia",
	})
	assertUnauthorized(t, errTakDikenal)
	if err.Error() != errTakDikenal.Error() {
		t.Fatalf("respons kuota-habis (%q) harus identik dengan kredensial tak dikenal (%q) — "+
			"perbedaannya menjadi orakel keberadaan akun", err, errTakDikenal)
	}
}

// Kuota di-key pada ID KREDENSIAL, bukan nilai mentah. Ini pelajaran REVIEW_BACKLOG A7 yang sudah
// dibayar sekali di jalur OTP: lookup berjalan di atas blind index yang menormalkan nilai lebih
// dulu, jadi kuota ber-key mentah bisa dilipatgandakan hanya dengan mengubah huruf besar/kecil.
// Fake repo di sini mencocokkan persis (bukan blind index), sehingga yang bisa & perlu dikunci
// adalah BENTUK key-nya.
func TestLoginEmployee_KuotaDikunciPadaIDKredensial(t *testing.T) {
	fx := newLoginFixture()
	_, emp := fx.seedEmployee(t)
	fx.assignTenant(emp, "pemkot-surabaya", true, true)
	cred := credOf(t, fx, domain.CredNIP, emp.NIP)

	if _, err := fx.loginEmployee().Execute(context.Background(), usecase.LoginEmployeeInput{
		CredType: domain.CredNIP, CredValue: emp.NIP, Password: "rahasia",
	}); err != nil {
		t.Fatalf("login seharusnya berhasil: %v", err)
	}

	if fx.limiter.calls[usecase.LoginCredKeyForTest(cred.ID)] == 0 {
		t.Fatal("lapis 2 tak pernah dipanggil dengan key ber-ID kredensial — " +
			"kuota kembali ber-key nilai mentah (regresi A7)")
	}
}

// Store limiter error → fail-closed: percobaan TIDAK dilanjutkan (bukan lolos begitu saja).
func TestLoginEmployee_LimiterError_FailClosed(t *testing.T) {
	fx := newLoginFixture()
	_, emp := fx.seedEmployee(t)
	fx.assignTenant(emp, "pemkot-surabaya", true, true)
	fx.limiter.err = errors.New("store down")

	res, err := fx.loginEmployee().Execute(context.Background(), usecase.LoginEmployeeInput{
		CredType: domain.CredNIP, CredValue: emp.NIP, Password: "rahasia",
	})
	if err == nil {
		t.Fatalf("limiter error harus fail-closed, malah sukses: %+v", res)
	}
}

// Jalur citizen memakai passwordAuthenticator yang SAMA — portal publik justru yang paling
// terekspos, jadi ia tak boleh mendapat perlakuan lebih longgar. Test ini yang akan gagal bila
// suatu saat salah satu jalur "dioptimalkan" dengan verifikasi sendiri.
func TestLoginCitizen_KuotaKredensialHabis_401(t *testing.T) {
	fx := newLoginFixture()
	person, _ := fx.seedEmployee(t)
	fx.creds.add(&domain.Credential{
		ID: uuid.New(), PersonID: person.ID, CredType: domain.CredEmail,
		CredValue: "budi@example.com", SecretHash: "h:rahasia",
	})
	cred := credOf(t, fx, domain.CredEmail, "budi@example.com")
	fx.limiter.allowN[usecase.LoginCredKeyForTest(cred.ID)] = 0

	_, err := fx.loginCitizen().Execute(context.Background(), usecase.LoginCitizenInput{
		CredType: domain.CredEmail, CredValue: "budi@example.com", Password: "rahasia",
	})
	assertUnauthorized(t, err)
}

// --- Biaya kerja seragam (temuan /security-review PR-W1) ---
//
// Respons 401 yang seragam saja tidak cukup: sebelum perbaikan ini, jalur "kredensial tak ada"
// pulang tanpa pernah menyentuh bcrypt (~2-5 ms) sementara "kredensial ada, password salah"
// membayar bcrypt cost 10 (~50-100 ms). Selisihnya terbaca dengan stopwatch — satu request per
// target sudah memastikan sebuah NIP/NIK/email/no_hp terdaftar, dan itu mengembalikan orakel
// keberadaan akun yang sudah dibayar mahal untuk ditutup (body 401 seragam, 202 senyap di
// RequestOTP, kuota lapis 2 menjawab 401 alih-alih 429).
//
// Yang diuji STRUKTURAL, bukan temporal: berapa kali `Verify` dipanggil. Test berbasis waktu
// flaky di CI dan tak membuktikan apa-apa; hitungan panggilan deterministik dan persis properti
// yang ingin dijaga — SETIAP jalur kegagalan spesifik-akun membayar tepat satu verifikasi.

// TestPasswordAuth_BiayaKerjaSeragam menuntut Verify dipanggil TEPAT SEKALI di seluruh jalur,
// gagal maupun berhasil. Jalur yang tak punya hash asli memverifikasi hash tiruan ber-cost sama.
func TestPasswordAuth_BiayaKerjaSeragam(t *testing.T) {
	cases := []struct {
		nama  string
		jalan func(t *testing.T, fx *loginFixture)
	}{
		{"kredensial tak ditemukan", func(t *testing.T, fx *loginFixture) {
			_, err := fx.loginEmployee().Execute(context.Background(), usecase.LoginEmployeeInput{
				CredType: domain.CredNIP, CredValue: "199001012015011999", Password: "rahasia",
			})
			assertUnauthorized(t, err)
		}},
		{"secret hash kosong (SSO/OTP-only)", func(t *testing.T, fx *loginFixture) {
			person, _ := fx.seedEmployee(t)
			fx.creds.add(&domain.Credential{
				ID: uuid.New(), PersonID: person.ID, CredType: domain.CredNIK,
				CredValue: person.NIK, SecretHash: "",
			})
			_, err := fx.loginEmployee().Execute(context.Background(), usecase.LoginEmployeeInput{
				CredType: domain.CredNIK, CredValue: person.NIK, Password: "rahasia",
			})
			assertUnauthorized(t, err)
		}},
		{"kuota lapis 2 habis", func(t *testing.T, fx *loginFixture) {
			_, emp := fx.seedEmployee(t)
			cred := credOf(t, fx, domain.CredNIP, emp.NIP)
			fx.limiter.allowN[usecase.LoginCredKeyForTest(cred.ID)] = 0
			_, err := fx.loginEmployee().Execute(context.Background(), usecase.LoginEmployeeInput{
				CredType: domain.CredNIP, CredValue: emp.NIP, Password: "rahasia",
			})
			assertUnauthorized(t, err)
		}},
		{"password salah", func(t *testing.T, fx *loginFixture) {
			_, emp := fx.seedEmployee(t)
			_, err := fx.loginEmployee().Execute(context.Background(), usecase.LoginEmployeeInput{
				CredType: domain.CredNIP, CredValue: emp.NIP, Password: "salah",
			})
			assertUnauthorized(t, err)
		}},
		{"password benar", func(t *testing.T, fx *loginFixture) {
			_, emp := fx.seedEmployee(t)
			fx.assignTenant(emp, "pemkot-surabaya", true, true)
			if _, err := fx.loginEmployee().Execute(context.Background(), usecase.LoginEmployeeInput{
				CredType: domain.CredNIP, CredValue: emp.NIP, Password: "rahasia",
			}); err != nil {
				t.Fatalf("login seharusnya berhasil: %v", err)
			}
		}},
		{"citizen, kredensial tak ditemukan", func(t *testing.T, fx *loginFixture) {
			_, err := fx.loginCitizen().Execute(context.Background(), usecase.LoginCitizenInput{
				CredType: domain.CredEmail, CredValue: "bukan-siapa-siapa@example.com", Password: "rahasia",
			})
			assertUnauthorized(t, err)
		}},
	}

	for _, c := range cases {
		t.Run(c.nama, func(t *testing.T) {
			fx := newLoginFixture()
			c.jalan(t, fx)
			if fx.passwords.verifyCalls != 1 {
				t.Fatalf("Verify dipanggil %d kali, harus tepat 1 — jalur ini punya biaya kerja "+
					"berbeda dari jalur lain, jadi keberadaan akun terbaca dari lama respons "+
					"meski body 401-nya seragam", fx.passwords.verifyCalls)
			}
		})
	}
}

// Hash tiruan berasal dari port.PasswordVerifier yang SAMA dengan yang memverifikasi hash asli,
// bukan konstanta yang ditulis tangan — sehingga cost-nya otomatis mengikuti bila bcrypt dinaikkan
// kelak. Test ini mengunci sambungan itu: fake mengembalikan Hash = "h:"+plain, jadi Verify pada
// jalur "kredensial tak ada" harus menerima hash BUATAN FAKE, bukan string asing.
func TestPasswordAuth_HashTiruanDariVerifierYangSama(t *testing.T) {
	fx := newLoginFixture()
	spy := &spyPasswords{fakePasswords: fx.passwords}

	uc := usecase.NewLoginCitizen(fx.creds, fx.persons, spy, fx.issuer, fx.limiter,
		usecase.DefaultLoginPolicy())
	_, err := uc.Execute(context.Background(), usecase.LoginCitizenInput{
		CredType: domain.CredEmail, CredValue: "tak-ada@example.com", Password: "rahasia",
	})
	assertUnauthorized(t, err)

	if len(spy.hashed) != 1 {
		t.Fatalf("Hash harus dipanggil sekali saat konstruksi, dapat %d", len(spy.hashed))
	}
	if len(spy.verified) != 1 {
		t.Fatalf("Verify harus dipanggil sekali, dapat %d", len(spy.verified))
	}
	if want := "h:" + spy.hashed[0]; spy.verified[0] != want {
		t.Fatalf("verifikasi tiruan memakai hash %q, harusnya hasil Hash verifier (%q) — "+
			"hash konstan akan tertinggal cost-nya saat bcrypt dinaikkan, dan celah timing "+
			"terbuka lagi tanpa satu pun test mengeluh", spy.verified[0], want)
	}
}

// spyPasswords merekam argumen Hash & Verify di atas fakePasswords.
type spyPasswords struct {
	*fakePasswords
	hashed   []string // plaintext yang di-hash
	verified []string // hash yang diverifikasi
}

func (s *spyPasswords) Hash(plain string) (string, error) {
	s.hashed = append(s.hashed, plain)
	return s.fakePasswords.Hash(plain)
}

func (s *spyPasswords) Verify(hash, plain string) error {
	s.verified = append(s.verified, hash)
	return s.fakePasswords.Verify(hash, plain)
}
