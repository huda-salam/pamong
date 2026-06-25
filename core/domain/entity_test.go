package domain_test

import (
	"context"
	"strings"
	"testing"

	"github.com/huda-salam/pamong/core/domain"
)

// validEntity adalah baseline EntityDef yang lulus validasi; tiap test memodifikasi
// satu aspek untuk menguji satu aturan.
func validEntity() domain.EntityDef {
	return domain.EntityDef{
		Name:       "SPM",
		Schema:     "penatausahaan",
		Tablename:  "penatausahaan.spms",
		Tier:       domain.Tier3,
		Audit:      domain.Audited{},
		Lockable:   domain.Lockable{FiscalField: "tanggal"},
		Searchable: []string{"nomor"},
		Fields: []domain.FieldDef{
			{Name: "nomor", Type: domain.FieldText, Required: true, Unique: true},
			{Name: "tanggal", Type: domain.FieldDate, Required: true},
			{Name: "nilai", Type: domain.FieldDecimal, Precision: 2},
			{Name: "status", Type: domain.FieldEnum, Options: []string{"draft", "sah"}},
		},
	}
}

func TestEntity_Valid(t *testing.T) {
	if err := validEntity().Validate(); err != nil {
		t.Fatalf("entity valid tak boleh error: %v", err)
	}
}

func TestEntity_AturanValidasi(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*domain.EntityDef)
		wantSub string
	}{
		{"audit nil", func(e *domain.EntityDef) { e.Audit = nil }, "Audit wajib"},
		{"lockable nil", func(e *domain.EntityDef) { e.Lockable = nil }, "Lockable wajib"},
		{"notaudited tanpa reason", func(e *domain.EntityDef) { e.Audit = domain.NotAudited{} }, "Reason"},
		{"tablename salah format", func(e *domain.EntityDef) { e.Tablename = "spms" }, "Tablename"},
		{"tier invalid", func(e *domain.EntityDef) { e.Tier = 9 }, "Tier"},
		{"enum tanpa options", func(e *domain.EntityDef) { e.Fields[3].Options = nil }, "Enum"},
		{"link tanpa linkto", func(e *domain.EntityDef) {
			e.Fields[0] = domain.FieldDef{Name: "ref", Type: domain.FieldLink}
		}, "Link"},
		{"decimal tanpa precision", func(e *domain.EntityDef) { e.Fields[2].Precision = 0 }, "Decimal"},
		{"field reserved", func(e *domain.EntityDef) { e.Fields[0].Name = "version" }, "reserved"},
		{"field duplikat", func(e *domain.EntityDef) { e.Fields[1].Name = "nomor" }, "duplikat"},
		{"fiscalfield bukan field", func(e *domain.EntityDef) { e.Lockable = domain.Lockable{FiscalField: "xx"} }, "FiscalField"},
		{"searchable bukan field", func(e *domain.EntityDef) { e.Searchable = []string{"xx"} }, "Searchable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := validEntity()
			tc.mutate(&e)
			err := e.Validate()
			if err == nil {
				t.Fatalf("harus ditolak, tapi lulus")
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("pesan tidak memuat %q; dapat: %v", tc.wantSub, err)
			}
		})
	}
}

// TestEntity_NotAuditedValid memastikan NotAudited dengan Reason diterima.
func TestEntity_NotAuditedValid(t *testing.T) {
	e := validEntity()
	e.Audit = domain.NotAudited{Reason: "data referensi statis, tidak perlu audit"}
	if err := e.Validate(); err != nil {
		t.Fatalf("NotAudited dengan Reason harus valid: %v", err)
	}
}

// TestRegistry_TabelDuplikatLintasModul memastikan dua entity beda modul tak boleh
// klaim nama tabel yang sama (PRD F2).
func TestRegistry_TabelDuplikatLintasModul(t *testing.T) {
	mkMod := func(name string) domain.Module {
		e := validEntity()
		e.Schema = name
		e.Tablename = name + ".spms" // schema beda, tapi kita paksa tabel sama di bawah
		return entModule{name: name, ents: []domain.EntityDef{e}}
	}
	// Paksa dua modul memakai tablename identik.
	a := mkMod("a")
	b := mkMod("b")
	am := a.(entModule)
	bm := b.(entModule)
	am.ents[0].Tablename = "shared.spms"
	am.ents[0].Schema = "shared"
	bm.ents[0].Tablename = "shared.spms"
	bm.ents[0].Schema = "shared"

	r := domain.NewRegistry()
	r.Register(am, bm)
	err := r.Validate()
	if err == nil || !strings.Contains(err.Error(), "diklaim dua entity") {
		t.Fatalf("tabel duplikat lintas-modul harus ditolak, dapat: %v", err)
	}
}

// entModule adalah modul uji yang membawa entity.
type entModule struct {
	name string
	ents []domain.EntityDef
}

func (m entModule) Manifest() domain.Manifest {
	return domain.Manifest{Name: m.name, Version: "1.0.0", Entities: m.ents}
}
func (m entModule) Bootstrap(context.Context, *domain.App) error { return nil }
