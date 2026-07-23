package notification_test

import (
	"context"
	"testing"

	"github.com/huda-salam/pamong/core/notification"
)

func newEngine(t *testing.T, seed ...notification.Template) *notification.TemplateEngine {
	t.Helper()
	store := notification.NewMemoryTemplateStore()
	for _, tm := range seed {
		if err := store.Upsert(context.Background(), tm); err != nil {
			t.Fatalf("seed template: %v", err)
		}
	}
	return notification.NewTemplateEngine(store)
}

func TestTemplateEngine_RenderGlobal(t *testing.T) {
	eng := newEngine(t, notification.Template{
		Key: "surat.disposisi.dibuat", Locale: "id",
		Subject: "Disposisi {{.nomor}}", Body: "Perihal: {{.perihal}}",
	})
	msg, err := eng.Render(context.Background(), "pemkot-surabaya", "surat.disposisi.dibuat", "id",
		map[string]any{"nomor": "001/IN", "perihal": "Rapat"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if msg.Subject != "Disposisi 001/IN" || msg.Body != "Perihal: Rapat" {
		t.Fatalf("hasil render = %+v", msg)
	}
}

func TestTemplateEngine_TenantOverrideWins(t *testing.T) {
	eng := newEngine(t,
		notification.Template{Key: "k", Locale: "id", Subject: "global", Body: "b"},
		notification.Template{TenantID: "pemkot", Key: "k", Locale: "id", Subject: "tenant", Body: "b"},
	)
	msg, err := eng.Render(context.Background(), "pemkot", "k", "id", nil)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if msg.Subject != "tenant" {
		t.Fatalf("subject = %q, mau template tenant menang", msg.Subject)
	}
}

func TestTemplateEngine_LocaleExactWins(t *testing.T) {
	eng := newEngine(t,
		notification.Template{Key: "k", Locale: "id", Subject: "indo", Body: "b"},
		notification.Template{Key: "k", Locale: "jv", Subject: "jowo", Body: "b"},
	)
	msg, err := eng.Render(context.Background(), "pemkot", "k", "jv", nil)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if msg.Subject != "jowo" {
		t.Fatalf("subject = %q, mau locale jv menang", msg.Subject)
	}
}

func TestTemplateEngine_FallbackToDefaultLocale(t *testing.T) {
	eng := newEngine(t,
		notification.Template{Key: "k", Locale: "id", Subject: "indo", Body: "b"},
	)
	// minta locale jv yang tak ada → jatuh ke default "id"
	msg, err := eng.Render(context.Background(), "pemkot", "k", "jv", nil)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if msg.Subject != "indo" {
		t.Fatalf("subject = %q, mau fallback ke default id", msg.Subject)
	}
}

func TestTemplateEngine_UnrelatedLocaleNotUsed(t *testing.T) {
	// Hanya ada global 'fr'; minta 'id'. Locale asing bukan default → JANGAN dipakai.
	eng := newEngine(t,
		notification.Template{Key: "k", Locale: "fr", Subject: "bonjour", Body: "b"},
	)
	if _, err := eng.Render(context.Background(), "pemkot", "k", "id", nil); err == nil {
		t.Fatal("mau ErrTemplateNotFound; locale asing tak boleh dipakai diam-diam")
	}
}

func TestTemplateEngine_TenantForeignLocaleFallsToGlobal(t *testing.T) {
	// Tenant hanya menerjemahkan 'jv'; global 'id' ada; minta 'id'.
	// Override tenant (locale asing utk permintaan ini) TIDAK menang atas global yang cocok.
	eng := newEngine(t,
		notification.Template{Key: "k", Locale: "id", Subject: "global-id", Body: "b"},
		notification.Template{TenantID: "pemkot", Key: "k", Locale: "jv", Subject: "tenant-jv", Body: "b"},
	)
	msg, err := eng.Render(context.Background(), "pemkot", "k", "id", nil)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if msg.Subject != "global-id" {
		t.Fatalf("subject = %q, mau global-id (tenant jv tak dipakai utk permintaan id)", msg.Subject)
	}
}

func TestTemplateEngine_NotFound(t *testing.T) {
	eng := newEngine(t)
	if _, err := eng.Render(context.Background(), "pemkot", "tak.ada", "id", nil); err == nil {
		t.Fatal("mau ErrTemplateNotFound")
	}
}

func TestTemplateEngine_MissingKeyFailsLoud(t *testing.T) {
	eng := newEngine(t, notification.Template{
		Key: "k", Locale: "id", Subject: "s", Body: "Halo {{.nama}}",
	})
	// data tak punya "nama" → missingkey=error harus gagal, bukan "<no value>"
	if _, err := eng.Render(context.Background(), "pemkot", "k", "id", map[string]any{}); err == nil {
		t.Fatal("mau ErrTemplateRender karena field data hilang")
	}
}

func TestTemplateEngine_InvalidSyntax(t *testing.T) {
	eng := newEngine(t, notification.Template{
		Key: "k", Locale: "id", Subject: "s", Body: "{{.nama",
	})
	if _, err := eng.Render(context.Background(), "pemkot", "k", "id", map[string]any{"nama": "x"}); err == nil {
		t.Fatal("mau ErrTemplateRender karena sintaks salah")
	}
}

func TestMemoryTemplateStore_UpsertOverwrites(t *testing.T) {
	store := notification.NewMemoryTemplateStore()
	_ = store.Upsert(context.Background(), notification.Template{Key: "k", Locale: "id", Subject: "v1", Body: "b"})
	_ = store.Upsert(context.Background(), notification.Template{Key: "k", Locale: "id", Subject: "v2", Body: "b"})
	cands, _ := store.Candidates(context.Background(), "", "k")
	if len(cands) != 1 || cands[0].Subject != "v2" {
		t.Fatalf("candidates = %+v, mau satu entri v2", cands)
	}
}

func TestMemoryTemplateStore_RejectEmpty(t *testing.T) {
	store := notification.NewMemoryTemplateStore()
	if err := store.Upsert(context.Background(), notification.Template{Key: "", Body: "b"}); err == nil {
		t.Fatal("mau error key kosong")
	}
	if err := store.Upsert(context.Background(), notification.Template{Key: "k", Body: ""}); err == nil {
		t.Fatal("mau error body kosong")
	}
}
