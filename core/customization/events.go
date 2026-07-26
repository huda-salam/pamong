package customization

// Nama event kustomisasi tenant (CLAUDE.md §Event: {modul}.{entity}.{kejadian_past_tense}).
// Dipublish oleh Manager setelah operasi tulis sukses; selain jejak, event ini adalah sinyal
// INVALIDASI cache merge per-tenant (PRD dependency: "Event bus — invalidasi cache merge saat
// kustomisasi berubah"). Konsumen cache men-subscribe event ini untuk membuang entri (tenant,
// module, entity) yang basi.
const (
	EventCustomFieldDitambahkan   = "customization.custom_field.ditambahkan"
	EventCustomFieldDinonaktifkan = "customization.custom_field.dinonaktifkan"
	EventLabelDiubah              = "customization.label.diubah"
	EventCapabilityDiubah         = "customization.capability.diubah"
)

// FieldChangedPayload menyertai event yang menyasar satu field entity — custom field
// (tambah/nonaktif) maupun label override. Cukup untuk invalidasi cache merge tanpa
// membocorkan definisi/nilai field.
type FieldChangedPayload struct {
	TenantID  string `json:"tenant_id"`
	Module    string `json:"module"`
	Entity    string `json:"entity"`
	FieldName string `json:"field_name"`
}

// CapabilityChangedPayload menyertai EventCapabilityDiubah.
type CapabilityChangedPayload struct {
	TenantID   string `json:"tenant_id"`
	Capability string `json:"capability"`
	Enabled    bool   `json:"enabled"`
}

// EventSchemaRegistrar adalah subset eventbus.SchemaRegistry (metode Register) — seam agar
// core/customization tak mengimport infra. eventbus.SchemaRegistry memenuhinya.
type EventSchemaRegistrar interface {
	Register(name string, schema any) error
}

// RegisterEventSchemas mendaftarkan schema payload keempat event kustomisasi ke registry event
// bus. **WAJIB dipanggil saat wiring** (mis. `customization.RegisterEventSchemas(bus.Schema())`):
// core/customization BUKAN Module ber-manifest, jadi tak ada registrasi otomatis via
// EventManifest.Produces. Tanpa ini, eventbus.Bus.Publish menolak event ("event tak terdaftar")
// dan setiap operasi tulis Manager gagal di langkah publish. Idempoten (register ulang tipe sama
// diizinkan registry).
//
// DEFERRED(Phase-5.1.1): pemanggil produksi belum ada (cmd/server/main.go stub) — lihat ROADMAP
// §Backlog "[Phase-5.1.1] Live wiring customization write-path".
func RegisterEventSchemas(r EventSchemaRegistrar) error {
	schemas := []struct {
		name    string
		payload any
	}{
		{EventCustomFieldDitambahkan, FieldChangedPayload{}},
		{EventCustomFieldDinonaktifkan, FieldChangedPayload{}},
		{EventLabelDiubah, FieldChangedPayload{}},
		{EventCapabilityDiubah, CapabilityChangedPayload{}},
	}
	for _, s := range schemas {
		if err := r.Register(s.name, s.payload); err != nil {
			return err
		}
	}
	return nil
}
