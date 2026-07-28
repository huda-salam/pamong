package db

import (
	"fmt"
	"strings"

	"github.com/huda-salam/pamong/core/domain"
)

// DDL generation dari EntityDef (PR-1.2.4). Menghasilkan SQL up+down untuk sebuah
// modul: CREATE SCHEMA + CREATE TABLE per entity dengan kolom standar framework
// (id, version, created_at, updated_at, deleted_at) plus index untuk field Searchable.
// Output dipakai `pamongctl generate migration` sebagai baseline yang bisa di-review.

// GenerateMigration menghasilkan pasangan (up, down) SQL untuk semua entity satu modul.
// Semua entity diasumsikan berada pada schema yang sama (= nama modul).
func GenerateMigration(schema string, entities []domain.EntityDef) (up, down string, err error) {
	if schema == "" {
		return "", "", fmt.Errorf("schema kosong")
	}
	var ub, db strings.Builder
	fmt.Fprintf(&ub, "-- Auto-generated dari EntityDef (pamongctl generate migration).\n")
	fmt.Fprintf(&ub, "-- Backward-compatible: hanya additive. Tinjau sebelum commit.\n\n")
	fmt.Fprintf(&ub, "CREATE SCHEMA IF NOT EXISTS %s;\n", schema)

	fmt.Fprintf(&db, "-- Down auto-generated. Urutan terbalik dari up.\n")

	dropStmts := make([]string, 0, len(entities))
	for _, e := range entities {
		table := e.TableName()
		stmt, err := createTable(table, e)
		if err != nil {
			return "", "", err
		}
		ub.WriteString("\n")
		ub.WriteString(stmt)
		for _, col := range e.Searchable {
			idx := indexName(table, col)
			fmt.Fprintf(&ub, "CREATE INDEX %s ON %s (%s);\n", idx, table, col)
		}
		// Blind index equality untuk field terenkripsi Searchable non-Unique (ADR-009 §2).
		// Field Unique sudah memperoleh index implisit dari constraint UNIQUE pada _bidx.
		for _, f := range e.Fields {
			if f.Class.IsEncrypted() && f.Searchable && !f.Unique {
				bcol := bidxColumn(f.Name)
				fmt.Fprintf(&ub, "CREATE INDEX %s ON %s (%s);\n", indexName(table, bcol), table, bcol)
			}
		}
		dropStmts = append(dropStmts, fmt.Sprintf("DROP TABLE IF EXISTS %s;", table))
	}
	// Down: drop tabel urutan terbalik agar FK intra-modul aman, lalu drop schema.
	for i := len(dropStmts) - 1; i >= 0; i-- {
		db.WriteString(dropStmts[i])
		db.WriteString("\n")
	}
	fmt.Fprintf(&db, "DROP SCHEMA IF EXISTS %s;\n", schema)

	return ub.String(), db.String(), nil
}

func createTable(table string, e domain.EntityDef) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "CREATE TABLE %s (\n", table)
	cols := []string{"    id             UUID PRIMARY KEY"}

	for _, f := range e.Fields {
		defs, err := columnDef(f)
		if err != nil {
			return "", err
		}
		for _, d := range defs {
			cols = append(cols, "    "+d)
		}
	}

	// Kolom sistem framework. created_by/updated_by SUDAH direserve di domain
	// (reservedFieldNames) tapi BELUM di-generate di sini — actor-tracking menyusul saat
	// generation pipeline punya konteks aktor. Nama dilindungi lebih dulu agar penambahan
	// nanti tidak bentrok dengan field modul.
	cols = append(cols,
		"    version        INT NOT NULL DEFAULT 1",
		"    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()",
		"    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()",
		"    deleted_at     TIMESTAMPTZ",
	)
	b.WriteString(strings.Join(cols, ",\n"))
	b.WriteString("\n);\n")
	return b.String(), nil
}

// columnDef menghasilkan definisi kolom dari FieldDef. Umumnya satu kolom; untuk field
// terenkripsi (Class personal_id/specific, ADR-009) DUA kolom fisik: {field}_enc (ciphertext
// AES-256-GCM) plus {field}_bidx (blind index HMAC) bila Searchable — nilai plaintext {field}
// TIDAK pernah disimpan. UNIQUE menempel pada _bidx, bukan _enc (nonce GCM acak → tak bisa unik).
func columnDef(f domain.FieldDef) ([]string, error) {
	if f.Class.IsEncrypted() {
		return encryptedColumnDefs(f), nil
	}
	sqlType, err := pgType(f)
	if err != nil {
		return nil, err
	}
	parts := []string{f.Name, sqlType}
	if f.Required {
		parts = append(parts, "NOT NULL")
	}
	if f.Unique {
		parts = append(parts, "UNIQUE")
	}
	if f.Default != nil {
		parts = append(parts, "DEFAULT "+*f.Default)
	}
	if f.Type == domain.FieldEnum {
		quoted := make([]string, len(f.Options))
		for i, o := range f.Options {
			quoted[i] = "'" + strings.ReplaceAll(o, "'", "''") + "'"
		}
		parts = append(parts, fmt.Sprintf("CHECK (%s IN (%s))", f.Name, strings.Join(quoted, ", ")))
	}
	return []string{strings.Join(parts, " ")}, nil
}

// encryptedColumnDefs memetakan satu field terenkripsi ke kolom fisiknya (ADR-009 §2).
// FieldDef.Validate menjamin invariant (mis. Unique ⇒ Searchable) sebelum sampai sini.
func encryptedColumnDefs(f domain.FieldDef) []string {
	enc := []string{encColumn(f.Name), "BYTEA"}
	if f.Required {
		enc = append(enc, "NOT NULL")
	}
	out := []string{strings.Join(enc, " ")}
	if f.Searchable {
		bidx := []string{bidxColumn(f.Name), "BYTEA"}
		if f.Required {
			bidx = append(bidx, "NOT NULL")
		}
		if f.Unique {
			bidx = append(bidx, "UNIQUE")
		}
		out = append(out, strings.Join(bidx, " "))
	}
	return out
}

func encColumn(field string) string  { return field + "_enc" }
func bidxColumn(field string) string { return field + "_bidx" }

// pgType memetakan FieldType ke tipe kolom Postgres.
func pgType(f domain.FieldDef) (string, error) {
	switch f.Type {
	case domain.FieldText, domain.FieldFile:
		// FieldFile menyimpan storage key (objek nyata di object storage).
		return "TEXT", nil
	case domain.FieldDate:
		return "DATE", nil
	case domain.FieldDateTime:
		return "TIMESTAMPTZ", nil
	case domain.FieldInt:
		return "BIGINT", nil
	case domain.FieldDecimal:
		return fmt.Sprintf("NUMERIC(20, %d)", f.Precision), nil
	case domain.FieldBool:
		return "BOOLEAN", nil
	case domain.FieldEnum:
		return "VARCHAR(64)", nil
	case domain.FieldLink:
		return "UUID", nil
	case domain.FieldJSON:
		return "JSONB", nil
	default:
		return "", fmt.Errorf("field %q: tipe %q tidak punya pemetaan DDL", f.Name, f.Type)
	}
}

func indexName(table, col string) string {
	// table = "schema.tabel"; pakai bagian tabel saja agar nama index ringkas.
	base := table
	if i := strings.LastIndex(table, "."); i >= 0 {
		base = table[i+1:]
	}
	return fmt.Sprintf("idx_%s_%s", base, col)
}
