package usecase_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

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
		usecase.DefaultLoginPolicy(), fx.gate)
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

// --- Gerbang concurrency bcrypt (temuan /code-review atas perbaikan A11) ---
//
// Penyeragaman biaya kerja menutup orakel timing DENGAN CARA membuat setiap percobaan membayar
// bcrypt — termasuk nilai yang tak terdaftar, yang sebelumnya berhenti murah di lookup. Itu
// mengalikan biaya per-request anonim 20-50×, dan lapis 1 tak membendungnya karena ia ber-key
// nilai MENTAH: penyerang yang mengirim nilai acak berbeda tiap request tak pernah menyentuh kuota
// mana pun. Tanpa batas concurrency, `/auth/*` yang memang dilayani tanpa otentikasi menjadi jalur
// menjenuhkan CPU seluruh proses — bukan cuma login.

// blockingPasswords menahan Verify PERTAMA sampai dilepas, agar kejenuhan gerbang bisa diuji tanpa
// sleep. HANYA yang pertama: panggilan berikutnya lewat begitu saja, supaya test GAGAL (bukan
// menggantung) bila gerbangnya suatu saat dilewati — test yang hang menyandera CI alih-alih
// melaporkan cacat.
type blockingPasswords struct {
	*fakePasswords
	mu       sync.Mutex
	terpakai bool
	masuk    chan struct{} // diisi saat Verify pertama mulai
	lepas    chan struct{} // ditunggu Verify pertama sebelum kembali
}

func (b *blockingPasswords) Verify(hash, plain string) error {
	b.mu.Lock()
	menahan := !b.terpakai
	b.terpakai = true
	b.mu.Unlock()
	if menahan {
		b.masuk <- struct{}{}
		<-b.lepas
	}
	return b.fakePasswords.Verify(hash, plain)
}

// Saat seluruh slot terpakai dan waktu tunggu habis, percobaan berikutnya dijawab 429 — BUKAN 401.
// Ini benar justru karena kejenuhan adalah keadaan PROSES: ia sama untuk kredensial yang ada maupun
// tidak, jadi tak menceritakan apa pun tentang keberadaan akun (beda dari kuota lapis 2, yang hanya
// tercapai untuk kredensial nyata dan karena itu wajib 401).
func TestVerifyGate_Jenuh_429BukanOrakel(t *testing.T) {
	fx := newLoginFixture()
	_, emp := fx.seedEmployee(t)
	blocker := &blockingPasswords{
		fakePasswords: fx.passwords,
		masuk:         make(chan struct{}),
		lepas:         make(chan struct{}),
	}
	// Satu slot, tanpa waktu tunggu berarti: percobaan kedua harus langsung menyerah.
	gate := usecase.NewVerifyGate(1, time.Millisecond)
	uc := usecase.NewLoginCitizen(fx.creds, fx.persons, blocker, fx.issuer, fx.limiter,
		usecase.DefaultLoginPolicy(), gate)
	ucEmp := usecase.NewLoginEmployee(fx.creds, fx.persons, fx.emps, fx.assigns, fx.tenants,
		blocker, fx.central, fx.tenantRoles, fx.issuer, fx.limiter, usecase.DefaultLoginPolicy(), gate)

	fx.creds.add(&domain.Credential{
		ID: uuid.New(), PersonID: uuid.New(), CredType: domain.CredEmail,
		CredValue: "budi@example.com", SecretHash: "h:rahasia",
	})

	selesai := make(chan struct{})
	go func() {
		defer close(selesai)
		_, _ = uc.Execute(context.Background(), usecase.LoginCitizenInput{
			CredType: domain.CredEmail, CredValue: "budi@example.com", Password: "rahasia",
		})
	}()
	<-blocker.masuk // slot satu-satunya kini dipegang goroutine di atas

	// Kredensial NYATA saat gerbang jenuh → 429.
	_, err := ucEmp.Execute(context.Background(), usecase.LoginEmployeeInput{
		CredType: domain.CredNIP, CredValue: emp.NIP, Password: "rahasia",
	})
	assertTooManyRequests(t, err)

	// Kredensial TAK DIKENAL saat gerbang jenuh → 429 juga. Keduanya harus tak bisa dibedakan:
	// gerbang tak boleh menjadi orakel baru menggantikan yang ditutup.
	_, errTakDikenal := ucEmp.Execute(context.Background(), usecase.LoginEmployeeInput{
		CredType: domain.CredNIP, CredValue: "199001012015011999", Password: "rahasia",
	})
	assertTooManyRequests(t, errTakDikenal)
	if err.Error() != errTakDikenal.Error() {
		t.Fatalf("respons gerbang jenuh berbeda antara kredensial ada (%q) dan tak ada (%q)", err, errTakDikenal)
	}

	blocker.lepas <- struct{}{}
	<-selesai
}

// Slot dilepas setelah Verify selesai — gerbang membatasi, bukan menyumbat permanen.
func TestVerifyGate_SlotDilepasSetelahVerify(t *testing.T) {
	fx := newLoginFixture()
	_, emp := fx.seedEmployee(t)
	fx.assignTenant(emp, "pemkot-surabaya", true, true)
	fx.gate = usecase.NewVerifyGate(1, time.Second)

	for i := 0; i < 3; i++ {
		if _, err := fx.loginEmployee().Execute(context.Background(), usecase.LoginEmployeeInput{
			CredType: domain.CredNIP, CredValue: emp.NIP, Password: "rahasia",
		}); err != nil {
			t.Fatalf("percobaan ke-%d gagal (%v) — slot tak dilepas setelah Verify", i+1, err)
		}
	}
}

// Gate nil ditolak saat konstruksi. Penyeragaman biaya kerja TANPA batas concurrency adalah
// perbaikan yang membawa regresi DoS-nya sendiri; ia tak boleh bisa dirakit setengah.
func TestNewLoginCitizen_GateNil_Panic(t *testing.T) {
	fx := newLoginFixture()
	defer func() {
		if recover() == nil {
			t.Fatal("gate nil harus panic saat konstruksi, bukan diam-diam tanpa batas concurrency")
		}
	}()
	usecase.NewLoginCitizen(fx.creds, fx.persons, fx.passwords, fx.issuer, fx.limiter,
		usecase.DefaultLoginPolicy(), nil)
}

// Lapis 2 dipanggil TANPA SYARAT — jumlah operasi limiter sama untuk kredensial yang ada maupun
// tidak. Selama store-nya map in-process selisihnya tak terasa; begitu ia Redis (yang memang
// direncanakan `port.RateLimiter`), satu panggilan ekstra = satu round trip jaringan, yaitu bentuk
// lemah dari orakel yang ditutup A11. Diuji sebagai BENTUK key, seperti test A7 di atas.
func TestPasswordAuth_LapisDuaDipanggilJugaSaatKredensialTakAda(t *testing.T) {
	fx := newLoginFixture()

	_, err := fx.loginEmployee().Execute(context.Background(), usecase.LoginEmployeeInput{
		CredType: domain.CredNIP, CredValue: "199001012015011999", Password: "rahasia",
	})
	assertUnauthorized(t, err)

	var lapis2 int
	for key, n := range fx.limiter.calls {
		if strings.HasPrefix(key, "login:cred:") {
			lapis2 += n
		}
	}
	if lapis2 != 1 {
		t.Fatalf("lapis 2 dipanggil %d kali untuk kredensial tak ada, harus 1 — jumlah operasi "+
			"limiter yang berbeda antar jalur menjadi orakel keberadaan akun begitu limiter "+
			"berpindah ke store jaringan", lapis2)
	}
}
