package customization

import "github.com/huda-salam/pamong/core/domain"

// EntityLookup mengembalikan definisi entity INTI berdasarkan (module, entity). Dipakai untuk
// menegakkan bahwa custom field menargetkan entity yang benar-benar ada dan tidak bentrok nama
// dengan field intinya. RegistryEntityLookup mengadaptasi *domain.Registry; test memakai fake.
type EntityLookup interface {
	Entity(module, entity string) (domain.EntityDef, bool)
}

// RegistryEntityLookup mengadaptasi *domain.Registry ke EntityLookup tanpa mengubah domain:
// module = nama modul terdaftar, entity = nama EntityDef di manifest modul itu.
type RegistryEntityLookup struct {
	Registry *domain.Registry
}

var _ EntityLookup = RegistryEntityLookup{}

// Entity mengimplementasi EntityLookup.
func (l RegistryEntityLookup) Entity(module, entity string) (domain.EntityDef, bool) {
	if l.Registry == nil {
		return domain.EntityDef{}, false
	}
	m, ok := l.Registry.Get(module)
	if !ok {
		return domain.EntityDef{}, false
	}
	for _, e := range m.Manifest().Entities {
		if e.Name == entity {
			return e, true
		}
	}
	return domain.EntityDef{}, false
}

// ValidateAgainstBase memeriksa custom field terhadap definisi entity INTI: namanya tidak boleh
// bentrok dengan field inti manapun (reserved sudah ditolak lebih dulu oleh FieldDef.Validate).
// Ini melengkapi CustomFieldDef.Validate (yang tak tahu field inti). Dipakai use case admin.
func ValidateAgainstBase(base domain.EntityDef, def CustomFieldDef) error {
	for _, f := range base.Fields {
		if f.Name == def.Field.Name {
			return ErrCustomFieldExists(def.Field.Name)
		}
	}
	return nil
}

// MergeEntity menghasilkan EntityDef efektif = definisi inti + custom field tenant yang aktif
// (PRD F1). Murni & deterministik (tak menyentuh DB) sehingga bisa diuji tanpa koneksi dan
// hasilnya bisa di-cache per (tenant, module, entity). Definisi inti TIDAK dimutasi — dikembalikan
// salinan dengan Fields yang sudah digabung. Custom field yang bentrok nama dengan field inti
// DILEWATI secara defensif (jalur tulis menolaknya lebih dulu via ValidateAgainstBase).
//
// Urutan: field inti dipertahankan; tiap custom disisipkan tepat SESUDAH field ber-nama
// InsertAfter (inti maupun custom yang sudah tersisip). InsertAfter kosong / tak ditemukan →
// custom ditempatkan di akhir, mengikuti urutan List dari store (deterministik).
func MergeEntity(base domain.EntityDef, customs []CustomFieldDef) domain.EntityDef {
	present := make(map[string]bool, len(base.Fields))
	for _, f := range base.Fields {
		present[f.Name] = true
	}

	merged := make([]domain.FieldDef, len(base.Fields))
	copy(merged, base.Fields)

	// Field dengan anchor konkret yang belum hadir ditunda; anchor bisa custom lain yang belum
	// tersisip (rantai InsertAfter). Iterasi sampai FIXPOINT — satu pass tak cukup untuk rantai
	// berjenjang yang urutan input-nya (List = alfabetis) berkebalikan dengan urutan dependency.
	pending := make([]CustomFieldDef, 0, len(customs))
	for _, c := range customs {
		if !c.IsActive || present[c.Field.Name] {
			continue // nonaktif atau bentrok nama (defensif) → lewati
		}
		if c.InsertAfter == "" {
			merged = append(merged, c.Field)
			present[c.Field.Name] = true
			continue
		}
		pending = append(pending, c)
	}

	for len(pending) > 0 {
		progressed := false
		rest := pending[:0]
		for _, c := range pending {
			if idx := indexOf(merged, c.InsertAfter); idx >= 0 {
				merged = insertAt(merged, idx+1, c.Field)
				present[c.Field.Name] = true
				progressed = true
			} else {
				rest = append(rest, c) // anchor belum hadir → coba lagi putaran berikutnya
			}
		}
		pending = rest
		if !progressed {
			break // anchor tak akan pernah hadir (tak dikenal / siklus) → sisanya ke akhir
		}
	}
	// Anchor tak terselesaikan (tak dikenal / rantai putus) → tempatkan di akhir, urutan stabil.
	for _, c := range pending {
		merged = append(merged, c.Field)
		present[c.Field.Name] = true
	}

	base.Fields = merged
	return base
}

// indexOf mengembalikan posisi field ber-nama name, atau -1.
func indexOf(fields []domain.FieldDef, name string) int {
	for i, f := range fields {
		if f.Name == name {
			return i
		}
	}
	return -1
}

// insertAt menyisipkan f pada posisi i (menggeser sisanya), tanpa merusak slice sumber.
func insertAt(fields []domain.FieldDef, i int, f domain.FieldDef) []domain.FieldDef {
	out := make([]domain.FieldDef, 0, len(fields)+1)
	out = append(out, fields[:i]...)
	out = append(out, f)
	out = append(out, fields[i:]...)
	return out
}
