//go:build integration

package db_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core/audit"
	"github.com/huda-salam/pamong/infra/db"
	"github.com/jackc/pgx/v5/pgxpool"
)

func newAuditRepo(t *testing.T) (*db.AuditRepo, *db.Pool, context.Context) {
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
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DROP TABLE IF EXISTS gov.audit_logs`)
		pgpool.Close()
	})
	if _, err := pool.Exec(ctx, `DROP TABLE IF EXISTS gov.audit_logs`); err != nil {
		t.Fatalf("reset: %v", err)
	}
	repo := db.NewAuditRepo(pool)
	if err := repo.EnsureSchema(ctx); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	return repo, pool, ctx
}

func TestAuditRepo_AppendAndQuery(t *testing.T) {
	repo, _, ctx := newAuditRepo(t)

	entityID := uuid.New()
	actor := uuid.New()
	entries := []audit.AuditEntry{
		{
			ID: uuid.New(), TenantID: "pemkot-surabaya", Entity: "persuratan.SuratMasuk",
			EntityID: entityID, Action: audit.ActionCreate, ActorID: actor,
			Diff:      []audit.FieldDiff{{Field: "status", Before: nil, After: "draft"}},
			Timestamp: time.Now().Add(-time.Minute),
		},
		{
			ID: uuid.New(), TenantID: "pemkot-surabaya", Entity: "persuratan.SuratMasuk",
			EntityID: entityID, Action: audit.ActionUpdate, ActorID: actor,
			Diff:      []audit.FieldDiff{{Field: "status", Before: "draft", After: "diterima"}},
			Timestamp: time.Now(),
		},
	}
	for _, e := range entries {
		if err := repo.Append(ctx, e); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	got, err := repo.ByEntity(ctx, "persuratan.SuratMasuk", entityID)
	if err != nil {
		t.Fatalf("byEntity: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("harus 2 entry, dapat %d", len(got))
	}
	// Terurut kronologis: create dulu, lalu update.
	if got[0].Action != audit.ActionCreate || got[1].Action != audit.ActionUpdate {
		t.Fatalf("urutan kronologis salah: %s, %s", got[0].Action, got[1].Action)
	}
	if len(got[1].Diff) != 1 || got[1].Diff[0].Field != "status" || got[1].Diff[0].After != "diterima" {
		t.Fatalf("diff tidak ter-roundtrip: %+v", got[1].Diff)
	}
}
