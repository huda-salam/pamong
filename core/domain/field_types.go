package domain

import "fmt"

// FieldType adalah tipe data field dalam definisi entity.
type FieldType string

const (
	FieldText     FieldType = "Text"
	FieldDate     FieldType = "Date"
	FieldDateTime FieldType = "DateTime"
	FieldInt      FieldType = "Integer"
	FieldDecimal  FieldType = "Decimal"
	FieldBool     FieldType = "Boolean"
	FieldEnum     FieldType = "Enum"
	FieldLink     FieldType = "Link"
	FieldFile     FieldType = "File"
	FieldJSON     FieldType = "JSON"
)

// validFieldTypes adalah himpunan tipe field yang dikenal.
var validFieldTypes = map[FieldType]bool{
	FieldText: true, FieldDate: true, FieldDateTime: true, FieldInt: true,
	FieldDecimal: true, FieldBool: true, FieldEnum: true, FieldLink: true,
	FieldFile: true, FieldJSON: true,
}

// reservedFieldNames adalah kolom standar yang dikelola framework — modul tidak boleh
// mendefinisikannya ulang (PRD F2). Framework menambahkannya otomatis saat generation.
var reservedFieldNames = map[string]bool{
	"id": true, "version": true, "created_at": true, "updated_at": true, "deleted_at": true,
}

// FieldDef adalah definisi satu field entity.
type FieldDef struct {
	Name      string
	Type      FieldType
	Required  bool
	Unique    bool
	Default   *string  // nil = tanpa default
	Options   []string // untuk FieldEnum
	LinkTo    string   // untuk FieldLink: "{modul}.{Entity}"
	MaxSizeMB int      // untuk FieldFile
	Precision int      // untuk FieldDecimal (jumlah digit di belakang koma)
}

// Validate memeriksa invariant struktural satu field (PRD F2). Pesan menyebut nama
// field agar mudah ditelusuri.
func (f FieldDef) Validate() error {
	if f.Name == "" {
		return fmt.Errorf("ada field tanpa nama")
	}
	if reservedFieldNames[f.Name] {
		return fmt.Errorf("field %q: nama reserved, dikelola framework — jangan didefinisikan ulang", f.Name)
	}
	if !validFieldTypes[f.Type] {
		return fmt.Errorf("field %q: tipe %q tidak dikenal", f.Name, f.Type)
	}
	switch f.Type {
	case FieldEnum:
		if len(f.Options) == 0 {
			return fmt.Errorf("field %q (Enum): Options wajib diisi", f.Name)
		}
	case FieldLink:
		if f.LinkTo == "" {
			return fmt.Errorf("field %q (Link): LinkTo wajib diisi (format modul.Entity)", f.Name)
		}
	case FieldDecimal:
		if f.Precision <= 0 {
			return fmt.Errorf("field %q (Decimal): Precision wajib > 0", f.Name)
		}
	}
	return nil
}
