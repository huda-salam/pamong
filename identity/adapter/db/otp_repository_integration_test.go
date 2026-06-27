//go:build integration

package db_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/identity/adapter/db"
	"github.com/huda-salam/pamong/identity/domain"
	infradb "github.com/huda-salam/pamong/infra/db"
)

// applyOTPMigration menerapkan migrasi 006 (id.otps) di atas schema id baseline (001).
func applyOTPMigration(t *testing.T, pool *infradb.Pool, ctx context.Context) {
	t.Helper()
	upSQL, err := os.ReadFile("../../migrations/006_create_otps.up.sql")
	if err != nil {
		t.Fatalf("baca migrasi 006: %v", err)
	}
	if _, err := pool.Exec(ctx, string(upSQL)); err != nil {
		t.Fatalf("apply migrasi 006: %v", err)
	}
}

// TestOTPRepo_RoundTrip membuktikan migrasi 006 + SQL repo benar: Create → FindLatestByCredential
// (yang terbaru menang), RecordAttempt mempersist, Consume idempoten & membuat OTP tak usable.
func TestOTPRepo_RoundTrip(t *testing.T) {
	pool, ctx := setupIdentityDB(t)
	applyOTPMigration(t, pool, ctx)

	persons := db.NewPersonRepo(pool)
	creds := db.NewCredentialRepo(pool)
	otps := db.NewOTPRepo(pool)

	p := &domain.Person{ID: uuid.New(), NIK: "3578010101900050", NamaLengkap: "Warga", IsActive: true}
	if err := persons.Save(ctx, p); err != nil {
		t.Fatalf("save person: %v", err)
	}
	cred := &domain.Credential{ID: uuid.New(), PersonID: p.ID, CredType: domain.CredEmail,
		CredValue: "warga@example.com"}
	if err := creds.Save(ctx, cred); err != nil {
		t.Fatalf("save credential: %v", err)
	}

	now := time.Now()
	old := &domain.OTP{ID: uuid.New(), CredentialID: cred.ID, CodeHash: "h:old",
		ExpiresAt: now.Add(5 * time.Minute), CreatedAt: now.Add(-time.Minute)}
	if err := otps.Create(ctx, old); err != nil {
		t.Fatalf("create OTP lama: %v", err)
	}
	latest := &domain.OTP{ID: uuid.New(), CredentialID: cred.ID, CodeHash: "h:latest",
		ExpiresAt: now.Add(5 * time.Minute)}
	if err := otps.Create(ctx, latest); err != nil {
		t.Fatalf("create OTP terbaru: %v", err)
	}

	// FindLatest mengembalikan yang created_at paling baru.
	got, err := otps.FindLatestByCredential(ctx, cred.ID)
	if err != nil {
		t.Fatalf("FindLatest: %v", err)
	}
	if got.ID != latest.ID || got.CodeHash != "h:latest" {
		t.Fatalf("harus OTP terbaru, dapat: %+v", got)
	}
	if got.Attempts != 0 || got.ConsumedAt != nil {
		t.Fatalf("OTP baru harus attempts=0 & belum consumed: %+v", got)
	}

	// RecordAttempt persist.
	if err := otps.RecordAttempt(ctx, latest.ID, 3); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}
	got, _ = otps.FindLatestByCredential(ctx, cred.ID)
	if got.Attempts != 3 {
		t.Fatalf("attempts harus 3, dapat %d", got.Attempts)
	}

	// Consume membuat OTP tak usable; idempoten.
	if err := otps.Consume(ctx, latest.ID); err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if err := otps.Consume(ctx, latest.ID); err != nil {
		t.Fatalf("Consume idempoten gagal: %v", err)
	}
	got, _ = otps.FindLatestByCredential(ctx, cred.ID)
	if got.ConsumedAt == nil || got.IsUsable(now) {
		t.Fatalf("OTP harus consumed & tak usable: %+v", got)
	}
}

// TestOTPRepo_FindLatest_NotFound: credential tanpa OTP → core.ErrNotFound.
func TestOTPRepo_FindLatest_NotFound(t *testing.T) {
	pool, ctx := setupIdentityDB(t)
	applyOTPMigration(t, pool, ctx)

	otps := db.NewOTPRepo(pool)
	if _, err := otps.FindLatestByCredential(ctx, uuid.New()); err == nil {
		t.Fatal("credential tanpa OTP harus error NotFound")
	}
}
