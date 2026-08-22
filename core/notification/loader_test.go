package notification_test

import (
	"strings"
	"testing"
	"testing/fstest"

	coreNotif "github.com/huda-salam/pamong/core/notification"
)

// kontrak adalah field yang disediakan adapter notifikasi alur (infra/workflow.notifyData).
// Di-copy, bukan diimport: core/notification tak boleh bergantung pada infra.
var kontrak = coreNotif.RenderContract{"instance_id", "state", "role"}

const yamlValid = `
templates:
  - key: surat_masuk.surat_selesai
    locale: id
    subject: "Surat selesai diproses"
    body: "Alur {{.instance_id}} mencapai {{.state}}."
`

func TestParseYAML_Sukses(t *testing.T) {
	got, err := coreNotif.ParseYAML("surat_masuk", kontrak, []byte(yamlValid))
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
	got, err := coreNotif.ParseYAML("surat_masuk", kontrak, []byte(yamlValid))
	if err != nil {
		t.Fatalf("ParseYAML: %v", err)
	}
	if got[0].TenantID != "" {
		t.Errorf("TenantID = %q, mau kosong (global)", got[0].TenantID)
	}
}

func TestParseYAML_LocaleKosongJadiDefault(t *testing.T) {
	got, err := coreNotif.ParseYAML("surat_masuk", kontrak, []byte(`
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
	_, err := coreNotif.ParseYAML("surat_masuk", kontrak, []byte(`
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
	if _, err := coreNotif.ParseYAML("surat_masuk", kontrak, []byte(`
templates:
  - key: "surat_masuk."
    subject: "S"
    body: "B"
`)); err == nil {
		t.Fatal("key berisi awalan saja diterima")
	}
}

func TestParseYAML_BodyKosongDitolak(t *testing.T) {
	if _, err := coreNotif.ParseYAML("surat_masuk", kontrak, []byte(`
templates:
  - key: surat_masuk.x
    subject: "S"
`)); err == nil {
		t.Fatal("body kosong diterima")
	}
}

func TestParseYAML_SubjectKosongDitolak(t *testing.T) {
	if _, err := coreNotif.ParseYAML("surat_masuk", kontrak, []byte(`
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
	if _, err := coreNotif.ParseYAML("surat_masuk", kontrak, []byte(`
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
	got, err := coreNotif.ParseYAML("surat_masuk", kontrak, []byte(`
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
	if _, err := coreNotif.ParseYAML("surat_masuk", kontrak, []byte("templates: []\n")); err == nil {
		t.Fatal("file tanpa entri diterima — ref manifest yang menunjuk file kosong adalah kekeliruan")
	}
}

func TestParseYAML_YAMLRusakDitolak(t *testing.T) {
	if _, err := coreNotif.ParseYAML("surat_masuk", kontrak, []byte("templates: [oops")); err == nil {
		t.Fatal("YAML rusak diterima")
	}
}

func TestParseYAML_NamaModulKosongDitolak(t *testing.T) {
	if _, err := coreNotif.ParseYAML("", kontrak, []byte(yamlValid)); err == nil {
		t.Fatal("nama modul kosong diterima — pagar namespace jadi tak bermakna")
	}
}

func TestParseFS_Sukses(t *testing.T) {
	fsys := fstest.MapFS{"notifications/a.yaml": {Data: []byte(yamlValid)}}
	got, err := coreNotif.ParseFS(fsys, "surat_masuk", "notifications/a.yaml", kontrak)
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
	_, err := coreNotif.ParseFS(fsys, "surat_masuk", "notifications/b.yaml", kontrak)
	if err == nil {
		t.Fatal("file tanpa entri diterima")
	}
	if !strings.Contains(err.Error(), "notifications/b.yaml") {
		t.Errorf("pesan error tak menyebut path: %v", err)
	}
}

func TestParseFS_PathTakAda(t *testing.T) {
	if _, err := coreNotif.ParseFS(fstest.MapFS{}, "surat_masuk", "hilang.yaml", kontrak); err == nil {
		t.Fatal("path tak ada diterima")
	}
}

// ===== Kontrak render (dry-run) =====
//
// Parse() saja hanya menangkap sintaks rusak. Field asing — kesalahan yang jauh lebih mungkin,
// karena tak ada apa pun di file YAML yang memberi tahu penulis field apa yang tersedia — lolos
// parse dan baru gagal saat Execute. Ketiga test berikut mengunci kedua sisinya.

func TestParseYAML_SintaksTemplateRusakDitolak(t *testing.T) {
	_, err := coreNotif.ParseYAML("surat_masuk", kontrak, []byte(`
templates:
  - key: surat_masuk.x
    subject: "S"
    body: "Alur {{.instance_id mencapai"
`))
	if err == nil {
		t.Fatal("body dengan action tak tertutup harus ditolak saat parse")
	}
	if !strings.Contains(err.Error(), "body") {
		t.Errorf("pesan tak menyebut bagian yang salah: %v", err)
	}
}

func TestParseYAML_FieldDiLuarKontrakDitolak(t *testing.T) {
	_, err := coreNotif.ParseYAML("surat_masuk", kontrak, []byte(`
templates:
  - key: surat_masuk.x
    subject: "S"
    body: "Surat {{.nomor_surat}} selesai."
`))
	if err == nil {
		t.Fatal("placeholder di luar kontrak harus ditolak — ia lolos Parse tapi gagal saat render")
	}
	// Pesannya harus menyebut field yang TERSEDIA; tanpa itu penulis template hanya tahu
	// bahwa ia salah, bukan apa yang boleh dipakai.
	for _, f := range kontrak {
		if !strings.Contains(err.Error(), f) {
			t.Errorf("pesan tak menyebut field tersedia %q: %v", f, err)
		}
	}
}

// Subject ikut diperiksa, bukan hanya body: judul in-app & email sama-sama di-render.
func TestParseYAML_FieldAsingDiSubjectDitolak(t *testing.T) {
	_, err := coreNotif.ParseYAML("surat_masuk", kontrak, []byte(`
templates:
  - key: surat_masuk.x
    subject: "Surat {{.perihal}}"
    body: "Alur {{.instance_id}}."
`))
	if err == nil {
		t.Fatal("field asing di subject harus ditolak")
	}
	if !strings.Contains(err.Error(), "subject") {
		t.Errorf("pesan tak menyebut subject: %v", err)
	}
}

// Kontrak kosong ditolak alih-alih diperlakukan sebagai "lewati pemeriksaan": nil yang berarti
// diam adalah persis lubang yang pemeriksaan ini tutup.
func TestParseYAML_KontrakKosongDitolak(t *testing.T) {
	if _, err := coreNotif.ParseYAML("surat_masuk", nil, []byte(yamlValid)); err == nil {
		t.Fatal("kontrak kosong harus ditolak")
	}
}

// ===== legacy_keys =====

func TestParseYAML_LegacyKeyDiseedSebagaiKeyTambahan(t *testing.T) {
	got, err := coreNotif.ParseYAML("surat_masuk", kontrak, []byte(`
templates:
  - key: surat_masuk.surat_selesai
    legacy_keys: [surat_selesai]
    locale: id
    subject: "S"
    body: "Alur {{.instance_id}}."
`))
	if err != nil {
		t.Fatalf("ParseYAML: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("jumlah template = %d, mau 2 (kanonik + alias)", len(got))
	}
	if got[0].Key != "surat_masuk.surat_selesai" || got[1].Key != "surat_selesai" {
		t.Fatalf("key = %q, %q", got[0].Key, got[1].Key)
	}
	// Alias harus IDENTIK isinya. Kalau tidak, tenant lama menerima bunyi yang berbeda dari
	// tenant baru untuk kejadian yang sama — perbedaan yang tak seorang pun putuskan.
	if got[1].Subject != got[0].Subject || got[1].Body != got[0].Body || got[1].Locale != got[0].Locale {
		t.Errorf("alias tidak identik dengan kanonik: %+v vs %+v", got[1], got[0])
	}
	if got[1].TenantID != "" {
		t.Errorf("alias harus global, TenantID = %q", got[1].TenantID)
	}
}

// Alias justru ADA untuk key yang melanggar aturan awalan — kalau ia ikut ditolak, jalur rename
// tertutup dan aturan awalan menghapus mitigasinya sendiri.
func TestParseYAML_LegacyKeyBebasDariAturanAwalan(t *testing.T) {
	got, err := coreNotif.ParseYAML("surat_masuk", kontrak, []byte(`
templates:
  - key: surat_masuk.x
    legacy_keys: [x, modul_lama.x]
    subject: "S"
    body: "Alur {{.instance_id}}."
`))
	if err != nil {
		t.Fatalf("legacy key tanpa awalan modul harus DITERIMA: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("jumlah template = %d, mau 3", len(got))
	}
}

func TestParseYAML_LegacyKeySamaDenganKanonikDitolak(t *testing.T) {
	if _, err := coreNotif.ParseYAML("surat_masuk", kontrak, []byte(`
templates:
  - key: surat_masuk.x
    legacy_keys: [surat_masuk.x]
    subject: "S"
    body: "Alur {{.instance_id}}."
`)); err == nil {
		t.Fatal("alias yang sama dengan key kanonik harus ditolak")
	}
}

func TestParseYAML_LegacyKeyKosongDitolak(t *testing.T) {
	if _, err := coreNotif.ParseYAML("surat_masuk", kontrak, []byte(`
templates:
  - key: surat_masuk.x
    legacy_keys: [""]
    subject: "S"
    body: "Alur {{.instance_id}}."
`)); err == nil {
		t.Fatal("legacy key kosong harus ditolak")
	}
}

// Alias tunduk pada larangan duplikat yang sama dengan key kanonik — termasuk saat ia menabrak
// key kanonik entri LAIN, yang tanpa pemeriksaan ini akan diam-diam kalah di InsertIfAbsent.
func TestParseYAML_LegacyKeyMenabrakEntriLainDitolak(t *testing.T) {
	if _, err := coreNotif.ParseYAML("surat_masuk", kontrak, []byte(`
templates:
  - key: surat_masuk.a
    subject: "A"
    body: "Alur {{.instance_id}}."
  - key: surat_masuk.b
    legacy_keys: [surat_masuk.a]
    subject: "B"
    body: "Alur {{.instance_id}}."
`)); err == nil {
		t.Fatal("alias yang menabrak key kanonik entri lain harus ditolak")
	}
}

// ===== YAML ketat =====

// Field WAJIB terlindungi oleh validasi: salah ketik membuatnya kosong dan boot menjatuhkannya.
// Field OPSIONAL tidak — dan `legacy_keys` adalah satu-satunya jalur migrasi saat key di-rename,
// jadi salah ketiknya menghasilkan persis kegagalan yang jalur itu ada untuk mencegah.
func TestParseYAML_FieldTakDikenalDitolak(t *testing.T) {
	for _, salah := range []string{"legacy_key", "legacyKeys", "legacykeys"} {
		t.Run(salah, func(t *testing.T) {
			_, err := coreNotif.ParseYAML("surat_masuk", kontrak, []byte(`
templates:
  - key: surat_masuk.x
    `+salah+`: [lama]
    subject: "S"
    body: "Alur {{.instance_id}}."
`))
			if err == nil {
				t.Fatalf("field %q diterima diam-diam; aliasnya tak akan pernah ditanam", salah)
			}
		})
	}
}

func TestParseYAML_EjaanBenarTetapDiterima(t *testing.T) {
	if _, err := coreNotif.ParseYAML("surat_masuk", kontrak, []byte(`
templates:
  - key: surat_masuk.x
    legacy_keys: [lama]
    subject: "S"
    body: "Alur {{.instance_id}}."
`)); err != nil {
		t.Fatalf("ejaan yang benar ikut ditolak: %v", err)
	}
}

func TestParseYAML_FileKosongDitolak(t *testing.T) {
	if _, err := coreNotif.ParseYAML("surat_masuk", kontrak, nil); err == nil {
		t.Fatal("file kosong diterima")
	}
}

// `body: "   "` lolos pemeriksaan `== ""`, dan InsertIfAbsent memakai pemeriksaan yang sama
// telanjangnya — default berisi spasi mendarat di setiap tenant tanpa satu pun error.
func TestParseYAML_BodySpasiSajaDitolak(t *testing.T) {
	if _, err := coreNotif.ParseYAML("surat_masuk", kontrak, []byte(`
templates:
  - key: surat_masuk.x
    subject: "S"
    body: "   "
`)); err == nil {
		t.Fatal("body berisi spasi saja diterima")
	}
}
