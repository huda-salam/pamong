package customization_test

import (
	"errors"
	"testing"

	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/core/config"
	"github.com/huda-salam/pamong/core/customization"
	"github.com/huda-salam/pamong/core/domain"
	"github.com/huda-salam/pamong/testkit"
)

// fakeLookup mengimplementasi customization.EntityLookup dari map, tanpa registry penuh.
type fakeLookup map[string]domain.EntityDef

func (l fakeLookup) Entity(module, entity string) (domain.EntityDef, bool) {
	e, ok := l[module+"."+entity]
	return e, ok
}

// managerEnv merakit Manager lengkap + dependensinya untuk test.
func managerEnv(t *testing.T) (*customization.Manager, *customization.MemoryCustomFieldStore, *customization.MemoryTenantCapabilityStore, *testkit.MockPublisher) {
	t.Helper()
	fields := customization.NewMemoryCustomFieldStore()
	labels := config.NewMemoryTenantConfigStore()
	caps := customization.NewMemoryTenantCapabilityStore()
	reg := customization.NewCapabilityRegistry()
	if err := reg.Register(customization.Capability{Name: "surat.disposisi_massal"}); err != nil {
		t.Fatal(err)
	}
	lookup := fakeLookup{"surat_masuk.SuratMasuk": baseEntity()}
	pub := testkit.NewMockPublisher()
	m := customization.NewManager(fields, labels, caps, reg, lookup, pub)
	return m, fields, caps, pub
}

// adminCtx: konteks dengan semua permission kustomisasi + tenant tertentu.
func adminCtx(t *testing.T, tenant string) *testkit.TestContext {
	return testkit.NewContext(t,
		testkit.WithTenant(tenant),
		testkit.WithPermission(customization.PermCustomFieldBuat),
		testkit.WithPermission(customization.PermCustomFieldHapus),
		testkit.WithPermission(customization.PermLabelUbah),
		testkit.WithPermission(customization.PermCapabilityUbah),
	)
}

func TestCreateCustomField_Success(t *testing.T) {
	m, fields, _, pub := managerEnv(t)
	ctx := adminCtx(t, "pemkot-surabaya")

	def := customization.CustomFieldDef{
		Module: "surat_masuk", Entity: "SuratMasuk", Field: textField("catatan"),
	}
	if err := m.CreateCustomField(ctx, def); err != nil {
		t.Fatalf("CreateCustomField: %v", err)
	}
	// TenantID diambil dari ctx, bukan parameter.
	got, _ := fields.List(ctx, "pemkot-surabaya", "surat_masuk", "SuratMasuk")
	if len(got) != 1 || got[0].TenantID != "pemkot-surabaya" || got[0].CreatedBy != ctx.PersonID() {
		t.Fatalf("field tak tersimpan benar: %+v", got)
	}
	if got[0].Class != customization.ClassInternal {
		t.Errorf("class default harus internal, dapat %q", got[0].Class)
	}
	testkit.AssertEventPublished(t, pub, customization.EventCustomFieldDitambahkan)
}

func TestCreateCustomField_PermissionDenied(t *testing.T) {
	m, _, _, _ := managerEnv(t)
	ctx := testkit.NewContext(t, testkit.WithTenant("t")) // tanpa permission
	err := m.CreateCustomField(ctx, customization.CustomFieldDef{
		Module: "surat_masuk", Entity: "SuratMasuk", Field: textField("catatan"),
	})
	assertCode(t, err, "PERMISSION_DENIED")
}

func TestCreateCustomField_EntityTakDikenal(t *testing.T) {
	m, _, _, _ := managerEnv(t)
	ctx := adminCtx(t, "t")
	err := m.CreateCustomField(ctx, customization.CustomFieldDef{
		Module: "aset", Entity: "Aset", Field: textField("x"),
	})
	assertCode(t, err, "NOT_FOUND")
}

func TestCreateCustomField_BentrokFieldInti(t *testing.T) {
	m, _, _, _ := managerEnv(t)
	ctx := adminCtx(t, "t")
	err := m.CreateCustomField(ctx, customization.CustomFieldDef{
		Module: "surat_masuk", Entity: "SuratMasuk", Field: textField("perihal"), // field inti
	})
	assertCode(t, err, "CONFLICT")
}

func TestCreateCustomField_BentrokCustomAktif(t *testing.T) {
	m, _, _, _ := managerEnv(t)
	ctx := adminCtx(t, "t")
	def := customization.CustomFieldDef{Module: "surat_masuk", Entity: "SuratMasuk", Field: textField("catatan")}
	if err := m.CreateCustomField(ctx, def); err != nil {
		t.Fatal(err)
	}
	// Buat lagi nama sama → conflict.
	err := m.CreateCustomField(ctx, def)
	assertCode(t, err, "CONFLICT")
}

