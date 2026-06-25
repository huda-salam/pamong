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
