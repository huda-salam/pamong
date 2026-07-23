package notification

import (
	"context"
	"strings"
	"text/template"
)

// Template adalah satu template notifikasi untuk (tenant, key, locale). TenantID kosong =
// template GLOBAL (default framework/modul) yang berlaku untuk semua tenant bila tenant
// belum meng-override. Subject dipakai email/judul in-app; Body adalah isi.
//
// Konten memakai sintaks text/template Go ({{.Field}}). Dipakai text/template (bukan
// html/template) karena target adalah teks (email plain, in-app) — bila kelak ada channel
// HTML, escaping jadi tanggung jawab channel itu, bukan engine.
type Template struct {
	TenantID string // "" = global default
	Key      string // {modul}.{kejadian}, mis. "surat_masuk.disposisi.dibuat"
	Locale   string // "id", "jv", dst; "" diperlakukan sebagai DefaultLocale
	Subject  string
	Body     string
}

// LocaleOrDefault mengembalikan locale template atau DefaultLocale bila kosong.
func (t Template) LocaleOrDefault() string {
	if t.Locale == "" {
		return DefaultLocale
	}
	return t.Locale
}

// TemplateEngine me-resolve template paling cocok untuk (tenant, key, locale) dari
// TemplateStore lalu me-render-nya dengan data. Pemilihan bersifat pure & deterministik
// (tak menyentuh DB) — store hanya menyediakan kandidat, engine yang memutuskan; pola sama
// dengan config.Resolver "paling spesifik menang".
type TemplateEngine struct {
	store TemplateStore
}

// NewTemplateEngine membuat engine di atas store.
func NewTemplateEngine(store TemplateStore) *TemplateEngine {
	return &TemplateEngine{store: store}
}

// Render memilih template terbaik untuk (tenant, key, locale) lalu mengeksekusinya dengan
// data. Urutan preferensi: (1) tenant-spesifik menang atas global, (2) locale sama persis
// menang atas DefaultLocale, (3) DefaultLocale menang atas locale lain. ErrTemplateNotFound
// bila tak ada kandidat; ErrTemplateRender bila sintaks salah / field data hilang.
func (e *TemplateEngine) Render(ctx context.Context, tenantID, key, locale string, data map[string]any) (RenderedMessage, error) {
	if locale == "" {
		locale = DefaultLocale
	}
	cands, err := e.store.Candidates(ctx, tenantID, key)
	if err != nil {
		return RenderedMessage{}, err
	}
	best, ok := pickTemplate(cands, tenantID, locale)
	if !ok {
		return RenderedMessage{}, ErrTemplateNotFound(tenantID, key)
	}

	subject, err := renderText(key, "subject", best.Subject, data)
	if err != nil {
		return RenderedMessage{}, err
	}
	body, err := renderText(key, "body", best.Body, data)
	if err != nil {
		return RenderedMessage{}, err
	}
	return RenderedMessage{Subject: subject, Body: body}, nil
}

// pickTemplate memilih kandidat paling cocok. Hanya kandidat yang applicable (global atau
// tenant sama) yang dipertimbangkan; skor tertinggi menang, deterministik.
func pickTemplate(cands []Template, tenantID, locale string) (Template, bool) {
	var best Template
	bestScore := -1
	for _, c := range cands {
		s := templateScore(c, tenantID, locale)
		if s > bestScore {
			bestScore, best = s, c
		}
	}
	if bestScore < 0 {
		return Template{}, false
	}
	return best, true
}

// templateScore memberi skor kecocokan; -1 = tidak applicable. Locale adalah GERBANG KERAS:
// template yang locale-nya bukan yang diminta dan bukan DefaultLocale TIDAK PERNAH dipakai —
// mencegah pengiriman konten bahasa asing secara diam-diam (mis. minta 'id', hanya ada 'fr').
// Di antara yang lolos gerbang: bobot tenant (4) mendominasi bobot locale (exact 2, default 1)
// agar override tenant menang atas global — TAPI hanya bila locale-nya sendiri terpakai (exact
// atau default). Tenant yang hanya menerjemahkan locale asing jatuh ke global, bukan menimpanya.
func templateScore(c Template, tenantID, locale string) int {
	localeScore := 0
	switch c.LocaleOrDefault() {
	case locale:
		localeScore = 2
	case DefaultLocale:
		localeScore = 1
	default:
		return -1 // locale tak cocok & bukan default → tak dipakai
	}
	switch c.TenantID {
	case tenantID:
		return 4 + localeScore
	case "":
		return localeScore // global — applicable untuk semua tenant
	default:
		return -1 // milik tenant lain
	}
}

// renderText mem-parse & mengeksekusi satu potong template. missingkey=error membuat field
// data yang hilang gagal keras di pintu render (bukan menghasilkan "<no value>" diam-diam) —
// konsisten dengan prinsip "syntax/kesalahan ketahuan di pintu masuk".
func renderText(key, part, tmpl string, data map[string]any) (string, error) {
	t, err := template.New(key + ":" + part).Option("missingkey=error").Parse(tmpl)
	if err != nil {
		return "", ErrTemplateRender(key, part+": "+err.Error())
	}
	var sb strings.Builder
	if err := t.Execute(&sb, data); err != nil {
		return "", ErrTemplateRender(key, part+": "+err.Error())
	}
	return sb.String(), nil
}

// MemoryTemplateStore adalah TemplateStore in-memory untuk seed & test. Kunci internal
// menggabungkan tenant+key+locale sehingga override tenant hidup berdampingan dengan global.
type MemoryTemplateStore struct {
	// templates[tenantID][key] = daftar template lintas locale
	templates map[string]map[string][]Template
}

// NewMemoryTemplateStore membuat store kosong.
func NewMemoryTemplateStore() *MemoryTemplateStore {
	return &MemoryTemplateStore{templates: make(map[string]map[string][]Template)}
}

var _ TemplateStore = (*MemoryTemplateStore)(nil)

// Candidates mengembalikan template tenant-spesifik + global untuk key.
func (s *MemoryTemplateStore) Candidates(_ context.Context, tenantID, key string) ([]Template, error) {
	var out []Template
	out = append(out, s.byTenant("", key)...)
	if tenantID != "" {
		out = append(out, s.byTenant(tenantID, key)...)
	}
	return out, nil
}

func (s *MemoryTemplateStore) byTenant(tenantID, key string) []Template {
	if byKey, ok := s.templates[tenantID]; ok {
		return byKey[key]
	}
	return nil
}

// Upsert menyimpan/menimpa template untuk (tenant, key, locale). Locale kosong dinormalkan
// ke DefaultLocale agar pencocokan konsisten.
func (s *MemoryTemplateStore) Upsert(_ context.Context, t Template) error {
	if t.Key == "" || t.Body == "" {
		return ErrInvalidTemplate("key dan body template wajib diisi")
	}
	t.Locale = t.LocaleOrDefault()
	byKey, ok := s.templates[t.TenantID]
	if !ok {
		byKey = make(map[string][]Template)
		s.templates[t.TenantID] = byKey
	}
	list := byKey[t.Key]
	for i, ex := range list {
		if ex.Locale == t.Locale {
			list[i] = t // timpa
			byKey[t.Key] = list
			return nil
		}
	}
	byKey[t.Key] = append(list, t)
	return nil
}
