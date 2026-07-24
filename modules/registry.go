// Package modules adalah satu-satunya daftar modul bisnis yang dipasang ke
// framework. Baik binary server (cmd/server) maupun toolchain (pamongctl) memakai
// All() agar daftar modul tidak terduplikasi di dua tempat.
package modules

import (
	"github.com/huda-salam/pamong/core/domain"
	surat_masuk "github.com/huda-salam/pamong/modules/surat_masuk"
)

// All mengembalikan instance semua modul terdaftar. Tambahkan modul baru di sini.
//
// CATATAN: `surat_masuk` di sini adalah modul REFERENSI/CONTOH (sample project), bukan
// modul produksi. Selama framework dibangun ia sengaja terdaftar sebagai target dev &
// harness validasi end-to-end (ROADMAP Phase 7). Deployment nyata untuk pemda MENGGANTI
// daftar ini dengan modul bisnis riil — pemisahan "modul contoh" dari "modul yang
// di-register deployment" dijadwalkan di Phase 5.1/7 (lihat ROADMAP backlog). Sampai itu,
// jangan asumsikan isi All() = modul yang layak dikirim ke produksi.
func All() []domain.Module {
	return []domain.Module{
		&surat_masuk.Module{},
	}
}