func TestDeactivateCustomField(t *testing.T) {
	m, fields, _, pub := managerEnv(t)
	ctx := adminCtx(t, "t")
	_ = m.CreateCustomField(ctx, customization.CustomFieldDef{Module: "surat_masuk", Entity: "SuratMasuk", Field: textField("catatan")})

	if err := m.DeactivateCustomField(ctx, "surat_masuk", "SuratMasuk", "catatan"); err != nil {
		t.Fatalf("Deactivate: %v", err)
	}
	got, _ := fields.List(ctx, "t", "surat_masuk", "SuratMasuk")
	if len(got) != 0 {
		t.Fatalf("field harus nonaktif, tersisa %+v", got)
	}
	testkit.AssertEventPublished(t, pub, customization.EventCustomFieldDinonaktifkan)
}

func TestDeactivateCustomField_PermissionDenied(t *testing.T) {
	m, _, _, _ := managerEnv(t)
	ctx := testkit.NewContext(t, testkit.WithTenant("t"))
	err := m.DeactivateCustomField(ctx, "surat_masuk", "SuratMasuk", "catatan")
	assertCode(t, err, "PERMISSION_DENIED")
}

func TestSetLabel(t *testing.T) {
	m, _, _, pub := managerEnv(t)
	ctx := adminCtx(t, "pemkot-surabaya")
	if err := m.SetLabel(ctx, "surat_masuk", "SuratMasuk", "perihal", "Hal Surat"); err != nil {
		t.Fatalf("SetLabel: %v", err)
	}
	testkit.AssertEventPublished(t, pub, customization.EventLabelDiubah)
}

func TestSetLabel_PermissionDenied(t *testing.T) {
	m, _, _, _ := managerEnv(t)
	ctx := testkit.NewContext(t, testkit.WithTenant("t"))
	err := m.SetLabel(ctx, "surat_masuk", "SuratMasuk", "perihal", "X")
	assertCode(t, err, "PERMISSION_DENIED")
}

func TestSetCapability_Success(t *testing.T) {
	m, _, caps, pub := managerEnv(t)
	ctx := adminCtx(t, "pemkot-surabaya")
	if err := m.SetCapability(ctx, "surat.disposisi_massal", true); err != nil {
		t.Fatalf("SetCapability: %v", err)
	}
	enabled, ok, _ := caps.Override(ctx, "pemkot-surabaya", "surat.disposisi_massal")
	if !ok || !enabled {
		t.Fatalf("override tak tersimpan: enabled=%v ok=%v", enabled, ok)
	}
	testkit.AssertEventPublished(t, pub, customization.EventCapabilityDiubah)
}

// TestSetCapability_TakTerdaftarFailClosed: override capability tak terdaftar ditolak (fail-closed),
// konsisten dengan CapabilityResolver.IsEnabled.
func TestSetCapability_TakTerdaftarFailClosed(t *testing.T) {
	m, _, _, _ := managerEnv(t)
	ctx := adminCtx(t, "t")
	err := m.SetCapability(ctx, "modul.fitur_hantu", true)
	assertCode(t, err, "NOT_FOUND")
}

func TestSetCapability_PermissionDenied(t *testing.T) {
	m, _, _, _ := managerEnv(t)
	ctx := testkit.NewContext(t, testkit.WithTenant("t"))
	err := m.SetCapability(ctx, "surat.disposisi_massal", true)
	assertCode(t, err, "PERMISSION_DENIED")
}

// TestEffectiveEntity: read-only merge inti + custom aktif.
func TestEffectiveEntity(t *testing.T) {
	m, _, _, _ := managerEnv(t)
	ctx := adminCtx(t, "t")
	_ = m.CreateCustomField(ctx, customization.CustomFieldDef{Module: "surat_masuk", Entity: "SuratMasuk", Field: textField("catatan")})

	eff, err := m.EffectiveEntity(ctx, "t", "surat_masuk", "SuratMasuk")
	if err != nil {
		t.Fatalf("EffectiveEntity: %v", err)
	}
	if got := fieldNames(eff); !equal(got, []string{"nomor", "perihal", "catatan"}) {
		t.Fatalf("merge efektif salah: %v", got)
	}
	// Entity tak dikenal → NotFound.
	if _, err := m.EffectiveEntity(ctx, "t", "aset", "Aset"); err == nil {
		t.Error("entity tak dikenal harus error")
	}
}

// assertCode memverifikasi err adalah *core.FrameworkError dengan Code tertentu.
func assertCode(t *testing.T, err error, code string) {
	t.Helper()
	var fe *core.FrameworkError
	if !errors.As(err, &fe) || fe.Code != code {
		t.Fatalf("harap %s, dapat %v", code, err)
	}
}
