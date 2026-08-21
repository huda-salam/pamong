package notification

import (
	"fmt"
	"io/fs"
	"strings"

	"gopkg.in/yaml.v3"
)

// ===== YAML intermediate struct =====
//
// Format file template notifikasi modul (lihat modules/surat_masuk/notifications/*.yaml):
//
//	templates:
//	  - key: surat_masuk.surat_selesai
//	    locale: id
//	    subject: "Surat selesai diproses"
//	    body: "Alur {{.instance_id}} ..."
//
// Struct YAML dipisah dari Template (domain) dengan sengaja: format file bisa berevolusi
// tanpa menyentuh domain — pola yang sama dengan core/workflow/loader.go.

type yamlTemplateFile struct {
	Templates []yamlTemplate `yaml:"templates"`
}

type yamlTemplate struct {
	Key     string `yaml:"key"`
	Locale  string `yaml:"locale"`
	Subject string `yaml:"subject"`
	Body    string `yaml:"body"`
}

// ParseYAML mem-parsing bytes YAML menjadi daftar Template GLOBAL (TenantID kosong) milik satu
// modul, tervalidasi.
//
// moduleName wajib: setiap key WAJIB berawalan "{moduleName}." — lihat validateKey.
//
// Template hasil parse selalu global (TenantID = ""), bukan per-tenant. Itu keputusan yang sama
// dengan default framework: baseline developer berlaku untuk semua tenant sampai satu tenant
// menuliskan barisnya sendiri, dan DBTemplateStore.Candidates sudah memenangkan baris tenant
// atas baris global. Modul tidak tahu-menahu soal tenant mana yang ada.
func ParseYAML(moduleName string, data []byte) ([]Template, error) {
	if moduleName == "" {
		return nil, ErrInvalidTemplate("nama modul wajib diisi saat mem-parse template")
	}
	var raw yamlTemplateFile
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("template notifikasi YAML tidak valid: %w", err)
	}
	if len(raw.Templates) == 0 {
		return nil, ErrInvalidTemplate(fmt.Sprintf(
			"modul %q: file template tidak memuat satu pun entri di bawah `templates:`", moduleName))
	}

	out := make([]Template, 0, len(raw.Templates))
	seen := make(map[string]struct{}, len(raw.Templates))
	for i, rt := range raw.Templates {
		t := Template{
			TenantID: "", // global — lihat doc di atas
			Key:      strings.TrimSpace(rt.Key),
			Locale:   strings.TrimSpace(rt.Locale),
			Subject:  rt.Subject,
			Body:     rt.Body,
		}
		if err := validateTemplate(moduleName, i, t); err != nil {
			return nil, err
		}
		t.Locale = t.LocaleOrDefault()

		// Duplikat (key, locale) DALAM satu file: yang belakangan tak akan pernah terpakai
		// karena seeding memakai InsertIfAbsent (yang pertama menang). Menolaknya di sini
		// mencegah "sudah saya ubah kok tidak berubah" yang tak meninggalkan jejak apa pun.
		id := t.Key + "\x00" + t.Locale
		if _, dup := seen[id]; dup {
			return nil, ErrInvalidTemplate(fmt.Sprintf(
				"modul %q: template %q locale %q didefinisikan lebih dari sekali dalam satu file",
				moduleName, t.Key, t.Locale))
		}
		seen[id] = struct{}{}
		out = append(out, t)
	}
	return out, nil
}

// validateTemplate memeriksa satu entri. Semua kegagalan menyebut modul + indeks entri agar
// diagnosanya langsung menunjuk baris yang salah, bukan sekadar "template tidak valid".
func validateTemplate(moduleName string, idx int, t Template) error {
	where := fmt.Sprintf("modul %q template ke-%d", moduleName, idx+1)
	if err := validateKey(moduleName, where, t.Key); err != nil {
		return err
	}
	if t.Body == "" {
		return ErrInvalidTemplate(where + ": body wajib diisi")
	}
	// Subject dituntut di sini meski TemplateStore hanya mewajibkan key+body. Bedanya sumber:
	// ini DEFAULT yang dikirim developer, bukan suntingan operator. Default tanpa judul
	// menghasilkan item inbox tanpa judul di setiap tenant, dan biaya menangkapnya saat boot
	// adalah nol.
	if strings.TrimSpace(t.Subject) == "" {
		return ErrInvalidTemplate(where + ": subject wajib diisi (menjadi judul item in-app)")
	}
	return nil
}

// validateKey menegakkan namespace `{modul}.{kejadian}` pada key template.
//
// Ini bukan kerapian: template modul di-seed sebagai baris GLOBAL, jadi seluruh modul berbagi
// satu ruang nama di `gov.notification_templates`. Dua modul yang sama-sama memakai key polos
// seperti "surat_selesai" akan bertabrakan, dan seeding ber-InsertIfAbsent membuat tabrakan itu
// DIAM: yang di-boot lebih dulu menang, modul kedua mengirim notifikasi berisi kalimat modul
// pertama. Konvensinya sejajar dengan nama event ({modul}.{entity}.{kejadian}) dan workflow
// template key ({modul}.{alur}.{varian}) — lihat tabel penamaan CLAUDE.md.
func validateKey(moduleName, where, key string) error {
	if key == "" {
		return ErrInvalidTemplate(where + ": key wajib diisi")
	}
	prefix := moduleName + "."
	if !strings.HasPrefix(key, prefix) {
		return ErrInvalidTemplate(fmt.Sprintf(
			"%s: key %q wajib berawalan %q — template modul berbagi satu ruang nama global, "+
				"key polos akan bertabrakan diam-diam antar modul", where, key, prefix))
	}
	if strings.TrimSpace(strings.TrimPrefix(key, prefix)) == "" {
		return ErrInvalidTemplate(fmt.Sprintf("%s: key %q hanya berisi awalan modul", where, key))
	}
	return nil
}

// ParseFS membaca path dari fsys lalu mem-parse-nya. Pembungkus tipis ParseYAML yang menyertakan
// path di pesan error — tanpa itu, kegagalan pada modul ber-banyak file tak bisa dilacak.
func ParseFS(fsys fs.FS, moduleName, path string) ([]Template, error) {
	data, err := fs.ReadFile(fsys, path)
	if err != nil {
		return nil, fmt.Errorf("baca template notifikasi %q modul %q: %w", path, moduleName, err)
	}
	tmpls, err := ParseYAML(moduleName, data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return tmpls, nil
}
