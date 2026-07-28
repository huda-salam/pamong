package customization

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core/domain"
)

// DataClass di-kanonik-kan di core/domain (ADR-009 §1); customization me-reuse-nya lewat alias
// agar tak ada dua tipe klasifikasi kembar yang bisa divergen. Alias menjaga call-site lama
// (ClassInternal, dst) tetap kompilasi. Custom field tenant WAJIB menyatakan class (default aman
// `internal`) supaya field ber-pengenal (NIK/NIP/no rekening) tak diam-diam disimpan `public`.
//
// DEFERRED(PR-3.8.3): penegakan perilaku (enkripsi at-rest + blind index untuk personal_id/
// specific) pada custom field menyusul bersama wiring repository. Di sini class DIDEKLARASIKAN &
// divalidasi nilainya. Catatan: CustomFieldDef.Class (di bawah) masih terpisah dari Field.Class
// (kolom domain.FieldDef yang kini ber-Class) — rekonsiliasi keduanya bagian PR-3.8.3.
type DataClass = domain.DataClass

const (
	ClassPublic     = domain.DataPublic
	ClassInternal   = domain.DataInternal
	ClassPersonal   = domain.DataPersonal
	ClassPersonalID = domain.DataPersonalID
	ClassSpecific   = domain.DataSpecific
)

// DefaultDataClass adalah class default yang aman untuk custom field tanpa deklarasi eksplisit:
// internal (tak ter-expose publik, tak butuh enkripsi). Sengaja BUKAN public (fail-safe).
// Beda dari zero-value domain.FieldDef (public): custom field tenant lebih konservatif.
const DefaultDataClass = domain.DataInternal

// CustomFieldDef adalah satu field yang DITAMBAHKAN tenant ke entity modul inti tanpa mengubah
// kode modul (PRD F1). Hidup di layer kustomisasi terpisah (gov.tenant_custom_fields); di-merge
// dengan EntityDef inti saat runtime oleh MergeEntity — upgrade modul tak menimpanya (F4).
//
// Field membungkus domain.FieldDef inti (bukan menduplikasi aturan tipe): custom field tunduk
// pada validasi tipe yang sama (Enum wajib Options, Decimal wajib Precision, nama reserved
// ditolak) lewat FieldDef.Validate.
type CustomFieldDef struct {
	TenantID string
	Module   string // modul target, mis. "surat_masuk"
	Entity   string // nama entity target, mis. "SuratMasuk"

	Field domain.FieldDef // definisi field (nama, tipe, required, dst) — reuse aturan inti
	Class DataClass       // klasifikasi data; kosong → DefaultDataClass saat Normalize

	// InsertAfter adalah nama field (inti atau custom) yang custom ini muncul SESUDAHnya di
	// form; "" = ditempatkan di akhir. Hanya memengaruhi urutan tampil, bukan storage.
	InsertAfter string

	IsActive  bool
	CreatedBy uuid.UUID
	CreatedAt time.Time
}

// Normalize mengisi default yang aman sebelum validasi/simpan (Class kosong → internal).
func (c CustomFieldDef) Normalize() CustomFieldDef {
	if c.Class == "" {
		c.Class = DefaultDataClass
	}
	return c
}

// Validate memeriksa invarian struktural custom field: target lengkap, class dikenal, dan
// FieldDef inti valid (termasuk nama reserved & aturan per-tipe). Collision dengan field INTI
// entity tidak dicek di sini (butuh definisi entity) — lihat ValidateAgainstBase.
func (c CustomFieldDef) Validate() error {
	if c.TenantID == "" {
		return ErrCustomFieldInvalid("", "tenant_id wajib diisi")
	}
	if c.Module == "" {
		return ErrCustomFieldInvalid(c.Field.Name, "module wajib diisi")
	}
	if c.Entity == "" {
		return ErrCustomFieldInvalid(c.Field.Name, "entity wajib diisi")
	}
	class := c.Class
	if class == "" {
		class = DefaultDataClass
	}
	if !class.Valid() {
		return ErrCustomFieldInvalid(c.Field.Name, "class "+string(class)+" tidak dikenal")
	}
	if err := c.Field.Validate(); err != nil {
		return err
	}
	return nil
}

// CustomFieldStore adalah driven port penyimpanan custom field per-tenant. Pluggable: Memory
// untuk test/bootstrap, adapter Postgres (gov.tenant_custom_fields) untuk produksi. Store hanya
// menyimpan & menolak duplikat nama antar custom field aktif pada (tenant, module, entity);
// collision dengan field inti ditegakkan di lapis use case (butuh definisi entity).
type CustomFieldStore interface {
	// List mengembalikan custom field AKTIF untuk (tenant, module, entity), terurut deterministik.
	List(ctx context.Context, tenantID, module, entity string) ([]CustomFieldDef, error)
	// Save menambah/menimpa satu custom field. Menolak bila nama bentrok dengan custom field
	// aktif lain pada scope yang sama (ErrCustomFieldExists).
	Save(ctx context.Context, def CustomFieldDef) error
	// Deactivate menandai custom field non-aktif (soft): tak lagi di-merge, tapi data lama tetap.
	Deactivate(ctx context.Context, tenantID, module, entity, fieldName string) error
}

// MemoryCustomFieldStore adalah CustomFieldStore in-memory untuk test/bootstrap. BUKAN jalur
// produksi ber-audit — itu adapter Postgres.
type MemoryCustomFieldStore struct {
	mu   sync.RWMutex
	defs map[string][]CustomFieldDef // key: cfKey(tenant, module, entity)
}

// NewMemoryCustomFieldStore membuat store kosong.
func NewMemoryCustomFieldStore() *MemoryCustomFieldStore {
	return &MemoryCustomFieldStore{defs: make(map[string][]CustomFieldDef)}
}

var _ CustomFieldStore = (*MemoryCustomFieldStore)(nil)

// List mengimplementasi CustomFieldStore: hanya field aktif, terurut nama.
func (s *MemoryCustomFieldStore) List(_ context.Context, tenantID, module, entity string) ([]CustomFieldDef, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	src := s.defs[cfKey(tenantID, module, entity)]
	out := make([]CustomFieldDef, 0, len(src))
	for _, d := range src {
		if d.IsActive {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Field.Name < out[j].Field.Name })
	return out, nil
}

// Save mengimplementasi CustomFieldStore: menimpa entri dengan nama sama, atau menambah baru.
// Menolak bila nama bentrok dengan custom field AKTIF lain (bukan dirinya sendiri).
func (s *MemoryCustomFieldStore) Save(_ context.Context, def CustomFieldDef) error {
	def = def.Normalize()
	if err := def.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := cfKey(def.TenantID, def.Module, def.Entity)
	list := s.defs[k]
	for i, d := range list {
		if d.Field.Name == def.Field.Name {
			list[i] = def // timpa (termasuk reaktivasi)
			s.defs[k] = list
			return nil
		}
	}
	s.defs[k] = append(list, def)
	return nil
}

// Deactivate mengimplementasi CustomFieldStore.
func (s *MemoryCustomFieldStore) Deactivate(_ context.Context, tenantID, module, entity, fieldName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := cfKey(tenantID, module, entity)
	list := s.defs[k]
	for i, d := range list {
		if d.Field.Name == fieldName {
			list[i].IsActive = false
			s.defs[k] = list
			return nil
		}
	}
	return ErrCustomFieldNotFound(fieldName)
}

// cfKey menggabungkan tenant+module+entity dengan pemisah NUL (tak mungkin muncul di nama).
func cfKey(tenantID, module, entity string) string {
	return tenantID + "\x00" + module + "\x00" + entity
}
