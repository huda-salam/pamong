package audit_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core/audit"
	"github.com/huda-salam/pamong/testkit"
)

func diffByField(diffs []audit.FieldDiff) map[string]audit.FieldDiff {
	m := make(map[string]audit.FieldDiff, len(diffs))
	for _, d := range diffs {
		m[d.Field] = d
	}
	return m
}

func TestDiff_Update_OnlyChangedFields(t *testing.T) {
	before := map[string]any{"nama": "A", "harga": 100, "aktif": true}
	after := map[string]any{"nama": "A", "harga": 150, "aktif": true}

	diffs := audit.Diff(before, after)
	if len(diffs) != 1 {
		t.Fatalf("harus 1 field berubah, dapat %d: %+v", len(diffs), diffs)
	}
	d := diffs[0]
	if d.Field != "harga" || d.Before != 100 || d.After != 150 {
		t.Fatalf("diff salah: %+v", d)
	}
}

func TestDiff_Create_AllAsAfter(t *testing.T) {
	diffs := audit.Diff(nil, map[string]any{"nama": "B", "harga": 50})
	m := diffByField(diffs)
	if len(diffs) != 2 || m["nama"].Before != nil || m["nama"].After != "B" {
		t.Fatalf("create diff salah: %+v", diffs)
	}
}

func TestDiff_Delete_AllAsBefore(t *testing.T) {
	diffs := audit.Diff(map[string]any{"nama": "C"}, nil)
	m := diffByField(diffs)
	if len(diffs) != 1 || m["nama"].Before != "C" || m["nama"].After != nil {
		t.Fatalf("delete diff salah: %+v", diffs)
	}
}

func TestDiff_Deterministic_SortedByField(t *testing.T) {
	diffs := audit.Diff(nil, map[string]any{"c": 1, "a": 1, "b": 1})
	if diffs[0].Field != "a" || diffs[1].Field != "b" || diffs[2].Field != "c" {
		t.Fatalf("urutan field harus terurut: %+v", diffs)
	}
}

// fakeStore menangkap entry yang ditulis untuk asersi unit test.
type fakeStore struct{ entries []audit.AuditEntry }

func (f *fakeStore) Append(_ context.Context, e audit.AuditEntry) error {
	f.entries = append(f.entries, e)
	return nil
}

func TestEngine_Record_BuildsEntryFromContext(t *testing.T) {
	store := &fakeStore{}
	eng := audit.NewEngine(store)

	person := uuid.New()
	ctx := testkit.Ctx(t, testkit.WithTenant("pemkot-surabaya"), testkit.WithPersonID(person))

	entityID := uuid.New()
	err := eng.Record(ctx, audit.RecordInput{
		Entity:   "persuratan.SuratMasuk",
		EntityID: entityID,
		Action:   audit.ActionUpdate,
		Before:   map[string]any{"status": "draft"},
		After:    map[string]any{"status": "diterima"},
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}

	if len(store.entries) != 1 {
		t.Fatalf("harus 1 entry, dapat %d", len(store.entries))
	}
	e := store.entries[0]
	if e.TenantID != "pemkot-surabaya" || e.ActorID != person {
		t.Fatalf("metadata actor/tenant salah: %+v", e)
	}
	if e.Entity != "persuratan.SuratMasuk" || e.EntityID != entityID || e.Action != audit.ActionUpdate {
		t.Fatalf("metadata entity salah: %+v", e)
	}
	if len(e.Diff) != 1 || e.Diff[0].Field != "status" {
		t.Fatalf("diff salah: %+v", e.Diff)
	}
	if e.ID == uuid.Nil || e.Timestamp.IsZero() {
		t.Fatalf("ID/timestamp harus terisi: %+v", e)
	}
}

func TestEngine_Record_NoopUpdate_Skipped(t *testing.T) {
	store := &fakeStore{}
	eng := audit.NewEngine(store)
	ctx := testkit.Ctx(t)

	err := eng.Record(ctx, audit.RecordInput{
		Entity:   "persuratan.SuratMasuk",
		EntityID: uuid.New(),
		Action:   audit.ActionUpdate,
		Before:   map[string]any{"status": "draft"},
		After:    map[string]any{"status": "draft"},
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if len(store.entries) != 0 {
		t.Fatalf("update tanpa perubahan tidak boleh menghasilkan entry, dapat %d", len(store.entries))
	}
}
