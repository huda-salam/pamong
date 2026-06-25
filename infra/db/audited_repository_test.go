package db_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core/domain"
	"github.com/huda-salam/pamong/infra/db"
)

// widget + mapper minimal untuk menguji pemilihan factory tanpa DB.
type widget struct {
	ID      uuid.UUID
	Nama    string
	Version int
}

type widgetMapper struct{}

func (widgetMapper) Table() string              { return "demo.widgets" }
func (widgetMapper) DataColumns() []string      { return []string{"nama"} }
func (widgetMapper) DataValues(e *widget) []any { return []any{e.Nama} }
func (widgetMapper) Scan(s db.RowScanner) (*widget, error) {
	var w widget
	return &w, s.Scan(&w.ID, &w.Nama, &w.Version)
}
func (widgetMapper) ID(e *widget) uuid.UUID      { return e.ID }
func (widgetMapper) Version(e *widget) int       { return e.Version }
func (widgetMapper) SetVersion(e *widget, v int) { e.Version = v }
func (widgetMapper) SearchColumns() []string     { return nil }

func notAuditedDef() domain.EntityDef {
	return domain.EntityDef{
		Name: "Widget", Schema: "demo", Tier: domain.Tier1,
		Audit: domain.NotAudited{Reason: "master sederhana"}, Lockable: domain.NotLockable{},
	}
}

func auditedDef() domain.EntityDef {
	d := notAuditedDef()
	d.Audit = domain.Audited{}
	return d
}

func TestNewRepository_NotAudited_ReturnsPlainRepo(t *testing.T) {
	repo, err := db.NewRepository[widget](nil, widgetMapper{}, notAuditedDef(), nil)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	if _, ok := repo.(*db.SQLRepository[widget]); !ok {
		t.Fatalf("NotAudited harus mengembalikan repo polos, dapat %T", repo)
	}
}

func TestNewRepository_Audited_RequiresEngine(t *testing.T) {
	if _, err := db.NewRepository[widget](nil, widgetMapper{}, auditedDef(), nil); err == nil {
		t.Fatal("Audited tanpa engine harus error")
	}
}
