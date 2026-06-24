package core_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/huda-salam/pamong/core"
)

// TestErrorTypes_Code memastikan tiap konstruktor menghasilkan Code yang benar
// dan tetap dikenali errors.As setelah dibungkus (%w).
func TestErrorTypes_Code(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		wantCode string
	}{
		{"NotFound", core.ErrNotFound("SuratMasuk", "abc"), "NOT_FOUND"},
		{"PermissionDenied", core.ErrPermissionDenied("surat_masuk:surat:buat"), "PERMISSION_DENIED"},
		{"Validation", core.ErrValidation("nomor_surat", "wajib"), "VALIDATION_ERROR"},
		{"Conflict", core.ErrConflict("version mismatch"), "CONFLICT"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Bungkus untuk meniru penyeberangan batas layer.
			wrapped := fmt.Errorf("konteks: %w", tc.err)

			var fe *core.FrameworkError
			if !errors.As(wrapped, &fe) {
				t.Fatalf("errors.As gagal mengekstrak FrameworkError dari %v", wrapped)
			}
			if fe.Code != tc.wantCode {
				t.Errorf("Code = %q, mau %q", fe.Code, tc.wantCode)
			}
		})
	}
}

// TestErrValidation_Field memastikan field disertakan untuk error validasi.
func TestErrValidation_Field(t *testing.T) {
	var fe *core.FrameworkError
	if !errors.As(core.ErrValidation("pagu", "harus > 0"), &fe) {
		t.Fatal("bukan FrameworkError")
	}
	if fe.Field != "pagu" {
		t.Errorf("Field = %q, mau 'pagu'", fe.Field)
	}
	if fe.Message != "harus > 0" {
		t.Errorf("Message = %q, mau 'harus > 0'", fe.Message)
	}
}
