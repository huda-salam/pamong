package domain

import "strings"

// Penegakan penamaan tabel (PR-1.2.2). Konvensi framework: nama tabel selalu
// `{schema}.{entity_plural}`, di mana schema = nama modul dan entity_plural adalah
// bentuk snake_case jamak dari nama entity. Aturan ini dihasilkan otomatis dari
// EntityDef sehingga modul tidak menulis nama tabel secara bebas — nama manual yang
// tidak cocok dengan hasil derivasi ditolak saat boot (lihat EntityDef.Validate).

// DeriveTableName menghasilkan nama tabel kanonik untuk sebuah entity pada schema
// tertentu: "{schema}.{plural}". Contoh: ("penatausahaan","SPM") -> "penatausahaan.spms".
func DeriveTableName(schema, entityName string) string {
	return schema + "." + pluralize(toSnakeCase(entityName))
}

// pluralize mengubah satu kata snake_case menjadi bentuk jamak ala Inggris yang
// dipakai konvensi (lihat contoh CLAUDE.md: spms, pegawais, jabatan_histories, asets).
// Hanya segmen terakhir yang dijamakkan agar "jabatan_history" -> "jabatan_histories".
func pluralize(s string) string {
	if s == "" {
		return s
	}
	head, tail := splitLastSegment(s)
	switch {
	case endsWithConsonantY(tail):
		tail = tail[:len(tail)-1] + "ies"
	case strings.HasSuffix(tail, "s"), strings.HasSuffix(tail, "x"),
		strings.HasSuffix(tail, "z"), strings.HasSuffix(tail, "ch"),
		strings.HasSuffix(tail, "sh"):
		tail += "es"
	default:
		tail += "s"
	}
	if head == "" {
		return tail
	}
	return head + "_" + tail
}

// splitLastSegment memecah snake_case menjadi (prefix tanpa segmen terakhir, segmen terakhir).
func splitLastSegment(s string) (head, last string) {
	if i := strings.LastIndex(s, "_"); i >= 0 {
		return s[:i], s[i+1:]
	}
	return "", s
}

// endsWithConsonantY benar bila kata berakhir "y" yang didahului konsonan (history -> histories,
// tapi "day" -> "days").
func endsWithConsonantY(s string) bool {
	if !strings.HasSuffix(s, "y") || len(s) < 2 {
		return false
	}
	switch s[len(s)-2] {
	case 'a', 'e', 'i', 'o', 'u':
		return false
	}
	return true
}

// toSnakeCase mengubah PascalCase/camelCase menjadi snake_case. Rentetan huruf besar
// (akronim) diperlakukan sebagai satu kata: "SPM" -> "spm", "SP2D" -> "sp2d",
// "JabatanHistory" -> "jabatan_history".
func toSnakeCase(s string) string {
	var b strings.Builder
	runes := []rune(s)
	for i, r := range runes {
		isUpper := r >= 'A' && r <= 'Z'
		if isUpper && i > 0 {
			prev := runes[i-1]
			prevLower := prev >= 'a' && prev <= 'z'
			// Batas kata: huruf besar setelah huruf kecil, atau awal kata baru di
			// akhir akronim (huruf besar diikuti huruf kecil). Angka diperlakukan
			// sebagai bagian kata di sekitarnya (SP2D -> sp2d, bukan sp2_d).
			nextLower := i+1 < len(runes) && runes[i+1] >= 'a' && runes[i+1] <= 'z'
			prevUpper := prev >= 'A' && prev <= 'Z'
			if prevLower || (prevUpper && nextLower) {
				b.WriteByte('_')
			}
		}
		if isUpper {
			b.WriteRune(r - 'A' + 'a')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
