//go:build integration

package sync_test

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/identity/sync"
	infradb "github.com/huda-salam/pamong/infra/db"
	"github.com/huda-salam/pamong/port"
	"github.com/jackc/pgx/v5/pgxpool"
)

// fakePools mengembalikan satu pool DB uji untuk tenant apa pun — cukup untuk menguji
// jalur tulis writer terhadap Postgres nyata tanpa registry/TenantConnManager penuh.
type fakePools struct{ pool *infradb.Pool }

func (f fakePools) Tenant(_ context.Context, _ string) (*infradb.Pool, error) { return f.pool, nil }

// setupTenantDB membuka pool ke DB uji dan membersihkan schema gov.
func setupTenantDB(t *testing.T) (*infradb.Pool, context.Context) {
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
	pool := infradb.NewPool(pgpool)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DROP TABLE IF EXISTS gov.user_profiles`)
		pgpool.Close()
	})
	if _, err := pool.Exec(ctx, `DROP TABLE IF EXISTS gov.user_profiles`); err != nil {
		t.Fatalf("reset gov: %v", err)
	}
	return pool, ctx
}

// TestSyncClone_EmploymentDitugaskan adalah DoD PR-2.2.4: event identity.employment.ditugaskan
// menghasilkan clone di gov.user_profiles tenant tujuan (lewat memory bus).
func TestSyncClone_EmploymentDitugaskan(t *testing.T) {
	pool, ctx := setupTenantDB(t)

	writer := sync.NewTenantDBWriter(fakePools{pool: pool})
	engine := sync.NewEngine(writer)
	bus := newBus(t)
	if err := engine.Register(bus); err != nil {
		t.Fatalf("register: %v", err)
	}

	personID := uuid.New()
	assignmentID := uuid.New()
	if err := bus.Publish(ctx, port.Event{
		Name:     domain.EventEmploymentDitugaskan,
		TenantID: "pemkot-surabaya",
		Payload: domain.EmploymentDitugaskanPayload{
			AssignmentID:     assignmentID,
			EmploymentID:     uuid.New(),
			PersonID:         personID,
			TenantID:         "pemkot-surabaya",
			NIK:              "3578010101900001",
			NIP:              "199001012015011001",
			NamaLengkap:      "Budi Santoso",
			EmploymentStatus: "asn",
		},
	}); err != nil {
		t.Fatalf("publish ditugaskan: %v", err)
	}

	// Clone harus muncul di gov.user_profiles tenant.
	var (
		gotNIK, gotNIP, gotNama, gotStatus string
		gotAssignment                      uuid.UUID
		gotCross                           bool
	)
	if err := pool.QueryRow(ctx,
		`SELECT nik, nip, nama_lengkap, employment_status, assignment_id, is_cross_tenant
		   FROM gov.user_profiles WHERE id = $1`, personID,
	).Scan(&gotNIK, &gotNIP, &gotNama, &gotStatus, &gotAssignment, &gotCross); err != nil {
		t.Fatalf("clone tidak ditemukan: %v", err)
	}
	if gotNIK != "3578010101900001" || gotNIP != "199001012015011001" ||
		gotNama != "Budi Santoso" || gotStatus != "asn" || gotAssignment != assignmentID || gotCross {
		t.Fatalf("data clone salah: nik=%s nip=%s nama=%s status=%s assignment=%s cross=%v",
			gotNIK, gotNIP, gotNama, gotStatus, gotAssignment, gotCross)
	}

	// Idempoten: event terkirim ulang tidak menggandakan baris (ON CONFLICT).
	if err := bus.Publish(ctx, port.Event{
		Name:     domain.EventEmploymentDitugaskan,
		TenantID: "pemkot-surabaya",
		Payload: domain.EmploymentDitugaskanPayload{
			AssignmentID: assignmentID, EmploymentID: uuid.New(), PersonID: personID,
			TenantID: "pemkot-surabaya", NIK: "3578010101900001", NIP: "199001012015011001",
			NamaLengkap: "Budi Santoso", EmploymentStatus: "asn",
		},
	}); err != nil {
		t.Fatalf("publish ulang: %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM gov.user_profiles WHERE id = $1`, personID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("clone harus idempoten (1 baris), dapat %d", count)
	}
}
