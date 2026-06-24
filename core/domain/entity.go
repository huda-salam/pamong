package domain

// EntityTier menentukan seberapa banyak framework menghasilkan kode vs modul menulis sendiri.
// Tier 1: generated penuh. Tier 2: generated + hook. Tier 3: hexagonal penuh.
type EntityTier int

const (
	Tier1 EntityTier = 1
	Tier2 EntityTier = 2
	Tier3 EntityTier = 3
)

// FieldType adalah tipe data field dalam definisi entity.
type FieldType string

const (
	FieldText     FieldType = "Text"
	FieldDate     FieldType = "Date"
	FieldDateTime FieldType = "DateTime"
	FieldInt      FieldType = "Int"
	FieldDecimal  FieldType = "Decimal"
	FieldBool     FieldType = "Bool"
	FieldEnum     FieldType = "Enum"
	FieldLink     FieldType = "Link"
	FieldFile     FieldType = "File"
	FieldJSON     FieldType = "JSON"
)

// Audited menandai entity sebagai auditable: framework menulis audit log
// setiap kali entity dimutasi. Harus dideklarasikan eksplisit — tidak ada default.
type Audited struct{}

// Lockable menandai entity sebagai terkunci oleh periode fiskal.
// FiscalField adalah nama kolom yang dipakai untuk menentukan periode.
type Lockable struct {
	FiscalField string
}

// FieldDef adalah definisi satu field entity.
type FieldDef struct {
	Name     string
	Type     FieldType
	Required bool
	Unique   bool
	Options  []string // untuk FieldEnum
	LinkTo   string   // untuk FieldLink: "{modul}.{Entity}"
}

// EntityDef adalah definisi lengkap sebuah entity.
// Diisi di manifest modul; dibaca registry framework untuk generate tabel, endpoint, dsb.
type EntityDef struct {
	Name      string
	Schema    string
	Tablename string
	Tier      EntityTier

	// Kedua field ini WAJIB dideklarasikan eksplisit (CODING_PHILOSOPHY #3).
	// Zero value dari Audited{} berarti "tidak diaudit" — dideklarasikan None.
	Audit    Audited
	Lockable Lockable

	HasAttachments bool
	Searchable     []string
	Fields         []FieldDef
}

// ConfigSpec mendefinisikan konfigurasi tambahan yang dibutuhkan modul.
// Nilai diambil dari env GOV_MODULE_{MODUL}_{KEY} atau UI admin tenant.
type ConfigSpec struct {
	Fields []ConfigField
}

// ConfigField adalah satu entri konfigurasi modul.
type ConfigField struct {
	Key      string
	Type     string
	Default  string
	Required bool
}
