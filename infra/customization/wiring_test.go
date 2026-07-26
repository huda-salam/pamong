package customization_test

import (
	"context"
	"testing"

	"github.com/huda-salam/pamong/core/config"
	coreCust "github.com/huda-salam/pamong/core/customization"
	"github.com/huda-salam/pamong/core/domain"
	"github.com/huda-salam/pamong/infra/eventbus"
	"github.com/huda-salam/pamong/port"
	"github.com/huda-salam/pamong/testkit"
)

// fakeLookup: EntityLookup dari satu entity inti untuk test wiring.
type fakeLookup struct{ e domain.EntityDef }

func (l fakeLookup) Entity(module, entity string) (domain.EntityDef, bool) {
	if module == l.e.Schema && entity == l.e.Name {
		return l.e, true
	}
	return domain.EntityDef{}, false
}

func baseEntity() domain.EntityDef {
	return domain.EntityDef{
		Name: "SuratMasuk", Schema: "surat_masuk", Tier: domain.Tier1,
		Audit: domain.Audited{}, Lockable: domain.NotLockable{},
		Fields: []domain.FieldDef{{Name: "nomor", Type: domain.FieldText}},
	}
}

func newManager(t *testing.T, pub port.EventPublisher) *coreCust.Manager {
	t.Helper()
	reg := coreCust.NewCapabilityRegistry()
	return coreCust.NewManager(
		coreCust.NewMemoryCustomFieldStore(),
		config.NewMemoryTenantConfigStore(),
		coreCust.NewMemoryTenantCapabilityStore(),
		reg,
		fakeLookup{baseEntity()},
		pub,
	)
}

func adminCtx(t *testing.T) *testkit.TestContext {
	return testkit.NewContext(t,
		testkit.WithTenant("pemkot-surabaya"),
		testkit.WithPermission(coreCust.PermCustomFieldBuat),
	)
}

func createField(m *coreCust.Manager, ctx port.AuthContext) error {
	return m.CreateCustomField(ctx, coreCust.CustomFieldDef{
		Module: "surat_masuk", Entity: "SuratMasuk",
		Field: domain.FieldDef{Name: "catatan", Type: domain.FieldText},
	})
}

// TestWiring_RegisteredSchemasAllowPublish: dengan RegisterEventSchemas dipanggil, Manager
// memakai eventbus.Bus ASLI dan write sukses + event benar-benar terkirim ke subscriber.
func TestWiring_RegisteredSchemasAllowPublish(t *testing.T) {
	bus := eventbus.NewMemory()
	if err := coreCust.RegisterEventSchemas(bus.Schema()); err != nil {
		t.Fatalf("RegisterEventSchemas: %v", err)
	}
	got := make(chan port.Event, 1)
	if err := bus.Subscribe(coreCust.EventCustomFieldDitambahkan, func(_ context.Context, e port.Event) error {
		got <- e
		return nil
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	m := newManager(t, bus)
	if err := createField(m, adminCtx(t)); err != nil {
		t.Fatalf("CreateCustomField dengan schema terdaftar harus sukses: %v", err)
	}
	select {
	case e := <-got:
		if p, ok := e.Payload.(coreCust.FieldChangedPayload); !ok || p.FieldName != "catatan" {
			t.Fatalf("payload event salah: %+v", e.Payload)
		}
	default:
		t.Fatal("event tak terkirim ke subscriber")
	}
}

// TestWiring_UnregisteredSchemasBlockWrite: TANPA RegisterEventSchemas, Bus.Publish menolak event
// → CreateCustomField gagal di langkah publish (membuktikan seam registrasi wajib saat wiring).
func TestWiring_UnregisteredSchemasBlockWrite(t *testing.T) {
	bus := eventbus.NewMemory() // schema kosong, sengaja tak RegisterEventSchemas
	m := newManager(t, bus)
	if err := createField(m, adminCtx(t)); err == nil {
		t.Fatal("tanpa registrasi schema, publish harus menolak → write gagal")
	}
}
