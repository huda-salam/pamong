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
