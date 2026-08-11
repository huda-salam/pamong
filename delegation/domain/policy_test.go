package domain_test

import (
	"testing"

	"github.com/huda-salam/pamong/delegation/domain"
)

// TestNonDelegableSet_Namespace: wildcard `ns:*` ada supaya larangan tak bocor seiring waktu —
// permission baru di keluarga yang dilarang tidak boleh diam-diam menjadi boleh didelegasikan.
func TestNonDelegableSet_Namespace(t *testing.T) {
	s := domain.NewNonDelegableSet("identity:*", "keuangan:spm:terbitkan")

	for _, perm := range []string{
		"identity:credential:buat",
		"identity:assignment:cross_tenant",
		"identity:permission:yang:belum:ada", // justru inti wildcard: belum ada saat aturan ditulis
		"keuangan:spm:terbitkan",
	} {
		if !s.Contains(perm) {
			t.Errorf("%q harus non-delegable", perm)
		}
	}
	for _, perm := range []string{"surat_masuk:surat:baca", "keuangan:spm:baca", "identitas:x:y"} {
		if s.Contains(perm) {
			t.Errorf("%q tidak boleh ikut terlarang", perm)
		}
	}
}

// TestDefaultNonDelegable_MenutupDuaNamespace: default yang dipakai composition root. `identity:*`
// menutup jalan pintas "delegasikan alih-alih berikan lewat role" (pagar role tenant sudah ada);
// `iam:*` menutup pelimpahan kemampuan MELIMPAHKAN, yang sekali lolos bisa dilebarkan berantai.
func TestDefaultNonDelegable_MenutupDuaNamespace(t *testing.T) {
	s := domain.DefaultNonDelegable()

	for _, perm := range []string{
		"identity:credential:buat",
		"iam:tenant_role:assign",
		"iam:delegasi:buat",
	} {
		if !s.Contains(perm) {
			t.Errorf("%q harus non-delegable secara default", perm)
		}
	}
	// Permission bisnis biasa tetap boleh didelegasikan — itu gunanya delegasi/PLT.
	if s.Contains("keuangan:spm:terbitkan") {
		t.Error("permission bisnis tidak boleh ikut terlarang secara default")
	}
}

// TestDefaultNonDelegable_TambahanTidakMenggantikan: larangan spesifik-tenant ditumpuk DI ATAS
// default, bukan menggantinya.
func TestDefaultNonDelegable_TambahanTidakMenggantikan(t *testing.T) {
	s := domain.DefaultNonDelegable("keuangan:spm:ttd_kpa")

	if !s.Contains("keuangan:spm:ttd_kpa") {
		t.Error("larangan tambahan tak berlaku")
	}
	if !s.Contains("identity:credential:buat") {
		t.Error("larangan default hilang saat tambahan diberikan")
	}
}
