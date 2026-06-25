// Package modules adalah satu-satunya daftar modul bisnis yang dipasang ke
// framework. Baik binary server (cmd/server) maupun toolchain (pamongctl) memakai
// All() agar daftar modul tidak terduplikasi di dua tempat.
package modules

import (
	"github.com/huda-salam/pamong/core/domain"
	surat_masuk "github.com/huda-salam/pamong/modules/surat_masuk"
)

// All mengembalikan instance semua modul terdaftar. Tambahkan modul baru di sini.
func All() []domain.Module {
	return []domain.Module{
		&surat_masuk.Module{},
	}
}
