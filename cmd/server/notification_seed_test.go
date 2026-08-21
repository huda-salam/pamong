package main

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/huda-salam/pamong/core/domain"
	coreNotif "github.com/huda-salam/pamong/core/notification"
	"github.com/huda-salam/pamong/modules"
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
