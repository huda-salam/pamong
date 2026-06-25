package domain

import (
	"fmt"
	"strings"
)

// EntityTier menentukan seberapa banyak framework menghasilkan kode vs modul menulis sendiri.
// Tier 1: generated penuh. Tier 2: generated + hook. Tier 3: hexagonal penuh.
type EntityTier int

const (
	Tier1 EntityTier = 1
	Tier2 EntityTier = 2
	Tier3 EntityTier = 3
)

// --- Kebijakan audit (WAJIB eksplisit) ---
//
// AuditPolicy sengaja dibuat sebagai interface tertutup tanpa zero value valid: field
// EntityDef.Audit yang nil = lupa diisi = ditolak saat boot. Ini mencegah "default
// diam-diam" yang berbahaya untuk sistem pemerintahan (CODING_PHILOSOPHY #3).
type AuditPolicy interface{ isAuditPolicy() }

// Audited menandai entity diaudit penuh (before/after, actor, waktu).
type Audited struct{}

func (Audited) isAuditPolicy() {}

// NotAudited menandai entity tidak diaudit — Reason WAJIB diisi agar keputusan sadar
// dan tercatat (bukan kelalaian).
type NotAudited struct{ Reason string }

func (NotAudited) isAuditPolicy() {}

// --- Kebijakan penguncian periode fiskal (WAJIB eksplisit) ---

// LockPolicy interface tertutup; nil = lupa diisi = ditolak saat boot.
type LockPolicy interface{ isLockPolicy() }

// Lockable menandai entity terkunci oleh periode fiskal berdasarkan FiscalField.
type Lockable struct{ FiscalField string }

func (Lockable) isLockPolicy() {}

// NotLockable menandai entity tidak terikat periode fiskal.
type NotLockable struct{}

func (NotLockable) isLockPolicy() {}

// EntityDef adalah definisi lengkap sebuah entity. Diisi di manifest modul; dibaca
// registry untuk generate tabel, endpoint, permission, audit, dan fiscal check.
type EntityDef struct {
	Name      string
	Schema    string // schema Postgres = nama modul
	Tablename string // {schema}.{entity_plural}
	Tier      EntityTier

	// Audit & Lockable WAJIB dideklarasikan eksplisit (tidak ada zero value valid).
	Audit    AuditPolicy
	Lockable LockPolicy

	HasAttachments bool
	Searchable     []string
	Fields         []FieldDef
	Hooks          HookSet
}

// TableName mengembalikan nama tabel efektif entity: Tablename eksplisit bila diisi,
// atau nama kanonik {schema}.{plural} yang diturunkan dari Schema + Name bila kosong.
// Validate menjamin keduanya identik bila Tablename diisi, jadi keduanya konsisten.
func (e EntityDef) TableName() string {
	if e.Tablename != "" {
		return e.Tablename
	}
	if e.Schema == "" || e.Name == "" {
		return ""
	}
	return DeriveTableName(e.Schema, e.Name)
}

// Validate memeriksa invariant struktural EntityDef (PRD F2). Dipanggil registry saat boot.
func (e EntityDef) Validate() error {
	var errs []string

	if e.Name == "" {
		errs = append(errs, "Name kosong")
	}
	if e.Schema == "" {
		errs = append(errs, "Schema kosong")
	}
	// Tablename harus persis sama dengan hasil derivasi kanonik {schema}.{plural}.
	// Tablename boleh dikosongkan (akan diisi otomatis oleh ResolveTableName); jika
	// diisi manual, wajib cocok agar tidak ada nama tabel "kreatif" yang lolos.
	if e.Schema != "" && e.Name != "" {
		want := DeriveTableName(e.Schema, e.Name)
		if e.Tablename != "" && e.Tablename != want {
			errs = append(errs, fmt.Sprintf("Tablename %q tidak sesuai konvensi; harus %q", e.Tablename, want))
		}
	}
	if e.Tier < Tier1 || e.Tier > Tier3 {
		errs = append(errs, fmt.Sprintf("Tier %d tidak valid (1..3)", e.Tier))
	}

	// Audit & Lockable wajib eksplisit.
	if e.Audit == nil {
		errs = append(errs, "Audit wajib dideklarasikan eksplisit (Audited{} atau NotAudited{Reason})")
	}
	if na, ok := e.Audit.(NotAudited); ok && na.Reason == "" {
		errs = append(errs, "NotAudited wajib menyertakan Reason")
	}
	if e.Lockable == nil {
		errs = append(errs, "Lockable wajib dideklarasikan eksplisit (Lockable{FiscalField} atau NotLockable{})")
	}

	// Field: tidak boleh duplikat nama; tiap field valid.
	seen := make(map[string]bool)
	for _, f := range e.Fields {
		if seen[f.Name] {
			errs = append(errs, fmt.Sprintf("field duplikat: %q", f.Name))
		}
		seen[f.Name] = true
		if err := f.Validate(); err != nil {
			errs = append(errs, err.Error())
		}
	}

	// FiscalField pada Lockable harus menunjuk field yang ada.
	if lk, ok := e.Lockable.(Lockable); ok {
		if lk.FiscalField == "" {
			errs = append(errs, "Lockable.FiscalField kosong")
		} else if !seen[lk.FiscalField] {
			errs = append(errs, fmt.Sprintf("Lockable.FiscalField %q bukan field entity", lk.FiscalField))
		}
	}

	// Searchable harus menunjuk field yang ada.
	for _, s := range e.Searchable {
		if !seen[s] {
			errs = append(errs, fmt.Sprintf("Searchable %q bukan field entity", s))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("entity %q tidak valid:\n    - %s", e.Name, strings.Join(errs, "\n    - "))
	}
	return nil
}

// ConfigSpec mendefinisikan konfigurasi tambahan yang dibutuhkan modul.
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
