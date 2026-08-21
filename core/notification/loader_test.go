package notification_test

import (
	"strings"
	"testing"
	"testing/fstest"

	coreNotif "github.com/huda-salam/pamong/core/notification"
)

const yamlValid = `
templates:
  - key: surat_masuk.surat_selesai
    locale: id
    subject: "Surat selesai diproses"
    body: "Alur {{.instance_id}} mencapai {{.state}}."
`

func TestParseYAML_Sukses(t *testing.T) {
	got, err := coreNotif.ParseYAML("surat_masuk", []byte(yamlValid))
	if err != nil {
		t.Fatalf("ParseYAML: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("jumlah template = %d, mau 1", len(got))
	}
	tm := got[0]
	if tm.Key != "surat_masuk.surat_selesai" {
		t.Errorf("key = %q", tm.Key)
	}
	if tm.Locale != "id" || tm.Subject == "" || tm.Body == "" {
		t.Errorf("isi template tidak lengkap: %+v", tm)
	}
}

// Template modul di-seed sebagai baris GLOBAL. Kalau ParseYAML membiarkan TenantID terisi,
// baseline developer akan mendarat sebagai override milik satu tenant — dan tenant lain
// kehilangan defaultnya tanpa satu pun error.
func TestParseYAML_TemplateSelaluGlobal(t *testing.T) {
	got, err := coreNotif.ParseYAML("surat_masuk", []byte(yamlValid))
	if err != nil {
		t.Fatalf("ParseYAML: %v", err)
	}
	if got[0].TenantID != "" {
		t.Errorf("TenantID = %q, mau kosong (global)", got[0].TenantID)
	}
}

func TestParseYAML_LocaleKosongJadiDefault(t *testing.T) {
	got, err := coreNotif.ParseYAML("surat_masuk", []byte(`
templates:
  - key: surat_masuk.x
    subject: "S"
    body: "B"
`))
	if err != nil {
		t.Fatalf("ParseYAML: %v", err)
	}
	if got[0].Locale != coreNotif.DefaultLocale {
		t.Errorf("locale = %q, mau %q", got[0].Locale, coreNotif.DefaultLocale)
	}
}

// Inti pagar namespace: dua modul yang sama-sama memakai key polos bertabrakan DIAM karena
// seeding memakai InsertIfAbsent — yang di-boot lebih dulu menang, yang kedua diam-diam
// mengirim kalimat modul pertama.
func TestParseYAML_KeyTanpaAwalanModulDitolak(t *testing.T) {
	_, err := coreNotif.ParseYAML("surat_masuk", []byte(`
templates:
  - key: surat_selesai
    subject: "S"
    body: "B"
`))
	if err == nil {
		t.Fatal("key polos diterima — pagar namespace tidak menutup")
	}
	if !strings.Contains(err.Error(), "surat_masuk.") {
		t.Errorf("pesan error tak menyebut awalan yang diminta: %v", err)
	}
}

func TestParseYAML_KeyHanyaAwalanDitolak(t *testing.T) {
	if _, err := coreNotif.ParseYAML("surat_masuk", []byte(`
templates:
  - key: "surat_masuk."
    subject: "S"
    body: "B"
`)); err == nil {
		t.Fatal("key berisi awalan saja diterima")
	}
}

func TestParseYAML_BodyKosongDitolak(t *testing.T) {
	if _, err := coreNotif.ParseYAML("surat_masuk", []byte(`
templates:
  - key: surat_masuk.x
    subject: "S"
`)); err == nil {
		t.Fatal("body kosong diterima")
	}
}

func TestParseYAML_SubjectKosongDitolak(t *testing.T) {
	if _, err := coreNotif.ParseYAML("surat_masuk", []byte(`
templates:
  - key: surat_masuk.x
    body: "B"
`)); err == nil {
		t.Fatal("subject kosong diterima — item in-app akan tak berjudul di semua tenant")
	}
}

// Duplikat dalam satu file: yang belakangan tak pernah terpakai (InsertIfAbsent), jadi
// menerimanya berarti menyembunyikan suntingan yang tak berefek.
func TestParseYAML_DuplikatKeyLocaleDitolak(t *testing.T) {
	if _, err := coreNotif.ParseYAML("surat_masuk", []byte(`
templates:
  - key: surat_masuk.x
    locale: id
    subject: "S1"
    body: "B1"
  - key: surat_masuk.x
    locale: id
    subject: "S2"
    body: "B2"
`)); err == nil {
		t.Fatal("duplikat (key, locale) diterima")
	}
}

// Locale berbeda pada key yang sama BUKAN duplikat — itu terjemahan, dan justru didukung
// TemplateEngine.
func TestParseYAML_KeySamaLocaleBerbedaDiterima(t *testing.T) {
	got, err := coreNotif.ParseYAML("surat_masuk", []byte(`
templates:
  - key: surat_masuk.x
    locale: id
    subject: "S"
    body: "B"
  - key: surat_masuk.x
    locale: jv
    subject: "S"
    body: "B"
`))
	if err != nil {
		t.Fatalf("ParseYAML: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("jumlah template = %d, mau 2", len(got))
	}
}

func TestParseYAML_FileTanpaEntriDitolak(t *testing.T) {
	if _, err := coreNotif.ParseYAML("surat_masuk", []byte("templates: []\n")); err == nil {
		t.Fatal("file tanpa entri diterima — ref manifest yang menunjuk file kosong adalah kekeliruan")
	}
}

func TestParseYAML_YAMLRusakDitolak(t *testing.T) {
	if _, err := coreNotif.ParseYAML("surat_masuk", []byte("templates: [oops")); err == nil {
		t.Fatal("YAML rusak diterima")
	}
}

func TestParseYAML_NamaModulKosongDitolak(t *testing.T) {
	if _, err := coreNotif.ParseYAML("", []byte(yamlValid)); err == nil {
		t.Fatal("nama modul kosong diterima — pagar namespace jadi tak bermakna")
	}
}

func TestParseFS_Sukses(t *testing.T) {
	fsys := fstest.MapFS{"notifications/a.yaml": {Data: []byte(yamlValid)}}
	got, err := coreNotif.ParseFS(fsys, "surat_masuk", "notifications/a.yaml")
	if err != nil {
		t.Fatalf("ParseFS: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("jumlah template = %d, mau 1", len(got))
	}
}

// Modul boleh punya banyak file; tanpa path di pesan error, kegagalan tak bisa dilacak
// ke file mana.
func TestParseFS_ErrorMenyebutPath(t *testing.T) {
	fsys := fstest.MapFS{"notifications/b.yaml": {Data: []byte("templates: []\n")}}
	_, err := coreNotif.ParseFS(fsys, "surat_masuk", "notifications/b.yaml")
	if err == nil {
		t.Fatal("file tanpa entri diterima")
	}
	if !strings.Contains(err.Error(), "notifications/b.yaml") {
		t.Errorf("pesan error tak menyebut path: %v", err)
	}
}

func TestParseFS_PathTakAda(t *testing.T) {
	if _, err := coreNotif.ParseFS(fstest.MapFS{}, "surat_masuk", "hilang.yaml"); err == nil {
		t.Fatal("path tak ada diterima")
	}
}
