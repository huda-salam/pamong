package domain_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/huda-salam/pamong/core/domain"
)

// eventModule adalah modul uji yang hanya membawa EventManifest.
type eventModule struct {
	name   string
	events domain.EventManifest
}

func (m eventModule) Manifest() domain.Manifest {
	return domain.Manifest{Name: m.name, Version: "1.0.0", Events: m.events}
}
func (m eventModule) Bootstrap(context.Context, *domain.App) error { return nil }

// fakeRegistrar meniru kontrak eventbus.SchemaRegistry yang relevan: menyimpan nama→tipe dan
// menolak nama yang sama dengan tipe berbeda. Aturannya SENGAJA ditiru di sini (bukan diimpor)
// karena core/domain tak boleh menyentuh infra; kesetiaan tiruan ini dikunci integration test
// yang memakai registry sungguhan.
type fakeRegistrar struct {
	seen map[string]any
}

func newFakeRegistrar() *fakeRegistrar { return &fakeRegistrar{seen: map[string]any{}} }

func (f *fakeRegistrar) Register(name string, schema any) error {
	if prev, ok := f.seen[name]; ok && reflect.TypeOf(prev) != reflect.TypeOf(schema) {
		return errConflictSchema
	}
	f.seen[name] = schema
	return nil
}

type errString string

func (e errString) Error() string { return string(e) }

const errConflictSchema = errString("event sudah terdaftar dengan tipe berbeda")

type payloadA struct{ ID string }
type payloadB struct{ Nomor int }

func TestRegistry_RegisterEventSchemas_SemuaProducesTerdaftar(t *testing.T) {
	r := domain.NewRegistry()
	r.Register(
		eventModule{name: "surat_masuk", events: domain.EventManifest{
			Produces: []domain.EventDef{
				{Name: "surat_masuk.surat.diterima", Schema: payloadA{}},
				{Name: "surat_masuk.disposisi.dibuat", Schema: payloadB{}},
			},
		}},
		eventModule{name: "kepegawaian", events: domain.EventManifest{
			Produces: []domain.EventDef{{Name: "kepegawaian.pegawai.mutasi", Schema: payloadA{}}},
		}},
	)

	reg := newFakeRegistrar()
	if err := r.RegisterEventSchemas(reg); err != nil {
		t.Fatalf("registrasi manifest yang sah tak boleh gagal: %v", err)
	}
	for _, want := range []string{
		"surat_masuk.surat.diterima", "surat_masuk.disposisi.dibuat", "kepegawaian.pegawai.mutasi",
	} {
		if _, ok := reg.seen[want]; !ok {
			t.Errorf("event %q tidak terdaftar — publish-nya akan ditolak bus", want)
		}
	}
	if len(reg.seen) != 3 {
		t.Errorf("jumlah event terdaftar = %d, mau 3 (%v)", len(reg.seen), reg.seen)
	}
}

// Registry TIDAK menduplikasi aturan nama→tipe (itu milik SchemaRegistry); yang diuji di sini
// adalah bahwa penolakannya diteruskan sebagai kegagalan boot DAN modul penyebabnya disebut.
// Tanpa atribusi itu, operator hanya melihat "event X konflik" tanpa tahu manifest mana.
func TestRegistry_RegisterEventSchemas_KonflikMenyebutModul(t *testing.T) {
	r := domain.NewRegistry()
	r.Register(
		eventModule{name: "modul_a", events: domain.EventManifest{
			Produces: []domain.EventDef{{Name: "bersama.entity.terjadi", Schema: payloadA{}}},
		}},
		eventModule{name: "modul_b", events: domain.EventManifest{
			Produces: []domain.EventDef{{Name: "bersama.entity.terjadi", Schema: payloadB{}}},
		}},
	)

	err := r.RegisterEventSchemas(newFakeRegistrar())
	if err == nil {
		t.Fatal("dua modul dengan nama event sama & tipe berbeda harus menggagalkan boot")
	}
	if !strings.Contains(err.Error(), "modul_b") || !strings.Contains(err.Error(), "bersama.entity.terjadi") {
		t.Errorf("pesan error harus menyebut modul & event penyebab, dapat: %v", err)
	}
}

// Nama event sama dengan tipe SAMA di dua modul tidak ditolak: itu satu kontrak kawat yang
// disepakati, bukan tabrakan. (Registry idempoten — lihat identity/domain.RegisterEventSchemas.)
func TestRegistry_RegisterEventSchemas_NamaSamaTipeSamaLolos(t *testing.T) {
	r := domain.NewRegistry()
	r.Register(
		eventModule{name: "modul_a", events: domain.EventManifest{
			Produces: []domain.EventDef{{Name: "bersama.entity.terjadi", Schema: payloadA{}}},
		}},
		eventModule{name: "modul_b", events: domain.EventManifest{
			Produces: []domain.EventDef{{Name: "bersama.entity.terjadi", Schema: payloadA{}}},
		}},
	)
	if err := r.RegisterEventSchemas(newFakeRegistrar()); err != nil {
		t.Fatalf("nama sama dengan tipe sama harus lolos: %v", err)
	}
}

func TestRegistry_ExternalSubscriptions(t *testing.T) {
	r := domain.NewRegistry()
	r.Register(
		eventModule{name: "surat_masuk", events: domain.EventManifest{
			Produces: []domain.EventDef{{Name: "surat_masuk.surat.diterima", Schema: payloadA{}}},
			Consumes: []domain.EventSubscription{
				{Event: "kepegawaian.pegawai.mutasi", Handler: "OnPegawaiMutasi"}, // produsen tak terpasang
				{Event: "surat_masuk.surat.diterima", Handler: "OnSuratSendiri"},  // diproduksi sendiri
			},
		}},
		eventModule{name: "arsip", events: domain.EventManifest{
			Consumes: []domain.EventSubscription{
				{Event: "surat_masuk.surat.diterima", Handler: "OnSurat"}, // produsen terpasang
			},
		}},
	)

	got := r.ExternalSubscriptions()
	if len(got) != 1 {
		t.Fatalf("jumlah subscription tanpa produsen = %d, mau 1: %+v", len(got), got)
	}
	if got[0].Module != "surat_masuk" || got[0].Event != "kepegawaian.pegawai.mutasi" ||
		got[0].Handler != "OnPegawaiMutasi" {
		t.Errorf("entri salah: %+v", got[0])
	}
}

// Subscription tanpa produsen TIDAK menggagalkan boot: Consumes antar modul memang loose, dan
// deployment yang berbeda memasang himpunan modul yang berbeda. Test ini mengunci keputusan itu
// supaya tak berubah diam-diam menjadi dependency keras.
func TestRegistry_ConsumesTanpaProdusenTidakMenggagalkanBoot(t *testing.T) {
	r := domain.NewRegistry()
	r.Register(eventModule{name: "surat_masuk", events: domain.EventManifest{
		Consumes: []domain.EventSubscription{{Event: "kepegawaian.pegawai.mutasi", Handler: "OnPegawaiMutasi"}},
	}})

	if err := r.Validate(); err != nil {
		t.Fatalf("Consumes menggantung tak boleh membuat registry tak valid: %v", err)
	}
	if err := r.RegisterEventSchemas(newFakeRegistrar()); err != nil {
		t.Fatalf("Consumes menggantung tak boleh menggagalkan registrasi schema: %v", err)
	}
}
