//go:build integration

package customization_test

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	coreCust "github.com/huda-salam/pamong/core/customization"
	"github.com/huda-salam/pamong/core/domain"
	infraCust "github.com/huda-salam/pamong/infra/customization"
	"github.com/huda-salam/pamong/infra/db"
)

func newEnv(t *testing.T) (*infraCust.DBCustomFieldStore, *infraCust.DBTenantCapabilityStore, *db.Pool, context.Context) {
	t.Helper()
	dsn := os.Getenv("PAMONG_TEST_DB_DSN")
	if dsn == "" {
		t.Skip("PAMONG_TEST_DB_DSN tidak diset — lewati integration test")
	}
	ctx := context.Background()
	pgpool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("buka pool: %v", err)
	}
	pool := db.NewPool(pgpool)

	_, _ = pool.Exec(ctx, `DROP SCHEMA IF EXISTS gov CASCADE`)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DROP SCHEMA IF EXISTS gov CASCADE`)
		pgpool.Close()
	})

	cf := infraCust.NewDBCustomFieldStore(pool)
	if err := cf.EnsureSchema(ctx); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	return cf, infraCust.NewDBTenantCapabilityStore(pool), pool, ctx
}

// TestDBCustomField_SaveListDeactivate — DoD PR-3.4.1 di Postgres nyata: custom field tersimpan,
// terbaca (JSONB round-trip), dan dinonaktifkan.
func TestDBCustomField_SaveListDeactivate(t *testing.T) {
	cf, _, _, ctx := newEnv(t)
	prec := "2"
	def := coreCust.CustomFieldDef{
		TenantID: "pemkot-surabaya", Module: "surat_masuk", Entity: "SuratMasuk",
		Field: domain.FieldDef{
			Name: "nilai_pagu", Type: domain.FieldDecimal, Precision: 2, Required: true, Default: &prec,
		},
		Class: coreCust.ClassInternal, InsertAfter: "nomor", IsActive: true,
		CreatedBy: uuid.New(),
	}
	if err := cf.Save(ctx, def); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := cf.List(ctx, "pemkot-surabaya", "surat_masuk", "SuratMasuk")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("harap 1 field, dapat %d", len(got))
	}
	f := got[0]
	if f.Field.Name != "nilai_pagu" || f.Field.Type != domain.FieldDecimal || f.Field.Precision != 2 ||
		!f.Field.Required || f.Field.Default == nil || *f.Field.Default != "2" {
		t.Fatalf("JSONB round-trip FieldDef salah: %+v", f.Field)
	}
	if f.Class != coreCust.ClassInternal || f.InsertAfter != "nomor" {
		t.Fatalf("metadata custom field salah: class=%q after=%q", f.Class, f.InsertAfter)
	}

	// Upsert nama sama tak menggandakan.
	def.InsertAfter = "perihal"
	if err := cf.Save(ctx, def); err != nil {
		t.Fatalf("Save (upsert): %v", err)
	}
	got, _ = cf.List(ctx, "pemkot-surabaya", "surat_masuk", "SuratMasuk")
	if len(got) != 1 || got[0].InsertAfter != "perihal" {
		t.Fatalf("upsert harus menimpa: %+v", got)
	}

	// Deactivate → hilang dari List.
	if err := cf.Deactivate(ctx, "pemkot-surabaya", "surat_masuk", "SuratMasuk", "nilai_pagu"); err != nil {
		t.Fatalf("Deactivate: %v", err)
	}
	got, _ = cf.List(ctx, "pemkot-surabaya", "surat_masuk", "SuratMasuk")
	if len(got) != 0 {
		t.Fatalf("setelah deactivate harus kosong: %+v", got)
	}
	// Deactivate lagi (tak ada aktif) → NotFound.
	if err := cf.Deactivate(ctx, "pemkot-surabaya", "surat_masuk", "SuratMasuk", "nilai_pagu"); err == nil {
		t.Error("deactivate field tak aktif harus error")
	}
}

// TestDBCapabilityOverride — persistensi override (carry-over PR-3.4.2) di Postgres nyata:
// ketiadaan baris → ok=false; Set → upsert; isolasi antar tenant.
func TestDBCapabilityOverride(t *testing.T) {
	_, cap, pool, ctx := newEnv(t)

	if _, ok, err := cap.Override(ctx, "pemkot-surabaya", "surat.disposisi_massal"); err != nil || ok {
		t.Fatalf("belum di-set harus ok=false: ok=%v err=%v", ok, err)
	}
	actor := uuid.New()
	if err := cap.Set(ctx, "pemkot-surabaya", "surat.disposisi_massal", true, &actor); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if enabled, ok, err := cap.Override(ctx, "pemkot-surabaya", "surat.disposisi_massal"); err != nil || !ok || !enabled {
		t.Fatalf("override: enabled=%v ok=%v err=%v", enabled, ok, err)
	}
	// set_by tercatat (atribusi — bukan NULL).
	var setBy *uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT set_by FROM gov.tenant_capability_overrides
		WHERE tenant_id = $1 AND capability = $2`, "pemkot-surabaya", "surat.disposisi_massal").
		Scan(&setBy); err != nil {
		t.Fatalf("baca set_by: %v", err)
	}
	if setBy == nil || *setBy != actor {
		t.Fatalf("set_by harus = actor %v, dapat %v", actor, setBy)
	}
	// Upsert ke false.
	if err := cap.Set(ctx, "pemkot-surabaya", "surat.disposisi_massal", false, nil); err != nil {
		t.Fatalf("Set false: %v", err)
	}
	if enabled, ok, _ := cap.Override(ctx, "pemkot-surabaya", "surat.disposisi_massal"); !ok || enabled {
		t.Fatalf("upsert ke false gagal: enabled=%v ok=%v", enabled, ok)
	}
	// Tenant lain tak terpengaruh.
	if _, ok, _ := cap.Override(ctx, "pemkot-malang", "surat.disposisi_massal"); ok {
		t.Error("override bocor ke tenant lain")
	}
}
