package domainnoinfra

import "testing"

// TestIsDomainPackage_SegmenBukanSubstring mengunci perbaikan bug: pencocokan lapisan
// domain harus per-segmen path, bukan substring. Tanpa ini, package seperti
// "rules/domainnoinfra" salah dikira lapisan "domain" dan ikut diperiksa.
func TestIsDomainPackage_SegmenBukanSubstring(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"github.com/huda-salam/pamong/modules/surat_masuk/domain", true},
		{"github.com/huda-salam/pamong/modules/surat_masuk/usecase", true},
		{"github.com/huda-salam/pamong/tools/linter/rules/domainnoinfra", false}, // bukan domain
		{"github.com/huda-salam/pamong/core/config", false},
		{"github.com/huda-salam/pamong/gateway", false},
		{"github.com/huda-salam/pamong/usecasekit", false}, // substring "usecase" tapi bukan segmen
	}
	for _, tc := range cases {
		if got := isDomainPackage(tc.path); got != tc.want {
			t.Errorf("isDomainPackage(%q) = %v, mau %v", tc.path, got, tc.want)
		}
	}
}

// TestViolation_SegmenInfra memastikan deteksi import infra juga per-segmen.
func TestViolation_SegmenInfra(t *testing.T) {
	if violation("github.com/huda-salam/pamong/infra/db") == "" {
		t.Error("import infra/db harus terdeteksi sebagai pelanggaran")
	}
	if violation("github.com/x/adapterkit") != "" {
		t.Error("'adapterkit' bukan segmen 'adapter' — tidak boleh dianggap pelanggaran")
	}
	if violation("os") == "" {
		t.Error("import os harus terdeteksi pelanggaran stdlib I/O")
	}
}
