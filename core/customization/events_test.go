package customization_test

import (
	"testing"

	"github.com/huda-salam/pamong/core/customization"
)

// fakeRegistrar merekam pendaftaran schema untuk verifikasi tanpa import infra/eventbus.
type fakeRegistrar struct {
	registered map[string]any
	failOn     string
}

func (r *fakeRegistrar) Register(name string, schema any) error {
	if name == r.failOn {
		return errFakeRegister
	}
	if r.registered == nil {
		r.registered = map[string]any{}
	}
	r.registered[name] = schema
	return nil
}

type fakeRegisterErr struct{}

func (fakeRegisterErr) Error() string { return "register gagal" }

var errFakeRegister error = fakeRegisterErr{}

// TestRegisterEventSchemas: keempat event terdaftar (seam wiring wajib agar Bus.Publish tak
// menolak event kustomisasi).
func TestRegisterEventSchemas(t *testing.T) {
	r := &fakeRegistrar{}
	if err := customization.RegisterEventSchemas(r); err != nil {
		t.Fatalf("RegisterEventSchemas: %v", err)
	}
	want := []string{
		customization.EventCustomFieldDitambahkan,
		customization.EventCustomFieldDinonaktifkan,
		customization.EventLabelDiubah,
		customization.EventCapabilityDiubah,
	}
	for _, name := range want {
		if _, ok := r.registered[name]; !ok {
			t.Errorf("event %q tak terdaftar", name)
		}
	}
	if len(r.registered) != len(want) {
		t.Errorf("jumlah terdaftar %d, harap %d", len(r.registered), len(want))
	}
}

// TestRegisterEventSchemas_PropagasiError: kegagalan registry diteruskan (fail-fast wiring).
func TestRegisterEventSchemas_PropagasiError(t *testing.T) {
	r := &fakeRegistrar{failOn: customization.EventLabelDiubah}
	if err := customization.RegisterEventSchemas(r); err == nil {
		t.Fatal("error registry harus diteruskan")
	}
}
