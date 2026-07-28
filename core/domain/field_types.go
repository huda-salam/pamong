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

// DataClass adalah sumbu klasifikasi data satu field (ADR-009 §1). Ia menurunkan perilaku
// enkripsi, audit diff, log/trace, dan export lewat tabel kebijakan framework — BUKAN flag
// per-concern (Encrypted/Sensitive/NoLog) yang mudah salah dikombinasikan. Nilai zero ("")
// diperlakukan sebagai DataPublic. Ini tipe kanonik; core/customization me-reuse-nya.
type DataClass string

const (
	DataPublic     DataClass = "public"      // default (zero value diperlakukan sebagai ini)
	DataInternal   DataClass = "internal"    // internal, non-publik
	DataPersonal   DataClass = "personal"    // PDP umum: nama, alamat, jabatan — TIDAK dienkripsi (harus dapat dicari)
	DataPersonalID DataClass = "personal_id" // pengenal unik: NIK, NIP, no_hp, email, no rekening — enc + blind index
	DataSpecific   DataClass = "specific"    // PDP spesifik: kesehatan, keuangan pribadi, biometrik — enc, DEK terpisah
)

// validDataClasses adalah himpunan class yang dikenal.
var validDataClasses = map[DataClass]bool{
	DataPublic: true, DataInternal: true, DataPersonal: true,
	DataPersonalID: true, DataSpecific: true,
}

// Normalize memetakan zero value ("") ke DataPublic; nilai lain dikembalikan apa adanya.
func (c DataClass) Normalize() DataClass {
	if c == "" {
		return DataPublic
	}
	return c
}

// Valid melaporkan apakah class dikenal (setelah normalisasi "" → public).
func (c DataClass) Valid() bool { return validDataClasses[c.Normalize()] }

// IsEncrypted melaporkan apakah field ber-class ini dienkripsi at-rest (ADR-009 §1): HANYA
// personal_id & specific. Nama/alamat (personal) TIDAK — harus dapat dicari; enkripsi selektif,
// bukan "semua PII". Enkripsi transparan digerakkan flag ini di lapis repository (PR-3.8.3).
func (c DataClass) IsEncrypted() bool {
	switch c.Normalize() {
	case DataPersonalID, DataSpecific:
		return true
	default:
		return false
	}
}

// reservedFieldNames adalah kolom standar yang dikelola framework — modul tidak boleh
// mendefinisikannya ulang (PRD F2). Framework mengisinya otomatis (system-managed): tidak
// pernah assignable dari UI/modul, hanya diset dari state sistem. created_by/updated_by
// direserve lebih dulu meski kolom actor-nya belum di-generate DDL — mencegah modul memakai
// nama itu sebagai field bisnis sebelum framework menambah actor-tracking (lihat DDL note).
var reservedFieldNames = map[string]bool{
	"id": true, "version": true, "created_at": true, "updated_at": true, "deleted_at": true,
	"created_by": true, "updated_by": true,
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

	// Class mengklasifikasikan data field (ADR-009). Zero value → DataPublic. Untuk class
	// terenkripsi (personal_id/specific) framework memetakan field ke DUA kolom fisik
	// ({field}_enc + {field}_bidx), bukan satu; enkripsi/dekripsi otomatis di lapis
	// repository (PR-3.8.3), bukan dipanggil use case.
	Class DataClass

	// Searchable menandai field TERENKRIPSI tetap mendukung equality lookup / UNIQUE lewat
	// blind index (kolom {field}_bidx, HMAC deterministik). Hanya bermakna untuk field
	// terenkripsi bertipe Text. Field terenkripsi kehilangan range/sort/partial (LIKE) —
	// konsekuensi diterima (ADR-009 §2). Pencarian teks biasa (plaintext ILIKE) memakai
	// EntityDef.Searchable, bukan flag ini.
	Searchable bool
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
	if !f.Class.Valid() {
		return fmt.Errorf("field %q: class %q tidak dikenal", f.Name, f.Class)
	}
	// Aturan enkripsi/blind-index (ADR-009 §5). Ditegakkan di pintu masuk agar DDL yang
	// mustahil ditegakkan (mis. UNIQUE atas ciphertext GCM nonce-acak) gagal saat validasi,
	// bukan runtime.
	if f.Searchable && f.Type != FieldText {
		return fmt.Errorf("field %q: Searchable hanya untuk tipe Text (blind index equality); tipe %q tak didukung", f.Name, f.Type)
	}
	if f.Searchable && !f.Class.IsEncrypted() {
		return fmt.Errorf("field %q: Searchable hanya bermakna untuk field terenkripsi (personal_id/specific); pencarian teks biasa pakai EntityDef.Searchable", f.Name)
	}
	if f.Class.IsEncrypted() {
		if f.Unique && !f.Searchable {
			return fmt.Errorf("field %q: Unique pada field terenkripsi wajib Searchable=true (UNIQUE ditegakkan lewat blind index {field}_bidx, bukan {field}_enc)", f.Name)
		}
		if f.Default != nil {
			return fmt.Errorf("field %q: field terenkripsi tak boleh punya Default (literal plaintext tak bisa jadi default kolom ciphertext)", f.Name)
		}
		if f.Type == FieldEnum || f.Type == FieldLink {
			return fmt.Errorf("field %q: tipe %q tak boleh dienkripsi (CHECK/FK mustahil atas ciphertext)", f.Name, f.Type)
		}
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
