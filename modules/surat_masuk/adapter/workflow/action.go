// Package workflow adalah driving adapter WORKFLOW modul surat_masuk: ia menerjemahkan satu
// pemanggilan action dari engine (nama action + params opaque) menjadi pemanggilan use case yang
// BERTIPE. Perannya persis sejajar dengan adapter HTTP — memetakan bentuk kawat ke input use
// case — hanya pemanggilnya yang berbeda: engine, bukan klien HTTP.
//
// Yang BOLEH ada di sini: pembacaan params + konversi tipe. Yang TIDAK boleh: perhitungan,
// validasi bisnis, akses DB, penerbitan event (linter: workflow-action-no-logic, CLAUDE.md #7).
// Seluruhnya sudah hidup di usecase.DisposisiSurat dan tetap di sana — termasuk permission check,
// sehingga transisi yang dipicu workflow melewati gerbang yang sama dengan panggilan langsung.
package workflow

import (
	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/modules/surat_masuk/usecase"
	"github.com/huda-salam/pamong/port"
)

// DisposisiAction membungkus use case DisposisiSurat sebagai port.WorkflowAction (ADR-022).
// Nama pendaftarannya ("DisposisiSurat") adalah nilai field `action` di workflows/disposisi.yaml.
type DisposisiAction struct {
	uc *usecase.DisposisiSurat
}

// NewDisposisiAction merakit adapter. Use case wajib non-nil: action yang terdaftar tapi menunjuk
// use case nil baru meledak saat transisi pertama, di produksi, di tengah alur berjalan.
func NewDisposisiAction(uc *usecase.DisposisiSurat) *DisposisiAction {
	if uc == nil {
		panic("surat_masuk/adapter/workflow: DisposisiSurat nil")
	}
	return &DisposisiAction{uc: uc}
}

var _ port.WorkflowAction = (*DisposisiAction)(nil)

// RunWorkflowAction memetakan input action ke usecase.DisposisiSuratInput.
//
// SuratID diambil dari in.EntityID — entitas yang dikelola instance — BUKAN dari params. Kalau ia
// boleh datang dari params, aktor dapat mendisposisi surat lain lewat instance yang boleh ia
// sentuh, dan seluruh pemeriksaan yang menempel pada instance (state, guard, riwayat) berlaku
// atas surat yang salah.
func (a *DisposisiAction) RunWorkflowAction(ctx port.AuthContext, in port.WorkflowActionInput) error {
	kepada, err := requiredString(in.Params, "kepada_jabatan")
	if err != nil {
		return err
	}
	instruksi, err := optionalString(in.Params, "instruksi")
	if err != nil {
		return err
	}
	_, err = a.uc.Execute(ctx, usecase.DisposisiSuratInput{
		SuratID:       in.EntityID,
		KepadaJabatan: kepada,
		Instruksi:     instruksi,
	})
	return err
}

// requiredString membaca param string yang wajib. Params bertipe map[string]any karena batas
// engine memang tak bertipe (ADR-022 Konsekuensi); konversi gagal di sini menjadi error transisi,
// dan engine membatalkan transisi — state tak berubah.
func requiredString(params map[string]any, key string) (string, error) {
	v, err := optionalString(params, key)
	if err != nil {
		return "", err
	}
	if v == "" {
		return "", core.ErrValidation(key, "wajib diisi pada params action")
	}
	return v, nil
}

// optionalString membaca param string opsional. Nilai absen/null → "" tanpa error; nilai bertipe
// lain → error eksplisit, bukan diam-diam jadi "" (salah tipe adalah bug pemanggil yang harus
// terlihat).
func optionalString(params map[string]any, key string) (string, error) {
	raw, ok := params[key]
	if !ok || raw == nil {
		return "", nil
	}
	s, ok := raw.(string)
	if !ok {
		return "", core.ErrValidation(key, "harus berupa string pada params action")
	}
	return s, nil
}
