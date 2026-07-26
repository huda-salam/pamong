package customization_test

import (
	"errors"
	"testing"

	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/core/customization"
	"github.com/huda-salam/pamong/core/domain"
)

// textField adalah helper FieldDef Text sederhana.
func textField(name string) domain.FieldDef {
	return domain.FieldDef{Name: name, Type: domain.FieldText}
}

func customField(tenant, module, entity, name string) customization.CustomFieldDef {
	return customization.CustomFieldDef{
		TenantID: tenant, Module: module, Entity: entity,
		Field: textField(name), IsActive: true,
	}
}

func TestCustomFieldDef_Validate(t *testing.T) {
	tests := []struct {
		name    string
		def     customization.CustomFieldDef
		wantErr bool
	}{
		{"valid", customField("t", "m", "E", "catatan"), false},
		{"tenant kosong", customization.CustomFieldDef{Module: "m", Entity: "E", Field: textField("x")}, true},
		{"module kosong", customization.CustomFieldDef{TenantID: "t", Entity: "E", Field: textField("x")}, true},
		{"entity kosong", customization.CustomFieldDef{TenantID: "t", Module: "m", Field: textField("x")}, true},
		{"nama reserved", customization.CustomFieldDef{TenantID: "t", Module: "m", Entity: "E", Field: textField("created_at")}, true},
		{"enum tanpa options", customization.CustomFieldDef{TenantID: "t", Module: "m", Entity: "E", Field: domain.FieldDef{Name: "j", Type: domain.FieldEnum}}, true},
		{"class tak dikenal", func() customization.CustomFieldDef {
			d := customField("t", "m", "E", "x")
			d.Class = "rahasia_negara"
			return d
		}(), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.def.Normalize().Validate()
			if tt.wantErr && err == nil {
				t.Fatalf("harap error, dapat nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("harap sukses, dapat %v", err)
			}
		})
	}
}

// TestNormalize_ClassDefaultInternal: class kosong → internal (default aman), bukan public.
func TestNormalize_ClassDefaultInternal(t *testing.T) {
	got := customField("t", "m", "E", "x").Normalize()
	if got.Class != customization.ClassInternal {
		t.Errorf("class default = %q, harap internal", got.Class)
	}
}

func TestMemoryCustomFieldStore_SaveListDeactivate(t *testing.T) {
	s := customization.NewMemoryCustomFieldStore()
	if err := s.Save(ctx(), customField("t", "m", "E", "b")); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(ctx(), customField("t", "m", "E", "a")); err != nil {
		t.Fatal(err)
	}
	// Isolasi: tenant/entity lain tak terpengaruh.
	if err := s.Save(ctx(), customField("t2", "m", "E", "z")); err != nil {
		t.Fatal(err)
	}

	got, err := s.List(ctx(), "t", "m", "E")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Field.Name != "a" || got[1].Field.Name != "b" {
		t.Fatalf("List harus terurut nama [a b], dapat %+v", got)
	}

	// Deactivate → tak lagi muncul di List.
	if err := s.Deactivate(ctx(), "t", "m", "E", "a"); err != nil {
		t.Fatal(err)
	}
	got, _ = s.List(ctx(), "t", "m", "E")
	if len(got) != 1 || got[0].Field.Name != "b" {
		t.Fatalf("setelah deactivate a, tersisa [b], dapat %+v", got)
	}

	// Deactivate field tak ada → NotFound.
	err = s.Deactivate(ctx(), "t", "m", "E", "hantu")
	var fe *core.FrameworkError
	if !errors.As(err, &fe) || fe.Code != "NOT_FOUND" {
		t.Errorf("harap NOT_FOUND, dapat %v", err)
	}
}

// TestMemoryCustomFieldStore_SaveMenimpaNamaSama: Save nama sama = upsert (mis. reaktivasi).
func TestMemoryCustomFieldStore_SaveMenimpaNamaSama(t *testing.T) {
	s := customization.NewMemoryCustomFieldStore()
	_ = s.Save(ctx(), customField("t", "m", "E", "x"))
	upd := customField("t", "m", "E", "x")
	upd.InsertAfter = "nomor"
	_ = s.Save(ctx(), upd)

	got, _ := s.List(ctx(), "t", "m", "E")
	if len(got) != 1 || got[0].InsertAfter != "nomor" {
		t.Fatalf("Save nama sama harus menimpa jadi satu entri terkini, dapat %+v", got)
	}
}
