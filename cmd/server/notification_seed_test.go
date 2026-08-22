package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/huda-salam/pamong/core/domain"
	coreNotif "github.com/huda-salam/pamong/core/notification"
	coreWf "github.com/huda-salam/pamong/core/workflow"
	"github.com/jackc/pgx/v5/pgconn"

	infrawf "github.com/huda-salam/pamong/infra/workflow"
	"github.com/huda-salam/pamong/modules"
	"github.com/huda-salam/pamong/port"
)

// fakeModule adalah modul minimal untuk menguji pengumpulan seed — hanya manifest yang dibaca.
type fakeModule struct {
	name  string
	notif []domain.NotificationRef
}

func (m fakeModule) Manifest() domain.Manifest {
	return domain.Manifest{Name: m.name, Version: "1.0.0", Notifications: m.notif}
}
func (m fakeModule) Bootstrap(context.Context, *domain.App) error { return nil }

// coreNotifParse adalah pembungkus ParseYAML untuk test — menjaga import test tetap ringkas.
func coreNotifParse(t *testing.T, modul, body string) ([]coreNotif.Template, error) {
	t.Helper()
	return coreNotif.ParseYAML(modul, infrawf.NotifyTemplateFields(), []byte(body))
}

func tmplFS(body string) fstest.MapFS {
	return fstest.MapFS{"notifications/t.yaml": {Data: []byte(body)}}
}

const seedSatu = `
templates:
  - key: modul_a.selesai
    subject: "S"
    body: "B"
`

func regDengan(mods ...domain.Module) *domain.Registry {
	r := domain.NewRegistry()
	r.Register(mods...)
	return r
}

func TestCollectNotificationSeeds_Sukses(t *testing.T) {
	reg := regDengan(fakeModule{name: "modul_a", notif: []domain.NotificationRef{
		{FS: tmplFS(seedSatu), Path: "notifications/t.yaml"},
	}})
	got, err := collectNotificationSeeds(reg)
	if err != nil {
		t.Fatalf("collectNotificationSeeds: %v", err)
	}
	if len(got) != 1 || got[0].Key != "modul_a.selesai" {
		t.Fatalf("hasil = %+v", got)
	}
}

