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
	return coreNotif.ParseYAML(modul, []byte(body))
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
