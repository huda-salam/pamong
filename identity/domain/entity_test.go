package domain_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/identity/domain"
)

func TestPerson_Validate(t *testing.T) {
	cases := []struct {
		name    string
		p       domain.Person
		wantErr error
	}{
		{"valid", domain.Person{NIK: "3578010101900001", NamaLengkap: "Budi"}, nil},
		{"nik kurang digit", domain.Person{NIK: "123", NamaLengkap: "Budi"}, domain.ErrNIKInvalid},
		{"nik ada huruf", domain.Person{NIK: "357801010190000X", NamaLengkap: "Budi"}, domain.ErrNIKInvalid},
		{"nama kosong", domain.Person{NIK: "3578010101900001"}, domain.ErrNamaKosong},
		{"kontak kosong boleh", domain.Person{NIK: "3578010101900001", NamaLengkap: "Budi"}, nil},
		{"email ber-CRLF", domain.Person{NIK: "3578010101900001", NamaLengkap: "Budi", Email: "budi@x.id\r\n"}, domain.ErrEmailFormat},
		{"email spasi tepi", domain.Person{NIK: "3578010101900001", NamaLengkap: "Budi", Email: " budi@x.id"}, domain.ErrEmailFormat},
		{"no_hp tab", domain.Person{NIK: "3578010101900001", NamaLengkap: "Budi", NoHP: "0811\t2233"}, domain.ErrNoHPFormat},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.p.Validate()
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("Validate() = %v, mau %v", err, c.wantErr)
			}
		})
	}
}

func TestEmployment_Validate(t *testing.T) {
	pid := uuid.New()
	cases := []struct {
		name    string
		e       domain.Employment
		wantErr error
	}{
		{"asn valid", domain.Employment{PersonID: pid, Status: domain.StatusASN, NIP: "199001012015011001"}, nil},
		{"asn tanpa nip", domain.Employment{PersonID: pid, Status: domain.StatusASN}, domain.ErrNIPWajibASN},
		{"asn nip pendek", domain.Employment{PersonID: pid, Status: domain.StatusASN, NIP: "123"}, domain.ErrNIPInvalid},
		{"non_asn valid", domain.Employment{PersonID: pid, Status: domain.StatusNonASN}, nil},
		{"non_asn ada nip", domain.Employment{PersonID: pid, Status: domain.StatusNonASN, NIP: "199001012015011001"}, domain.ErrNIPTerisiNonASN},
		{"status invalid", domain.Employment{PersonID: pid, Status: "kontrak"}, domain.ErrStatusInvalid},
		{"person kosong", domain.Employment{Status: domain.StatusNonASN}, domain.ErrPersonIDKosong},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.e.Validate()
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("Validate() = %v, mau %v", err, c.wantErr)
			}
		})
	}
}

func TestCredential_Validate(t *testing.T) {
	pid := uuid.New()
	cases := []struct {
		name    string
		c       domain.Credential
		wantErr error
	}{
		{"valid", domain.Credential{PersonID: pid, CredType: domain.CredNIP, CredValue: "199001012015011001"}, nil},
		{"tipe invalid", domain.Credential{PersonID: pid, CredType: "sidik_jari", CredValue: "x"}, domain.ErrCredTypeInvalid},
		{"nilai kosong", domain.Credential{PersonID: pid, CredType: domain.CredEmail}, domain.ErrCredValueKosong},
		{"person kosong", domain.Credential{CredType: domain.CredNIK, CredValue: "x"}, domain.ErrPersonIDKosong},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.c.Validate()
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("Validate() = %v, mau %v", err, c.wantErr)
			}
		})
	}
}

// TestCredential_Validate_BentukPengenal menutup jalur TULIS dari orakel keberadaan akun.
//
// Sejak pengenal di-lookup lewat blind index (PR-3.8.6), infra/crypto.normalize membuang spasi
// tepi sebelum meng-HMAC. Nilai yang tersimpan verbatim karena itu bisa "ditemukan" oleh ejaan
// lain — dan kalau nilai TERSIMPAN-nya sendiri yang ber-CRLF, ia me-resolve dengan sukses lalu
// ditolak SMTP sebagai header injection (500), sementara alamat tak dikenal menjawab 200. Satu
// probe per target. Menolak di pintu masuk membuat keadaan itu tak bisa dibuat.
//
// Verifikasi mutasi: lemahkan bentukPengenalRusak (mis. buang cabang TrimSpace, atau ganti
// unicode.IsControl jadi `false`) — setiap sub-test di bawah HARUS gagal.
func TestCredential_Validate_BentukPengenal(t *testing.T) {
	pid := uuid.New()
	cases := []struct {
		name    string
		value   string
		wantErr error
	}{
		{"bersih", "budi@x.id", nil},
		{"spasi di tengah tidak dilarang", "Jl. Merdeka 1", nil}, // aturannya tepi, bukan seluruh nilai
		{"LF di akhir", "budi@x.id\n", domain.ErrCredValueFormat},
		{"CRLF di akhir", "budi@x.id\r\n", domain.ErrCredValueFormat},
		{"CRLF + header sisipan", "budi@x.id\r\nBcc: penyerang@y.id", domain.ErrCredValueFormat},
		{"CR di tengah", "budi\r@x.id", domain.ErrCredValueFormat},
		{"NUL di tengah", "budi\x00@x.id", domain.ErrCredValueFormat},
		{"TAB di tengah", "budi\t@x.id", domain.ErrCredValueFormat},
		{"DEL di tengah", "budi\x7f@x.id", domain.ErrCredValueFormat},
		{"C1 control di tengah", "budi@x.id", domain.ErrCredValueFormat},
		{"spasi di awal", " budi@x.id", domain.ErrCredValueFormat},
		{"spasi di akhir", "budi@x.id ", domain.ErrCredValueFormat},
		{"NBSP di tepi", "budi@x.id ", domain.ErrCredValueFormat},
		{"hanya spasi", "   ", domain.ErrCredValueFormat},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cred := domain.Credential{PersonID: pid, CredType: domain.CredEmail, CredValue: c.value}
			if err := cred.Validate(); !errors.Is(err, c.wantErr) {
				t.Fatalf("Validate(%q) = %v, mau %v", c.value, err, c.wantErr)
			}
		})
	}
}

// TestCredential_Validate_KanonikSetelahLolos mengikat aturan bentuk pada ALASANNYA, bukan pada
// daftar karakter: nilai yang lolos Validate harus sudah kanonik menurut normalisasi yang dipakai
// blind index (trim; case-fold khusus purpose `email` tidak ikut diuji di sini — itu kebijakan
// infra/crypto, dan domain memang tak boleh menyentuhnya). Kalau suatu saat aturan bentuk
// dilonggarkan, test ini gagal bersama yang di atas.
func TestCredential_Validate_KanonikSetelahLolos(t *testing.T) {
	pid := uuid.New()
	values := []string{
		"budi@x.id", "081122334455", "199001012015011001", "3578010101900001",
		"budi@x.id ", " budi@x.id", "budi@x.id\r\n", "\tbudi@x.id\t",
	}
	for _, v := range values {
		cred := domain.Credential{PersonID: pid, CredType: domain.CredEmail, CredValue: v}
		if err := cred.Validate(); err != nil {
			continue // ditolak di pintu masuk — tak pernah tersimpan
		}
		if v != strings.TrimSpace(v) {
			t.Fatalf("nilai %q lolos Validate tapi tak sama dengan bentuk ter-normalisasi %q — "+
				"nilai tersimpan bisa berbeda dari ejaan yang me-resolve kepadanya", v, strings.TrimSpace(v))
		}
	}
}
