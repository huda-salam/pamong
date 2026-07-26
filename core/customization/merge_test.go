package customization_test

import (
	"errors"
	"testing"

	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/core/customization"
	"github.com/huda-salam/pamong/core/domain"
)

// baseEntity: entity inti dengan dua field [nomor, perihal].
func baseEntity() domain.EntityDef {
	return domain.EntityDef{
		Name: "SuratMasuk", Schema: "surat_masuk", Tier: domain.Tier1,
		Audit: domain.Audited{}, Lockable: domain.NotLockable{},
		Fields: []domain.FieldDef{textField("nomor"), textField("perihal")},
	}
}

func fieldNames(e domain.EntityDef) []string {
	out := make([]string, len(e.Fields))
	for i, f := range e.Fields {
		out[i] = f.Name
	}
	return out
}

func withInsertAfter(c customization.CustomFieldDef, after string) customization.CustomFieldDef {
	c.InsertAfter = after
	return c
}

func TestMergeEntity_AppendAtEnd(t *testing.T) {
	base := baseEntity()
	merged := customization.MergeEntity(base, []customization.CustomFieldDef{
		customField("t", "surat_masuk", "SuratMasuk", "catatan"),
	})
	if got := fieldNames(merged); !equal(got, []string{"nomor", "perihal", "catatan"}) {
		t.Fatalf("append di akhir gagal: %v", got)
	}
	// Base tak dimutasi.
	if len(base.Fields) != 2 {
		t.Errorf("base entity termutasi: %v", fieldNames(base))
	}
}

func TestMergeEntity_InsertAfter(t *testing.T) {
	base := baseEntity()
	merged := customization.MergeEntity(base, []customization.CustomFieldDef{
		withInsertAfter(customField("t", "surat_masuk", "SuratMasuk", "sifat"), "nomor"),
	})
	if got := fieldNames(merged); !equal(got, []string{"nomor", "sifat", "perihal"}) {
		t.Fatalf("insert setelah nomor gagal: %v", got)
	}
}

// TestMergeEntity_ChainedInsertAfter: anchor bisa custom lain yang tersisip lebih dulu maupun
// yang datang belakangan (deferred resolution), hasil deterministik.
func TestMergeEntity_ChainedInsertAfter(t *testing.T) {
	base := baseEntity()
	// b anchor ke a; a anchor ke nomor. b diproses lebih dulu (anchor belum ada) → deferred.
	merged := customization.MergeEntity(base, []customization.CustomFieldDef{
		withInsertAfter(customField("t", "surat_masuk", "SuratMasuk", "b"), "a"),
		withInsertAfter(customField("t", "surat_masuk", "SuratMasuk", "a"), "nomor"),
	})
	if got := fieldNames(merged); !equal(got, []string{"nomor", "a", "b", "perihal"}) {
		t.Fatalf("chained insert gagal: %v", got)
	}
}

// TestMergeEntity_ChainReverseInputOrder: rantai 3-tingkat yang urutan input-nya (List =
// alfabetis) BERKEBALIKAN dengan urutan dependency harus tetap terurut benar (fixpoint, bukan
// satu pass). alpha←beta←gamma di-List sbg [alpha,beta,gamma]; gamma anchor nomor, beta anchor
// gamma, alpha anchor beta → hasil harus [nomor,gamma,beta,alpha,perihal].
func TestMergeEntity_ChainReverseInputOrder(t *testing.T) {
	base := baseEntity()
	merged := customization.MergeEntity(base, []customization.CustomFieldDef{
		withInsertAfter(customField("t", "surat_masuk", "SuratMasuk", "alpha"), "beta"),
		withInsertAfter(customField("t", "surat_masuk", "SuratMasuk", "beta"), "gamma"),
		withInsertAfter(customField("t", "surat_masuk", "SuratMasuk", "gamma"), "nomor"),
	})
	if got := fieldNames(merged); !equal(got, []string{"nomor", "gamma", "beta", "alpha", "perihal"}) {
		t.Fatalf("rantai berjenjang harus terurut via fixpoint: %v", got)
	}
}

func TestMergeEntity_UnknownAnchorKeEnd(t *testing.T) {
	base := baseEntity()
	merged := customization.MergeEntity(base, []customization.CustomFieldDef{
		withInsertAfter(customField("t", "surat_masuk", "SuratMasuk", "x"), "tidak_ada"),
	})
	if got := fieldNames(merged); !equal(got, []string{"nomor", "perihal", "x"}) {
		t.Fatalf("anchor tak dikenal harus jatuh ke akhir: %v", got)
	}
}

func TestMergeEntity_InactiveDilewati(t *testing.T) {
	base := baseEntity()
	inactive := customField("t", "surat_masuk", "SuratMasuk", "draft")
	inactive.IsActive = false
	merged := customization.MergeEntity(base, []customization.CustomFieldDef{inactive})
	if got := fieldNames(merged); !equal(got, []string{"nomor", "perihal"}) {
		t.Fatalf("field nonaktif harus dilewati: %v", got)
	}
}

// TestMergeEntity_CollisionDefensifDilewati: custom ber-nama sama field inti dilewati (jalur
// tulis menolaknya lebih dulu; merge bersikap defensif agar tak menduplikasi kolom).
func TestMergeEntity_CollisionDefensifDilewati(t *testing.T) {
	base := baseEntity()
	merged := customization.MergeEntity(base, []customization.CustomFieldDef{
		customField("t", "surat_masuk", "SuratMasuk", "nomor"),
	})
	if got := fieldNames(merged); !equal(got, []string{"nomor", "perihal"}) {
		t.Fatalf("collision harus dilewati: %v", got)
	}
}

func TestValidateAgainstBase(t *testing.T) {
	base := baseEntity()
	// Bentrok field inti → conflict.
	err := customization.ValidateAgainstBase(base, customField("t", "surat_masuk", "SuratMasuk", "perihal"))
	var fe *core.FrameworkError
	if !errors.As(err, &fe) || fe.Code != "CONFLICT" {
		t.Errorf("harap CONFLICT untuk bentrok field inti, dapat %v", err)
	}
	// Nama baru → ok.
	if err := customization.ValidateAgainstBase(base, customField("t", "surat_masuk", "SuratMasuk", "baru")); err != nil {
		t.Errorf("nama baru harus lolos, dapat %v", err)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
