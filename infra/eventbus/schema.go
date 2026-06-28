package eventbus

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sync"

	"github.com/huda-salam/pamong/core"
)

// SchemaRegistry menyimpan tipe payload yang sah untuk tiap nama event. Ia adalah
// gerbang yang menutup vektor "event tanpa schema": hanya event yang terdaftar yang
// boleh dipublikasikan, dan payload-nya wajib bertipe sama dengan yang didaftarkan
// (PRD eventbus F2). Registry sengaja lepas dari core/domain — ia cuma tahu
// nama→tipe; helper pengisi-dari-manifest (EventManifest.Produces) menyusul saat
// wiring, agar driver lain (NATS/Redis) memakai registry yang sama.
type SchemaRegistry struct {
	mu     sync.RWMutex
	schema map[string]reflect.Type
}

// NewSchemaRegistry membuat registry kosong.
func NewSchemaRegistry() *SchemaRegistry {
	return &SchemaRegistry{schema: make(map[string]reflect.Type)}
}

// Register mendaftarkan nama event beserta contoh struct payload-nya. Tipe payload
// disimpan ter-normalisasi (pointer di-deref) sehingga *T dan T diperlakukan sama.
// Mendaftar ulang nama yang sama dengan tipe berbeda ditolak — perubahan schema
// adalah versi baru, bukan penimpaan diam-diam.
func (r *SchemaRegistry) Register(name string, schema any) error {
	if name == "" {
		return core.ErrValidation("event", "nama event kosong")
	}
	if schema == nil {
		return core.ErrValidation("event", fmt.Sprintf("schema event %q nil", name))
	}
	t := normalize(reflect.TypeOf(schema))

	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.schema[name]; ok && existing != t {
		return core.ErrConflict(fmt.Sprintf(
			"event %q sudah terdaftar dengan tipe %s, tidak boleh diubah ke %s", name, existing, t))
	}
	r.schema[name] = t
	return nil
}

// Validate menolak event yang namanya tidak terdaftar atau payload-nya bertipe
// berbeda dari schema terdaftar (PRD eventbus F2). Dipanggil Bus sebelum dispatch.
func (r *SchemaRegistry) Validate(name string, payload any) error {
	r.mu.RLock()
	want, ok := r.schema[name]
	r.mu.RUnlock()
	if !ok {
		return core.ErrValidation("event", fmt.Sprintf("event %q tidak terdaftar di schema registry", name))
	}
	got := normalize(reflect.TypeOf(payload))
	if got != want {
		return core.ErrValidation("payload", fmt.Sprintf(
			"payload event %q bertipe %s, schema mengharapkan %s", name, typeName(got), want))
	}
	return nil
}

// normalize meng-deref tipe pointer agar *T dan T setara saat pencocokan.
func normalize(t reflect.Type) reflect.Type {
	for t != nil && t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t
}

// Unmarshal membuat instance baru dari tipe payload terdaftar untuk event `name`
// lalu meng-unmarshal `data` JSON ke dalamnya. Dipakai relay untuk merekonstruksi
// payload bertype-safe dari JSONB outbox. Mengembalikan pointer ke nilai baru.
func (r *SchemaRegistry) Unmarshal(name string, data []byte) (any, error) {
	r.mu.RLock()
	t, ok := r.schema[name]
	r.mu.RUnlock()
	if !ok {
		return nil, core.ErrValidation("event", fmt.Sprintf("event %q tidak terdaftar di schema registry", name))
	}
	v := reflect.New(t).Interface() // *T agar json.Unmarshal bisa populate field
	if err := json.Unmarshal(data, v); err != nil {
		return nil, fmt.Errorf("unmarshal payload event %q: %w", name, err)
	}
	// Dereference ke T agar type assertion subscriber konsisten dengan jalur publish langsung.
	return reflect.ValueOf(v).Elem().Interface(), nil
}

func typeName(t reflect.Type) string {
	if t == nil {
		return "nil"
	}
	return t.String()
}
