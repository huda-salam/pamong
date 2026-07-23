//go:build integration

package notification_test

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	coreNotif "github.com/huda-salam/pamong/core/notification"
	"github.com/huda-salam/pamong/infra/db"
	infraNotif "github.com/huda-salam/pamong/infra/notification"
)

func newNotifEnv(t *testing.T) (*db.Pool, context.Context) {
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

	drop := `DROP TABLE IF EXISTS gov.notification_deliveries;
	         DROP TABLE IF EXISTS gov.notification_inapp;
	         DROP TABLE IF EXISTS gov.notification_templates`
	_, _ = pool.Exec(ctx, drop)
	if err := infraNotif.EnsureSchema(ctx, pool); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), drop)
		pgpool.Close()
	})
	return pool, ctx
}

func TestDBTemplateStore_UpsertAndCandidates(t *testing.T) {
	pool, ctx := newNotifEnv(t)
	store := infraNotif.NewDBTemplateStore(pool)

	// global + tenant override untuk key sama
	must(t, store.Upsert(ctx, coreNotif.Template{Key: "k", Locale: "id", Subject: "global", Body: "b"}))
	must(t, store.Upsert(ctx, coreNotif.Template{TenantID: "pemkot", Key: "k", Locale: "id", Subject: "tenant", Body: "b"}))

	cands, err := store.Candidates(ctx, "pemkot", "k")
	if err != nil {
		t.Fatalf("candidates: %v", err)
	}
	if len(cands) != 2 {
		t.Fatalf("candidates = %d, mau 2 (global + tenant)", len(cands))
	}

	// upsert ulang locale sama harus MENIMPA (bukan duplikat)
	must(t, store.Upsert(ctx, coreNotif.Template{TenantID: "pemkot", Key: "k", Locale: "id", Subject: "tenant-v2", Body: "b"}))
	cands, _ = store.Candidates(ctx, "pemkot", "k")
	if len(cands) != 2 {
		t.Fatalf("setelah upsert ulang candidates = %d, mau tetap 2", len(cands))
	}
}

func TestDBTemplateStore_RendersViaEngine(t *testing.T) {
	pool, ctx := newNotifEnv(t)
	store := infraNotif.NewDBTemplateStore(pool)
	must(t, store.Upsert(ctx, coreNotif.Template{
		Key: "surat.disposisi", Locale: "id", Subject: "Disposisi {{.nomor}}", Body: "Isi",
	}))
	eng := coreNotif.NewTemplateEngine(store)
	msg, err := eng.Render(ctx, "pemkot", "surat.disposisi", "id", map[string]any{"nomor": "007"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if msg.Subject != "Disposisi 007" {
		t.Fatalf("subject = %q", msg.Subject)
	}
}

func TestDBInAppInbox_AppendAndList(t *testing.T) {
	pool, ctx := newNotifEnv(t)
	inbox := infraNotif.NewDBInAppInbox(pool)
	pid := uuid.New()

	for i := 0; i < 3; i++ {
		if _, err := inbox.Append(ctx, coreNotif.InAppItem{
			TenantID: "pemkot", PersonID: pid, Subject: "s", Body: "b",
		}); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	// penerima lain tak boleh tercampur
	_, _ = inbox.Append(ctx, coreNotif.InAppItem{TenantID: "pemkot", PersonID: uuid.New(), Body: "x"})

	items, err := inbox.List(ctx, "pemkot", pid.String(), 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("items = %d, mau 3 (isolasi penerima)", len(items))
	}

	limited, _ := inbox.List(ctx, "pemkot", pid.String(), 2)
	if len(limited) != 2 {
		t.Fatalf("limited = %d, mau 2", len(limited))
	}
}

func TestDBDeliveryRecorder_Record(t *testing.T) {
	pool, ctx := newNotifEnv(t)
	rec := infraNotif.NewDBDeliveryRecorder(pool)
	pid := uuid.New()

	must(t, rec.Record(ctx, coreNotif.DeliveryRecord{
		TenantID: "pemkot", PersonID: pid, Channel: "email",
		TemplateKey: "k", Status: coreNotif.StatusDelivered,
	}))
	must(t, rec.Record(ctx, coreNotif.DeliveryRecord{
		TenantID: "pemkot", PersonID: pid, Channel: "in_app",
		TemplateKey: "k", Status: coreNotif.StatusFailed, Error: "boom",
	}))

	var n int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM gov.notification_deliveries WHERE person_id = $1`, pid,
	).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 2 {
		t.Fatalf("deliveries = %d, mau 2", n)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
