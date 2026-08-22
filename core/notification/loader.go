package notification

import (
	"bytes"
	"errors"
	"fmt"
	"io"
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
//	    legacy_keys: [surat_selesai]   # opsional — lihat validateLegacyKey
//
// Struct YAML dipisah dari Template (domain) dengan sengaja: format file bisa berevolusi
// tanpa menyentuh domain — pola yang sama dengan core/workflow/loader.go.

type yamlTemplateFile struct {
	Templates []yamlTemplate `yaml:"templates"`
}

type yamlTemplate struct {
	Key        string   `yaml:"key"`
	Locale     string   `yaml:"locale"`
	Subject    string   `yaml:"subject"`
	Body       string   `yaml:"body"`
	LegacyKeys []string `yaml:"legacy_keys"`
}

// RenderContract adalah daftar nama field yang PASTI disediakan pemanggil saat me-render template
// ini — kontrak data antara penulis template dan adapter yang mengirimnya.
//
// Ia ada karena Parse() saja hanya menangkap SETENGAH kesalahan template. `{{.typo` (sintaks
// rusak) memang gagal saat parse, tapi `{{.nomor_surat}}` — field yang tak pernah dikirim
// siapa pun — lolos parse dengan mulus dan baru gagal saat Execute. Yang kedua justru kesalahan
// yang lebih mungkin dilakukan penulis template, karena tak ada satu pun tempat di file YAML yang
// memberitahunya field apa yang tersedia.
//
// Karena itu kontraknya dinyatakan EKSPLISIT dan diteruskan dari pemanggil yang tahu adapternya
// (cmd/server, dari infra/workflow.NotifyTemplateFields), bukan diterka core. Kontrak kosong
// ditolak: nil yang berarti "lewati pemeriksaan" akan mengembalikan lubang ini secara diam-diam.
type RenderContract []string

// ParseYAML mem-parsing bytes YAML menjadi daftar Template GLOBAL (TenantID kosong) milik satu
// modul, tervalidasi.
//
// moduleName wajib: setiap key WAJIB berawalan "{moduleName}." — lihat validateKey.
// contract wajib: setiap subject & body di-DRY-RUN terhadapnya — lihat RenderContract.
//
// Template hasil parse selalu global (TenantID = ""), bukan per-tenant. Itu keputusan yang sama
// dengan default framework: baseline developer berlaku untuk semua tenant sampai satu tenant
// menuliskan barisnya sendiri, dan DBTemplateStore.Candidates sudah memenangkan baris tenant
// atas baris global. Modul tidak tahu-menahu soal tenant mana yang ada.
func ParseYAML(moduleName string, contract RenderContract, data []byte) ([]Template, error) {
	if moduleName == "" {
		return nil, ErrInvalidTemplate("nama modul wajib diisi saat mem-parse template")
	}
	if len(contract) == 0 {
		return nil, ErrInvalidTemplate(fmt.Sprintf(
			"modul %q: kontrak field render wajib dinyatakan — tanpa itu placeholder yang tak "+
				"pernah dikirim siapa pun lolos boot dan gagal saat render", moduleName))
	}
	// KnownFields(true): field YAML yang tak dikenal DITOLAK, tidak diabaikan.
	//
	// Ini bukan kerewelan. Field yang WAJIB (`key`, `subject`, `body`) sudah terlindungi — salah
	// ketik membuatnya kosong dan validasi menjatuhkannya. Yang tak terlindungi justru field
	// OPSIONAL: `legacy_key:` atau `legacyKeys:` ter-parse mulus, boot lulus, dan aliasnya tak
	// pernah ditanam. Padahal `legacy_keys` adalah satu-satunya jalur migrasi saat key di-rename,
	// jadi salah ketiknya menghasilkan persis kegagalan yang jalur itu ada untuk mencegah: tenant
	// lama berhenti menerima notifikasi, diam-diam, tanpa satu pun error di mana pun.
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var raw yamlTemplateFile
	if err := dec.Decode(&raw); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("template notifikasi YAML tidak valid: %w", err)
	}
	if len(raw.Templates) == 0 {
		return nil, ErrInvalidTemplate(fmt.Sprintf(
			"modul %q: file template tidak memuat satu pun entri di bawah `templates:`", moduleName))
	}

	out := make([]Template, 0, len(raw.Templates))
	seen := make(map[string]struct{}, len(raw.Templates))

	// tambah menyimpan satu template setelah memastikan (key, locale)-nya belum dipakai.
	//
	// Duplikat DALAM satu file: yang belakangan tak akan pernah terpakai karena seeding memakai
	// InsertIfAbsent (yang pertama menang). Menolaknya di sini mencegah "sudah saya ubah kok tidak
	// berubah" yang tak meninggalkan jejak apa pun. Alias legacy ikut lewat sini supaya alias yang
	// menabrak key kanonik entri lain juga tertangkap.
	tambah := func(t Template) error {
		id := t.Key + "\x00" + t.Locale
		if _, dup := seen[id]; dup {
			return ErrInvalidTemplate(fmt.Sprintf(
				"modul %q: template %q locale %q didefinisikan lebih dari sekali dalam satu file",
				moduleName, t.Key, t.Locale))
		}
		seen[id] = struct{}{}
		out = append(out, t)
		return nil
	}

	for i, rt := range raw.Templates {
		t := Template{
			TenantID: "", // global — lihat doc di atas
			Key:      strings.TrimSpace(rt.Key),
			Locale:   strings.TrimSpace(rt.Locale),
			Subject:  rt.Subject,
			Body:     rt.Body,
		}
		if err := validateTemplate(moduleName, i, t, contract); err != nil {
			return nil, err
		}
		t.Locale = t.LocaleOrDefault()
		if err := tambah(t); err != nil {
			return nil, err
		}

		for _, lk := range rt.LegacyKeys {
			alias := t
			alias.Key = strings.TrimSpace(lk)
			if err := validateLegacyKey(moduleName, i, t.Key, alias.Key); err != nil {
				return nil, err
			}
			if err := tambah(alias); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

// validateTemplate memeriksa satu entri. Semua kegagalan menyebut modul + indeks entri agar
// diagnosanya langsung menunjuk baris yang salah, bukan sekadar "template tidak valid".
func validateTemplate(moduleName string, idx int, t Template, contract RenderContract) error {
	where := fmt.Sprintf("modul %q template ke-%d", moduleName, idx+1)
	if err := validateKey(moduleName, where, t.Key); err != nil {
		return err
	}
	// TrimSpace, bukan `== ""`: `body: "   "` lolos pemeriksaan telanjang, dan InsertIfAbsent
	// memakai pemeriksaan yang sama telanjangnya — jadi default berisi spasi mendarat di setiap
	// tenant tanpa satu pun error. Sama alasannya dengan subject di bawah.
	if strings.TrimSpace(t.Body) == "" {
		return ErrInvalidTemplate(where + ": body wajib diisi")
	}
	// Subject dituntut di sini meski TemplateStore hanya mewajibkan key+body. Bedanya sumber:
	// ini DEFAULT yang dikirim developer, bukan suntingan operator. Default tanpa judul
	// menghasilkan item inbox tanpa judul di setiap tenant, dan biaya menangkapnya saat boot
	// adalah nol.
	if strings.TrimSpace(t.Subject) == "" {
		return ErrInvalidTemplate(where + ": subject wajib diisi (menjadi judul item in-app)")
	}
	return ValidateRenderable(where, t, contract)
}

// ValidateRenderable mem-PARSE lalu meng-EKSEKUSI subject & body terhadap kontrak field, dengan
// mesin & opsi yang persis sama dengan jalur render sesungguhnya (renderText, missingkey=error).
//
// Dua langkahnya menangkap dua kelas kesalahan yang berbeda dan keduanya nyata:
//   - Parse   → sintaks rusak: `{{.state` tanpa penutup.
//   - Execute → field asing: `{{.nomor_surat}}` yang tak pernah dikirim adapter mana pun.
//
// Nilai dry-run sengaja string kosong: yang diuji adalah BENTUK template, bukan isinya. Diekspor
// karena template framework (yang dibangun di kode, bukan YAML) harus lewat pagar yang sama —
// kalau tidak, satu-satunya template yang tak bisa ditulis operator justru yang tak diperiksa.
func ValidateRenderable(where string, t Template, contract RenderContract) error {
	if len(contract) == 0 {
		return ErrInvalidTemplate(where + ": kontrak field render wajib dinyatakan")
	}
	data := make(map[string]any, len(contract))
	for _, f := range contract {
		data[f] = ""
	}
	for _, bagian := range []struct{ nama, teks string }{
		{"subject", t.Subject},
		{"body", t.Body},
	} {
		if _, err := renderText(t.Key, bagian.nama, bagian.teks, data); err != nil {
			return ErrInvalidTemplate(fmt.Sprintf(
				"%s: %s tidak bisa di-render dengan field yang disediakan pemanggil (%s): %v",
				where, bagian.nama, strings.Join(contract, ", "), err))
		}
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

// validateLegacyKey memeriksa satu alias `legacy_keys`. Alias adalah key TAMBAHAN yang di-seed
// dengan subject & body yang sama persis dengan entri kanoniknya.
//
// Ia sengaja DIKECUALIKAN dari aturan awalan modul, karena justru itu gunanya: aturan awalan
// diperkenalkan setelah key polos sudah beredar, dan definisi alur yang tersimpan di DB tenant
// TIDAK ikut ter-update saat baseline di-rename (SeedIfAbsent tak pernah memutakhirkan definisi
// yang sudah ada). Tanpa jalur alias, satu-satunya cara me-rename key template adalah membiarkan
// tenant lama gagal mengirim notifikasi diam-diam — aturan yang menutup jalan keluarnya sendiri.
//
// Alias adalah UTANG, bukan fitur: ia tetap tunduk pada larangan tabrakan antar modul (lewat
// pemeriksaan duplikat pemanggil), dan harus dihapus begitu jalur upgrade definisi baseline ada.
func validateLegacyKey(moduleName string, idx int, canonical, legacy string) error {
	where := fmt.Sprintf("modul %q template ke-%d", moduleName, idx+1)
	if legacy == "" {
		return ErrInvalidTemplate(where + ": legacy_keys memuat entri kosong")
	}
	if legacy == canonical {
		return ErrInvalidTemplate(fmt.Sprintf(
			"%s: legacy key %q sama dengan key kanoniknya — alias tak menambah apa pun", where, legacy))
	}
	return nil
}

// ParseFS membaca path dari fsys lalu mem-parse-nya. Pembungkus tipis ParseYAML yang menyertakan
// path di pesan error — tanpa itu, kegagalan pada modul ber-banyak file tak bisa dilacak.
func ParseFS(fsys fs.FS, moduleName, path string, contract RenderContract) ([]Template, error) {
	data, err := fs.ReadFile(fsys, path)
	if err != nil {
		return nil, fmt.Errorf("baca template notifikasi %q modul %q: %w", path, moduleName, err)
	}
	tmpls, err := ParseYAML(moduleName, contract, data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return tmpls, nil
}
