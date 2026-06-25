package domain_test

import (
	"testing"

	"github.com/huda-salam/pamong/core/domain"
)

func TestDeriveTableName(t *testing.T) {
	cases := []struct {
		schema, entity, want string
	}{
		{"penatausahaan", "SPM", "penatausahaan.spms"},
		{"penatausahaan", "SP2D", "penatausahaan.sp2ds"},
		{"kepegawaian", "Pegawai", "kepegawaian.pegawais"},
		{"kepegawaian", "JabatanHistory", "kepegawaian.jabatan_histories"},
		{"aset", "Aset", "aset.asets"},
		{"persuratan", "SuratMasuk", "persuratan.surat_masuks"},
		{"keuangan", "Berkas", "keuangan.berkases"},
	}
	for _, c := range cases {
		if got := domain.DeriveTableName(c.schema, c.entity); got != c.want {
			t.Errorf("DeriveTableName(%q,%q) = %q, mau %q", c.schema, c.entity, got, c.want)
		}
	}
}

func TestEntityDef_TableName_AutoDerived(t *testing.T) {
	e := domain.EntityDef{Schema: "penatausahaan", Name: "SPM"} // Tablename kosong
	if got := e.TableName(); got != "penatausahaan.spms" {
		t.Fatalf("TableName auto-derive = %q, mau penatausahaan.spms", got)
	}
}

func TestEntityDef_Validate_RejectsWrongTableName(t *testing.T) {
	e := domain.EntityDef{
		Name:      "SPM",
		Schema:    "penatausahaan",
		Tablename: "penatausahaan.spm", // salah: tidak jamak
		Tier:      domain.Tier1,
		Audit:     domain.NotAudited{Reason: "test"},
		Lockable:  domain.NotLockable{},
	}
	if err := e.Validate(); err == nil {
		t.Fatal("Tablename salah konvensi harus ditolak")
	}
}

func TestEntityDef_Residency_DefaultTenant(t *testing.T) {
	e := domain.EntityDef{Name: "X", Schema: "m"} // Residency tidak diisi
	if e.IsCentral() {
		t.Fatal("default residency harus tenant, bukan central")
	}
}

func TestEntityDef_Residency_Central(t *testing.T) {
	e := domain.EntityDef{
		Name: "Wilayah", Schema: "referensi", Tier: domain.Tier1,
		Residency: domain.ResidencyCentral,
		Audit:     domain.NotAudited{Reason: "data referensi statis"}, Lockable: domain.NotLockable{},
	}
	if !e.IsCentral() {
		t.Fatal("ResidencyCentral harus IsCentral() true")
	}
	if err := e.Validate(); err != nil {
		t.Fatalf("entity central valid harus lolos: %v", err)
	}
}

func TestEntityDef_Validate_RejectsUnknownResidency(t *testing.T) {
	e := domain.EntityDef{
		Name: "X", Schema: "m", Tier: domain.Tier1,
		Residency: domain.DataResidency(99),
		Audit:     domain.NotAudited{Reason: "t"}, Lockable: domain.NotLockable{},
	}
	if err := e.Validate(); err == nil {
		t.Fatal("residency tak dikenal harus ditolak")
	}
}

func TestEntityDef_Validate_AcceptsCanonicalTableName(t *testing.T) {
	e := domain.EntityDef{
		Name:      "SPM",
		Schema:    "penatausahaan",
		Tablename: "penatausahaan.spms",
		Tier:      domain.Tier1,
		Audit:     domain.NotAudited{Reason: "test"},
		Lockable:  domain.NotLockable{},
	}
	if err := e.Validate(); err != nil {
		t.Fatalf("Tablename kanonik harus lolos, dapat: %v", err)
	}
}
