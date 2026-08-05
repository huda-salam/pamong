package domain_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/huda-salam/pamong/identity/domain"
)

// fakeRegistrar merekam pendaftaran schema tanpa mengimport infra/eventbus (domain nol-infra).
type fakeRegistrar struct {
	registered map[string]any
	failOn     string
}

var errRegistrasiGagal = errors.New("register gagal")

func (r *fakeRegistrar) Register(name string, schema any) error {
	if name == r.failOn {
		return errRegistrasiGagal
	}
	if r.registered == nil {
		r.registered = map[string]any{}
	}
	r.registered[name] = schema
	return nil
}

// TestRegisterEventSchemas mengunci daftar event identity yang WAJIB ada di registry bus.
// Nilainya bukan "fungsi ini memanggil Register" melainkan CAKUPANNYA: event identity yang
// ditambahkan tanpa mendaftarkannya akan lolos compile lalu ditolak Bus.Publish saat runtime —
// sesudah tulisan DB-nya commit, dan (bila pemanggil membuang error publish) tanpa gejala.
func TestRegisterEventSchemas(t *testing.T) {
	r := &fakeRegistrar{}
	if err := domain.RegisterEventSchemas(r); err != nil {
		t.Fatalf("RegisterEventSchemas: %v", err)
	}
	want := map[string]any{
		domain.EventPersonDibuat:         domain.PersonDibuatPayload{},
		domain.EventEmploymentDibuat:     domain.EmploymentDibuatPayload{},
		domain.EventEmploymentDitugaskan: domain.EmploymentDitugaskanPayload{},
	}
	for name, payload := range want {
		got, ok := r.registered[name]
		if !ok {
			t.Errorf("event %q tak terdaftar", name)
			continue
		}
		// Tipe payload ikut diperiksa: SchemaRegistry mencocokkan identitas TIPE GO, jadi
		// mendaftarkan nama yang benar dengan payload yang salah tetap menolak publish.
		if gotType, wantType := fmt.Sprintf("%T", got), fmt.Sprintf("%T", payload); gotType != wantType {
			t.Errorf("event %q terdaftar dengan payload %s, harap %s", name, gotType, wantType)
		}
	}
	if len(r.registered) != len(want) {
		t.Errorf("jumlah terdaftar %d, harap %d — ada event identity yang belum/berlebih didaftarkan",
			len(r.registered), len(want))
	}
}

// TestRegisterEventSchemas_PropagasiError: kegagalan registry diteruskan supaya wiring gagal
// saat boot, bukan diam-diam melanjutkan dengan registry setengah terisi.
func TestRegisterEventSchemas_PropagasiError(t *testing.T) {
	r := &fakeRegistrar{failOn: domain.EventEmploymentDitugaskan}
	if err := domain.RegisterEventSchemas(r); !errors.Is(err, errRegistrasiGagal) {
		t.Fatalf("error registry harus diteruskan, dapat %v", err)
	}
}
