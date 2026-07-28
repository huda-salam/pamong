package domain_test

import (
	"testing"

	"github.com/huda-salam/pamong/core/domain"
)

func TestDataClass_Normalize(t *testing.T) {
	if got := domain.DataClass("").Normalize(); got != domain.DataPublic {
		t.Errorf("zero value harus menormalkan ke public; dapat %q", got)
	}
	if got := domain.DataPersonalID.Normalize(); got != domain.DataPersonalID {
		t.Errorf("nilai non-kosong tak boleh berubah; dapat %q", got)
	}
}

func TestDataClass_Valid(t *testing.T) {
	valid := []domain.DataClass{"", domain.DataPublic, domain.DataInternal, domain.DataPersonal, domain.DataPersonalID, domain.DataSpecific}
	for _, c := range valid {
		if !c.Valid() {
			t.Errorf("class %q seharusnya valid", c)
		}
	}
	if domain.DataClass("rahasia").Valid() {
		t.Error("class tak dikenal seharusnya invalid")
	}
}

func TestDataClass_IsEncrypted(t *testing.T) {
	// Hanya personal_id & specific yang dienkripsi (ADR-009 §1). personal (nama/alamat) TIDAK.
	enc := map[domain.DataClass]bool{
		"":                    false, // zero → public
		domain.DataPublic:     false,
		domain.DataInternal:   false,
		domain.DataPersonal:   false, // penting: nama TIDAK dienkripsi (harus dapat dicari)
		domain.DataPersonalID: true,
		domain.DataSpecific:   true,
	}
	for c, want := range enc {
		if got := c.IsEncrypted(); got != want {
			t.Errorf("IsEncrypted(%q)=%v, mau %v", c, got, want)
		}
	}
}

// TestFieldDef_EncryptedValid memastikan field terenkripsi yang benar (Text, Searchable, Unique)
// lulus validasi — inilah bentuk kanonik pengenal seperti NIK/NIP.
func TestFieldDef_EncryptedValid(t *testing.T) {
	f := domain.FieldDef{Name: "nik", Type: domain.FieldText, Required: true, Unique: true, Class: domain.DataPersonalID, Searchable: true}
	if err := f.Validate(); err != nil {
		t.Fatalf("field terenkripsi valid tak boleh error: %v", err)
	}
	// specific tanpa searchable (mis. data biometrik yang tak perlu lookup) juga valid.
	g := domain.FieldDef{Name: "biometrik", Type: domain.FieldJSON, Class: domain.DataSpecific}
	if err := g.Validate(); err != nil {
		t.Fatalf("field specific non-searchable valid tak boleh error: %v", err)
	}
}
