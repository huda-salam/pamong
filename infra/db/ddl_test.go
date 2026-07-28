package db_test

import (
	"strings"
	"testing"

	"github.com/huda-salam/pamong/core/domain"
	"github.com/huda-salam/pamong/infra/db"
)

func sampleEntity() domain.EntityDef {
	def := "0"
	return domain.EntityDef{
		Name:       "Barang",
		Schema:     "aset",
		Tier:       domain.Tier1,
		Audit:      domain.Audited{},
		Lockable:   domain.NotLockable{},
		Searchable: []string{"nama"},
		Fields: []domain.FieldDef{
			{Name: "nama", Type: domain.FieldText, Required: true},
			{Name: "kode", Type: domain.FieldText, Unique: true},
			{Name: "harga", Type: domain.FieldDecimal, Precision: 2},
			{Name: "jumlah", Type: domain.FieldInt, Default: &def},
			{Name: "aktif", Type: domain.FieldBool},
			{Name: "kondisi", Type: domain.FieldEnum, Options: []string{"baik", "rusak"}},
			{Name: "lokasi_id", Type: domain.FieldLink, LinkTo: "aset.Lokasi"},
			{Name: "meta", Type: domain.FieldJSON},
		},
	}
}

func TestGenerateMigration_Up(t *testing.T) {
	up, down, err := db.GenerateMigration("aset", []domain.EntityDef{sampleEntity()})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	wants := []string{
		"CREATE SCHEMA IF NOT EXISTS aset;",
		"CREATE TABLE aset.barangs (",
		"id             UUID PRIMARY KEY",
		"nama TEXT NOT NULL",
		"kode TEXT UNIQUE",
		"harga NUMERIC(20, 2)",
		"jumlah BIGINT DEFAULT 0",
		"aktif BOOLEAN",
		"kondisi VARCHAR(64) CHECK (kondisi IN ('baik', 'rusak'))",
		"lokasi_id UUID",
		"meta JSONB",
		"version        INT NOT NULL DEFAULT 1",
		"created_at     TIMESTAMPTZ NOT NULL DEFAULT now()",
		"deleted_at     TIMESTAMPTZ",
		"CREATE INDEX idx_barangs_nama ON aset.barangs (nama);",
	}
	for _, w := range wants {
		if !strings.Contains(up, w) {
			t.Errorf("up tidak memuat %q\n---\n%s", w, up)
		}
	}

	if !strings.Contains(down, "DROP TABLE IF EXISTS aset.barangs;") ||
		!strings.Contains(down, "DROP SCHEMA IF EXISTS aset;") {
		t.Errorf("down kurang lengkap:\n%s", down)
	}
}

func TestGenerateMigration_EmptySchema(t *testing.T) {
	if _, _, err := db.GenerateMigration("", nil); err == nil {
		t.Fatal("schema kosong harus error")
	}
}

// encryptedEntity mendeklarasikan pengenal terenkripsi (ADR-009): nik (Unique+Searchable →
// _enc + _bidx UNIQUE), email (Searchable non-Unique → _enc + _bidx + index), biometrik
// (specific non-searchable → hanya _enc).
func encryptedEntity() domain.EntityDef {
	return domain.EntityDef{
		Name:     "Warga",
		Schema:   "layanan",
		Tier:     domain.Tier1,
		Audit:    domain.Audited{},
		Lockable: domain.NotLockable{},
		Fields: []domain.FieldDef{
			{Name: "nama", Type: domain.FieldText, Required: true, Class: domain.DataPersonal},
			{Name: "nik", Type: domain.FieldText, Required: true, Unique: true, Class: domain.DataPersonalID, Searchable: true},
			{Name: "email", Type: domain.FieldText, Class: domain.DataPersonalID, Searchable: true},
			{Name: "biometrik", Type: domain.FieldJSON, Class: domain.DataSpecific},
		},
	}
}

func TestGenerateMigration_EncryptedColumns(t *testing.T) {
	if err := encryptedEntity().Validate(); err != nil {
		t.Fatalf("entity terenkripsi harus valid: %v", err)
	}
	up, down, err := db.GenerateMigration("layanan", []domain.EntityDef{encryptedEntity()})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	wants := []string{
		"nama TEXT NOT NULL",             // personal (nama) TIDAK dienkripsi
		"nik_enc BYTEA NOT NULL",         // ciphertext
		"nik_bidx BYTEA NOT NULL UNIQUE", // UNIQUE menempel di blind index, bukan _enc
		"email_enc BYTEA",                // non-required
		"email_bidx BYTEA",               // searchable non-unique
		"CREATE INDEX idx_wargas_email_bidx ON layanan.wargas (email_bidx);",
		"biometrik_enc BYTEA", // specific non-searchable → hanya _enc
	}
	for _, w := range wants {
		if !strings.Contains(up, w) {
			t.Errorf("up tidak memuat %q\n---\n%s", w, up)
		}
	}

	// Plaintext pengenal TIDAK boleh muncul sebagai kolom.
	forbidden := []string{
		"\n    nik TEXT", "\n    email TEXT", "\n    biometrik JSONB",
		"nik_enc BYTEA NOT NULL UNIQUE", // UNIQUE tak boleh di _enc
		"biometrik_bidx",                // non-searchable → tak ada bidx
	}
	for _, f := range forbidden {
		if strings.Contains(up, f) {
			t.Errorf("up seharusnya TIDAK memuat %q\n---\n%s", f, up)
		}
	}

	if !strings.Contains(down, "DROP TABLE IF EXISTS layanan.wargas;") {
		t.Errorf("down kurang lengkap:\n%s", down)
	}
}
