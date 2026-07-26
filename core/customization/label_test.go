package customization_test

import (
	"testing"

	"github.com/huda-salam/pamong/core/config"
	"github.com/huda-salam/pamong/core/customization"
)

func TestLabelKey(t *testing.T) {
	got := customization.LabelKey("surat_masuk", "SuratMasuk", "perihal")
	want := "customization.label.surat_masuk.SuratMasuk.perihal"
	if got != want {
		t.Errorf("LabelKey = %q, harap %q", got, want)
	}
}

// TestLabelResolver membuktikan override label tersimpan sebagai tenant config ber-scope dan
// terbaca kembali; tenant lain tak terpengaruh (isolasi lewat scope).
func TestLabelResolver(t *testing.T) {
	store := config.NewMemoryTenantConfigStore()
	resolver := config.NewResolver(store)
	lr := customization.NewLabelResolver(resolver)

	// Belum di-set → ok=false (pemakai pakai label bawaan).
	if _, ok, err := lr.Label(ctx(), "t", "surat_masuk", "SuratMasuk", "perihal"); err != nil || ok {
		t.Fatalf("belum di-set harus ok=false: ok=%v err=%v", ok, err)
	}

	_ = store.Set(ctx(), config.ConfigEntry{
		Scope: config.ConfigScope{TenantID: "t"},
		Key:   customization.LabelKey("surat_masuk", "SuratMasuk", "perihal"),
		Value: "Hal / Perihal Surat",
	})

	v, ok, err := lr.Label(ctx(), "t", "surat_masuk", "SuratMasuk", "perihal")
	if err != nil || !ok || v != "Hal / Perihal Surat" {
		t.Fatalf("label override: v=%q ok=%v err=%v", v, ok, err)
	}
	// Tenant lain tak terpengaruh.
	if _, ok, _ := lr.Label(ctx(), "t2", "surat_masuk", "SuratMasuk", "perihal"); ok {
		t.Error("override tenant t bocor ke t2")
	}
}