// Modul tanpa NotificationRef sah — tak semua modul mengirim notifikasi.
func TestCollectNotificationSeeds_ModulTanpaTemplateBukanError(t *testing.T) {
	got, err := collectNotificationSeeds(regDengan(fakeModule{name: "modul_a"}))
	if err != nil {
		t.Fatalf("collectNotificationSeeds: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("hasil = %+v, mau kosong", got)
	}
}

// FS nil ditolak di BOOT dengan alasan yang sama seperti WorkflowRef: seed yang dibaca dari
// disk lulus di mesin developer lalu gagal di produksi.
func TestCollectNotificationSeeds_FSNilDitolak(t *testing.T) {
	_, err := collectNotificationSeeds(regDengan(fakeModule{name: "modul_a",
		notif: []domain.NotificationRef{{Path: "notifications/t.yaml"}}}))
	if err == nil {
		t.Fatal("FS nil diterima")
	}
}

func TestCollectNotificationSeeds_PathKosongDitolak(t *testing.T) {
	_, err := collectNotificationSeeds(regDengan(fakeModule{name: "modul_a",
		notif: []domain.NotificationRef{{FS: tmplFS(seedSatu)}}}))
	if err == nil {
		t.Fatal("Path kosong diterima")
	}
}

// Key milik modul LAIN ditolak: awalan modul adalah satu-satunya yang membuat ruang nama
// global itu aman.
func TestCollectNotificationSeeds_KeyMilikModulLainDitolak(t *testing.T) {
	_, err := collectNotificationSeeds(regDengan(fakeModule{name: "modul_b",
		notif: []domain.NotificationRef{{FS: tmplFS(seedSatu), Path: "notifications/t.yaml"}}}))
	if err == nil {
		t.Fatal("modul_b berhasil mendaftarkan key milik modul_a")
	}
	if !strings.Contains(err.Error(), "modul_b.") {
		t.Errorf("pesan error tak menunjuk awalan yang benar: %v", err)
	}
}

// Satu modul mendaftarkan file yang sama dua kali. Tanpa pemeriksaan ini, InsertIfAbsent
// membuat entri kedua diam-diam tak terpakai.
func TestCollectNotificationSeeds_RefGandaDitolak(t *testing.T) {
	fsys := tmplFS(seedSatu)
	_, err := collectNotificationSeeds(regDengan(fakeModule{name: "modul_a",
		notif: []domain.NotificationRef{
			{FS: fsys, Path: "notifications/t.yaml"},
			{FS: fsys, Path: "notifications/t.yaml"},
		}}))
	if err == nil {
		t.Fatal("ref ganda diterima")
	}
	if !strings.Contains(err.Error(), "dua kali") {
		t.Errorf("pesan error tak menjelaskan duplikasi: %v", err)
	}
}

// YAML rusak menjatuhkan BOOT, bukan tenant pertama yang kebetulan memakai notifikasi.
func TestCollectNotificationSeeds_YAMLRusakMenjatuhkanBoot(t *testing.T) {
	_, err := collectNotificationSeeds(regDengan(fakeModule{name: "modul_a",
		notif: []domain.NotificationRef{{FS: tmplFS("templates: [oops"), Path: "notifications/t.yaml"}}}))
	if err == nil {
		t.Fatal("YAML rusak diterima")
	}
}

// Modul referensi harus benar-benar mengirim default untuk setiap template yang dirujuk
// definisi alurnya. Ini yang menutup lubang "notify: modul selalu ErrTemplateNotFound".
func TestCollectNotificationSeeds_ModulReferensiMengirimDefaultnya(t *testing.T) {
	reg := domain.NewRegistry()
	reg.Register(modules.All()...)
	got, err := collectNotificationSeeds(reg)
	if err != nil {
		t.Fatalf("collectNotificationSeeds: %v", err)
	}
	punya := false
	for _, tm := range got {
		if tm.Key == "surat_masuk.surat_selesai" {
			punya = true
			if tm.TenantID != "" {
				t.Errorf("TenantID = %q, mau kosong (global)", tm.TenantID)
			}
			if tm.Subject == "" || tm.Body == "" {
				t.Errorf("template tanpa subject/body: %+v", tm)
			}
		}
	}
	if !punya {
		t.Fatalf("surat_masuk.surat_selesai tidak diseed; yang ada: %+v", got)
	}
}

// ===== validateNotifyTemplatesSeeded =====

const alurDenganNotify = `
id: modul_a.alur.standar
entity: modul_a.X
version: 1
initial_state: mulai
states:
  mulai:
    label: Mulai
    actions: [selesaikan]
  selesai:
    label: Selesai
    is_terminal: true
transitions:
  - from: mulai
    to: selesai
    on: selesaikan
    notify:
      to_role: petugas
      template: modul_a.selesai
`

func alurFS(body string) fstest.MapFS {
	return fstest.MapFS{"workflows/a.yaml": {Data: []byte(body)}}
}

func TestValidateNotifyTemplatesSeeded_TemplateAdaLolos(t *testing.T) {
	seeds := []domain.WorkflowRef{{FS: alurFS(alurDenganNotify), Path: "workflows/a.yaml"}}
	tmpls, err := coreNotifParse(t, "modul_a", seedSatu)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateNotifyTemplatesSeeded(seeds, tmpls); err != nil {
		t.Fatalf("validateNotifyTemplatesSeeded: %v", err)
	}
}

// Inti pagar: alur merujuk template yang tak seorang pun tulis → BOOT gagal, bukan notifikasi
// yang diam-diam tak sampai setelah rilis.
func TestValidateNotifyTemplatesSeeded_TemplateHilangMenjatuhkanBoot(t *testing.T) {
	seeds := []domain.WorkflowRef{{FS: alurFS(alurDenganNotify), Path: "workflows/a.yaml"}}
	err := validateNotifyTemplatesSeeded(seeds, nil)
	if err == nil {
		t.Fatal("template hilang diterima — pagar tidak menutup")
	}
	for _, mau := range []string{"modul_a.selesai", "modul_a.alur.standar", "workflows/a.yaml"} {
		if !strings.Contains(err.Error(), mau) {
			t.Errorf("pesan error tak menyebut %q: %v", mau, err)
		}
	}
}

// Alur boleh merujuk template milik FRAMEWORK; itu bukan rujukan yatim.
func TestValidateNotifyTemplatesSeeded_TemplateFrameworkDihitung(t *testing.T) {
	alur := strings.Replace(alurDenganNotify, "modul_a.selesai", EscalationTemplateKey, 1)
	seeds := []domain.WorkflowRef{{FS: alurFS(alur), Path: "workflows/a.yaml"}}
	if err := validateNotifyTemplatesSeeded(seeds, nil, EscalationTemplateKey); err != nil {
		t.Fatalf("template framework ditolak: %v", err)
	}
}

// Transisi tanpa notify: tak ada yang perlu diperiksa.
func TestValidateNotifyTemplatesSeeded_TanpaNotifyLolos(t *testing.T) {
	alur := strings.Split(alurDenganNotify, "    notify:")[0]
	seeds := []domain.WorkflowRef{{FS: alurFS(alur), Path: "workflows/a.yaml"}}
	if err := validateNotifyTemplatesSeeded(seeds, nil); err != nil {
		t.Fatalf("alur tanpa notify ditolak: %v", err)
	}
}

// Rakitan produksi yang sesungguhnya: definisi alur + template modul referensi harus konsisten
// satu sama lain. Test inilah yang gagal bila seseorang menambah `notify:` tanpa defaultnya.
func TestValidateNotifyTemplatesSeeded_RakitanProduksiKonsisten(t *testing.T) {
	reg := domain.NewRegistry()
	reg.Register(modules.All()...)
	wf, err := collectWorkflowSeeds(reg)
	if err != nil {
		t.Fatalf("collectWorkflowSeeds: %v", err)
	}
	nt, err := collectNotificationSeeds(reg)
	if err != nil {
		t.Fatalf("collectNotificationSeeds: %v", err)
	}
	if err := validateNotifyTemplatesSeeded(wf, nt, EscalationTemplateKey); err != nil {
		t.Fatalf("rakitan produksi tidak konsisten: %v", err)
	}
}

// ===== validateDefaultLocaleAda (gerbang locale) =====

// TemplateEngine memperlakukan locale sebagai gerbang keras: template yang hanya ada dalam
// locale lain LOLOS boot lalu tak pernah terpilih saat render — gagal yang persis sama dengan
// tak punya template sama sekali.
func TestCollectNotificationSeeds_TanpaLocaleDefaultDitolak(t *testing.T) {
	_, err := collectNotificationSeeds(regDengan(fakeModule{name: "modul_a",
		notif: []domain.NotificationRef{{FS: tmplFS(`
templates:
  - key: modul_a.selesai
    locale: jv
    subject: "S"
    body: "B"
`), Path: "notifications/t.yaml"}}}))
	if err == nil {
		t.Fatal("template tanpa varian locale default diterima")
	}
	if !strings.Contains(err.Error(), coreNotif.DefaultLocale) {
		t.Errorf("pesan error tak menyebut locale default: %v", err)
	}
}

func TestCollectNotificationSeeds_LocaleDefaultPlusTerjemahanDiterima(t *testing.T) {
	got, err := collectNotificationSeeds(regDengan(fakeModule{name: "modul_a",
		notif: []domain.NotificationRef{{FS: tmplFS(`
templates:
  - key: modul_a.selesai
    locale: id
    subject: "S"
    body: "B"
  - key: modul_a.selesai
    locale: jv
    subject: "S"
    body: "B"
`), Path: "notifications/t.yaml"}}}))
	if err != nil {
		t.Fatalf("terjemahan di samping default ditolak: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("jumlah template = %d, mau 2", len(got))
	}
}

// ===== laporStaleNotifyTemplates (drift definisi tersimpan) =====

type fakeDefReader struct{ def coreWf.WorkflowDefinition }

func (r fakeDefReader) Get(string) (coreWf.WorkflowDefinition, error) { return r.def, nil }

type catatanLog struct {
	port.Logger
	errs []string
}

func (l *catatanLog) Error(_ context.Context, msg string, f ...port.Field) {
	for _, fld := range f {
		msg += " " + fmt.Sprint(fld.Value)
	}
	l.errs = append(l.errs, msg)
}
func (l *catatanLog) Info(context.Context, string, ...port.Field)  {}
func (l *catatanLog) Warn(context.Context, string, ...port.Field)  {}
func (l *catatanLog) Debug(context.Context, string, ...port.Field) {}

func defTersimpan(template string) coreWf.WorkflowDefinition {
	return coreWf.WorkflowDefinition{
		ID: "modul_a.alur.standar", Version: 1, InitialState: "mulai",
		States: []coreWf.State{{Name: "mulai"}, {Name: "selesai", IsTerminal: true}},
		Transitions: []coreWf.Transition{{
			From: "mulai", To: "selesai", On: "selesaikan",
			Notify: &coreWf.NotifySpec{ToRole: "petugas", Template: template},
		}},
	}
}

// Titik buta pagar boot: yang dieksekusi adalah definisi di DB, bukan YAML di binary. Tenant
// yang di-provision sebelum rename key tetap memakai key lama, dan SeedIfAbsent tak pernah
// memutakhirkannya.
func TestLaporStaleNotifyTemplates_DefinisiTersimpanBasiDilaporkan(t *testing.T) {
	log := &catatanLog{}
	refs := []domain.WorkflowRef{{FS: alurFS(alurDenganNotify), Path: "workflows/a.yaml"}}
	tersedia := map[string]struct{}{"modul_a.selesai": {}}

	laporStaleNotifyTemplates(context.Background(), "pemkot-x",
		fakeDefReader{def: defTersimpan("surat_selesai")}, refs, tersedia, log)

	if len(log.errs) != 1 {
		t.Fatalf("jumlah laporan = %d, mau 1: %v", len(log.errs), log.errs)
	}
	for _, mau := range []string{"pemkot-x", "modul_a.alur.standar", "surat_selesai", "mulai→selesai"} {
		if !strings.Contains(log.errs[0], mau) {
			t.Errorf("laporan tak menyebut %q: %s", mau, log.errs[0])
		}
	}
}

func TestLaporStaleNotifyTemplates_DefinisiSelarasTakDilaporkan(t *testing.T) {
	log := &catatanLog{}
	refs := []domain.WorkflowRef{{FS: alurFS(alurDenganNotify), Path: "workflows/a.yaml"}}
	tersedia := map[string]struct{}{"modul_a.selesai": {}}

	laporStaleNotifyTemplates(context.Background(), "pemkot-x",
		fakeDefReader{def: defTersimpan("modul_a.selesai")}, refs, tersedia, log)

	if len(log.errs) != 0 {
		t.Fatalf("definisi selaras ikut dilaporkan: %v", log.errs)
	}
}

// Melaporkan, TIDAK menggagalkan: satu key basi tak boleh mematikan seluruh permukaan workflow
// tenant — termasuk GET riwayat yang tak menyentuh notifikasi.
func TestLaporStaleNotifyTemplates_TakMengembalikanError(t *testing.T) {
	refs := []domain.WorkflowRef{{FS: alurFS(alurDenganNotify), Path: "workflows/a.yaml"}}
	// Signature-nya sendiri yang menjamin ini; test mengunci agar tak berubah tanpa sadar.
	laporStaleNotifyTemplates(context.Background(), "pemkot-x",
		fakeDefReader{def: defTersimpan("hilang")}, refs, map[string]struct{}{}, &catatanLog{})
}

func TestTemplateKeys_MencakupFrameworkDanModul(t *testing.T) {
	f := &notificationFactory{moduleTemplates: []coreNotif.Template{{Key: "modul_a.selesai"}}}
	keys := f.templateKeys()
	for _, mau := range []string{"modul_a.selesai", EscalationTemplateKey} {
		if _, ada := keys[mau]; !ada {
			t.Errorf("key %q tidak ada di templateKeys()", mau)
		}
	}
}

// defTersimpanNotify membangun definisi tersimpan dengan NotifySpec apa adanya — termasuk yang
// TAK LENGKAP, yang kini ditolak coreWf.Validate tapi masih bisa ada di baris DB yang ditulis
// sebelum aturan itu berlaku.
func defTersimpanNotify(spec *coreWf.NotifySpec) coreWf.WorkflowDefinition {
	def := defTersimpan("")
	def.Transitions[0].Notify = spec
	return def
}

// `notify:` tak lengkap gagal dengan cara yang sama persis dengan key basi: engine memanggil
// notifier, pengirimannya gagal, transisinya tetap sukses, tak ada yang tahu. Karena itu ia
// masuk laporan yang sama — bukan dilewati seperti sebelumnya.
func TestLaporStaleNotifyTemplates_NotifyTanpaTemplateDilaporkan(t *testing.T) {
	log := &catatanLog{}
	refs := []domain.WorkflowRef{{FS: alurFS(alurDenganNotify), Path: "workflows/a.yaml"}}

	laporStaleNotifyTemplates(context.Background(), "pemkot-x",
		fakeDefReader{def: defTersimpanNotify(&coreWf.NotifySpec{ToRole: "petugas"})},
		refs, map[string]struct{}{"modul_a.selesai": {}}, log)

	if len(log.errs) != 1 {
		t.Fatalf("jumlah laporan = %d, mau 1: %v", len(log.errs), log.errs)
	}
	if !strings.Contains(log.errs[0], "tak lengkap") {
		t.Errorf("laporan tak menyebut sebabnya: %s", log.errs[0])
	}
}

func TestLaporStaleNotifyTemplates_NotifyTanpaRoleDilaporkan(t *testing.T) {
	log := &catatanLog{}
	refs := []domain.WorkflowRef{{FS: alurFS(alurDenganNotify), Path: "workflows/a.yaml"}}

	laporStaleNotifyTemplates(context.Background(), "pemkot-x",
		fakeDefReader{def: defTersimpanNotify(&coreWf.NotifySpec{Template: "modul_a.selesai"})},
		refs, map[string]struct{}{"modul_a.selesai": {}}, log)

	if len(log.errs) != 1 {
		t.Fatalf("peran kosong tak dilaporkan meski templatenya ada: %v", log.errs)
	}
}

// Transisi tanpa `notify:` sama sekali adalah keadaan normal mayoritas transisi — ia tak boleh
// ikut terseret oleh pelonggaran pemeriksaan di atas.
func TestLaporStaleNotifyTemplates_TanpaNotifyTakDilaporkan(t *testing.T) {
	log := &catatanLog{}
	refs := []domain.WorkflowRef{{FS: alurFS(alurDenganNotify), Path: "workflows/a.yaml"}}

	laporStaleNotifyTemplates(context.Background(), "pemkot-x",
		fakeDefReader{def: defTersimpanNotify(nil)}, refs, map[string]struct{}{}, log)

	if len(log.errs) != 0 {
		t.Fatalf("transisi tanpa notify ikut dilaporkan: %v", log.errs)
	}
}

// Tanpa pool (mis. pemanggil yang belum me-resolve DB) pemeriksaan tetap berjalan di atas
// baseline — bukan panik, dan bukan pula diam total.
func TestTemplateKeysFor_TanpaPoolJatuhKeBaseline(t *testing.T) {
	f := &notificationFactory{moduleTemplates: []coreNotif.Template{{Key: "modul_a.selesai"}}}
	keys := f.templateKeysFor(context.Background(), "pemkot-x", nil)
	if _, ada := keys["modul_a.selesai"]; !ada {
		t.Error("baseline modul hilang saat pool nil")
	}
	if _, ada := keys[EscalationTemplateKey]; !ada {
		t.Error("key framework hilang saat pool nil")
	}
}

// ===== Rakitan produksi =====

// Rename key template di baseline TIDAK mengubah definisi alur yang sudah tersimpan di DB tenant
// (SeedIfAbsent tak pernah memutakhirkan yang sudah ada). Tanpa alias, setiap tenant yang
// di-provision sebelum rename `surat_selesai` → `surat_masuk.surat_selesai` akan gagal mengirim
// notifikasi selesai — diam-diam, karena kegagalan notifikasi sengaja tak menjatuhkan transisi.
func TestCollectNotificationSeeds_AliasKeyLamaIkutDiseed(t *testing.T) {
	reg := domain.NewRegistry()
	reg.Register(modules.All()...)
	tmpls, err := collectNotificationSeeds(reg)
	if err != nil {
		t.Fatalf("collectNotificationSeeds: %v", err)
	}
	var kanonik, alias *coreNotif.Template
	for i := range tmpls {
		switch tmpls[i].Key {
		case "surat_masuk.surat_selesai":
			kanonik = &tmpls[i]
		case "surat_selesai":
			alias = &tmpls[i]
		}
	}
	if kanonik == nil {
		t.Fatal("key kanonik tak ada di seed produksi")
	}
	if alias == nil {
		t.Fatal("key lama tak ikut diseed; tenant yang di-provision sebelum rename kehilangan notifikasinya")
	}
	if alias.Body != kanonik.Body || alias.Subject != kanonik.Subject {
		t.Error("alias tidak identik dengan kanonik")
	}
}

// Kontrak field yang divalidasi saat boot harus persis yang dikirim adapter. Bila salah satu
// bergeser, pagarnya berubah dari menjaga menjadi berbohong — dan tetap terlihat hijau.
func TestNotifyTemplateFields_SelarasDenganKontrakAdapter(t *testing.T) {
	got := infrawf.NotifyTemplateFields()
	mau := []string{"instance_id", "role", "state"}
	if strings.Join(got, ",") != strings.Join(mau, ",") {
		t.Fatalf("kontrak = %v, mau %v", got, mau)
	}
}

// Template framework dibangun di KODE, jadi ia melewati ParseYAML. Tanpa test ini, satu-satunya
// template yang tak bisa disunting operator justru menjadi satu-satunya yang tak pernah
// diperiksa — dan salah ketik di dalamnya baru terlihat saat eskalasi SLA pertama, di produksi.
func TestFrameworkTemplates_LolosKontrakRender(t *testing.T) {
	if err := validateFrameworkTemplates(frameworkTemplates()); err != nil {
		t.Fatalf("template framework tak lolos kontrak render: %v", err)
	}
}

// ===== Tabrakan key modul ↔ framework =====

// `legacy_keys` dikecualikan dari aturan awalan modul, jadi ia satu-satunya jalur yang bisa
// menuliskan key milik framework. Seeding framework berjalan lebih dulu di prepare, sehingga
// tabrakan ini menghasilkan diam yang paling membingungkan: boot lulus, modul memuat templatenya,
// dan notifikasi modul mengirimkan kalimat eskalasi framework.
func TestCollectNotificationSeeds_AliasMenabrakKeyFrameworkDitolak(t *testing.T) {
	body := fmt.Sprintf(`
templates:
  - key: modul_a.selesai
    legacy_keys: [%s]
    subject: "S"
    body: "Alur {{.instance_id}}."
`, EscalationTemplateKey)

	_, err := collectNotificationSeeds(regDengan(fakeModule{name: "modul_a",
		notif: []domain.NotificationRef{{FS: tmplFS(body), Path: "notifications/t.yaml"}}}),
		EscalationTemplateKey)
	if err == nil {
		t.Fatal("alias yang menabrak key framework harus ditolak")
	}
	if !strings.Contains(err.Error(), "framework") {
		t.Errorf("pesan tak menyebut asal tabrakan: %v", err)
	}
}

// Key modul yang KEBETULAN mirip tapi tidak sama tetap harus lolos — penjaga di atas tak boleh
// melebar menjadi larangan atas prefix.
func TestCollectNotificationSeeds_KeyMiripFrameworkTetapDiterima(t *testing.T) {
	body := `
templates:
  - key: modul_a.workflow_sla_escalate
    subject: "S"
    body: "Alur {{.instance_id}}."
`
	if _, err := collectNotificationSeeds(regDengan(fakeModule{name: "modul_a",
		notif: []domain.NotificationRef{{FS: tmplFS(body), Path: "notifications/t.yaml"}}}),
		EscalationTemplateKey); err != nil {
		t.Fatalf("key modul yang hanya mirip harus diterima: %v", err)
	}
}

// Rakitan produksi harus lolos penjaga yang sama — kalau tidak, penjaganya hanya berlaku di test.
func TestCollectNotificationSeeds_RakitanProduksiTanpaTabrakanFramework(t *testing.T) {
	reg := domain.NewRegistry()
	reg.Register(modules.All()...)
	if _, err := collectNotificationSeeds(reg, EscalationTemplateKey); err != nil {
		t.Fatalf("rakitan produksi menabrak key framework: %v", err)
	}
}

// ===== Peringatan inventaris: yang normal tak boleh berbunyi =====

// pgErrPalsu meniru *pgconn.PgError agar db.IsUndefinedTable bisa diuji tanpa Postgres.
func pgErrPalsu(code string) error { return &pgconn.PgError{Code: code, Message: "palsu"} }

type catatanSemua struct {
	port.Logger
	warns, debugs int
}

func (l *catatanSemua) Error(context.Context, string, ...port.Field) {}
func (l *catatanSemua) Info(context.Context, string, ...port.Field)  {}
func (l *catatanSemua) Warn(context.Context, string, ...port.Field)  { l.warns++ }
func (l *catatanSemua) Debug(context.Context, string, ...port.Field) { l.debugs++ }

// Tumpukan notifikasi sengaja disiapkan BELAKANGAN, jadi tabel yang belum ada adalah keadaan
// normal pada setiap tenant baru. Memperingatkan di situ memasang alarm yang selalu berbunyi saat
// semuanya benar — persis cacat yang pemeriksaan drift ini ada untuk menghindari.
func TestLaporInventarisGagal_TabelBelumAdaBukanPeringatan(t *testing.T) {
	log := &catatanSemua{}
	f := &notificationFactory{logger: log}

	f.laporInventarisGagal(context.Background(), "pemkot-x", pgErrPalsu("42P01"))

	if log.warns != 0 {
		t.Errorf("tabel belum ada memicu %d peringatan; harus 0", log.warns)
	}
	if log.debugs != 1 {
		t.Errorf("jejak debug = %d, mau 1", log.debugs)
	}
}

// Kegagalan yang sesungguhnya tetap harus terdengar — kalau tidak, pembedaan di atas berubah dari
// mengurangi bising menjadi menyembunyikan masalah.
func TestLaporInventarisGagal_KegagalanLainTetapDiperingatkan(t *testing.T) {
	log := &catatanSemua{}
	f := &notificationFactory{logger: log}

	f.laporInventarisGagal(context.Background(), "pemkot-x", pgErrPalsu("42501")) // izin ditolak

	if log.warns != 1 {
		t.Errorf("kegagalan nyata menghasilkan %d peringatan; mau 1", log.warns)
	}
}

// Dua modul bisa mengklaim satu key global lewat `legacy_keys` pada locale yang saling
// melengkapi: masing-masing lolos pemeriksaan (key, locale), dan validateDefaultLocaleAda puas
// karena SALAH SATU menyediakan varian DefaultLocale. Yang mereka bagi tetap satu baris global,
// dan siapa yang menang ditentukan urutan boot.
func TestCollectNotificationSeeds_AliasDuaModulLocaleBerbedaDitolak(t *testing.T) {
	a := `
templates:
  - key: modul_a.selesai
    locale: id
    legacy_keys: [bersama]
    subject: "A"
    body: "Alur {{.instance_id}}."
`
	b := `
templates:
  - key: modul_b.selesai
    locale: id
    subject: "B"
    body: "Alur {{.instance_id}}."
  - key: modul_b.selesai
    locale: jv
    legacy_keys: [bersama]
    subject: "B jv"
    body: "Alur {{.instance_id}}."
`
	reg := domain.NewRegistry()
	reg.Register(
		fakeModule{name: "modul_a", notif: []domain.NotificationRef{{FS: tmplFS(a), Path: "notifications/t.yaml"}}},
		fakeModule{name: "modul_b", notif: []domain.NotificationRef{{FS: tmplFS(b), Path: "notifications/t.yaml"}}},
	)

	_, err := collectNotificationSeeds(reg, EscalationTemplateKey)
	if err == nil {
		t.Fatal("dua modul berhasil berbagi satu key global lewat locale yang berbeda")
	}
	if !strings.Contains(err.Error(), "bersama") {
		t.Errorf("pesan tak menunjuk key yang ditabrak: %v", err)
	}
}

// Locale ganda DALAM satu modul tetap sah — penjaga kepemilikan tak boleh melebar jadi larangan
// atas i18n.
func TestCollectNotificationSeeds_SatuModulBanyakLocaleDiterima(t *testing.T) {
	body := `
templates:
  - key: modul_a.selesai
    locale: id
    subject: "A"
    body: "Alur {{.instance_id}}."
  - key: modul_a.selesai
    locale: jv
    subject: "A jv"
    body: "Alur {{.instance_id}}."
`
	if _, err := collectNotificationSeeds(regDengan(fakeModule{name: "modul_a",
		notif: []domain.NotificationRef{{FS: tmplFS(body), Path: "notifications/t.yaml"}}}),
		EscalationTemplateKey); err != nil {
		t.Fatalf("satu modul dengan dua locale ditolak: %v", err)
	}
}
