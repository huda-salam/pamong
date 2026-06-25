package domain_test

import (
	"errors"
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
