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

// applyRevokedTokensMigration menerapkan migrasi 005 (id.revoked_tokens) di atas schema id
// baseline (001) yang sudah disiapkan setupIdentityDB. Pola sama dengan applyAssignmentMigration.
func applyRevokedTokensMigration(t *testing.T, pool *infradb.Pool, ctx context.Context) {
	t.Helper()
	upSQL, err := os.ReadFile("../../migrations/005_create_revoked_tokens.up.sql")
	if err != nil {
		t.Fatalf("baca migrasi 005: %v", err)
	}
	if _, err := pool.Exec(ctx, string(upSQL)); err != nil {
		t.Fatalf("apply migrasi 005: %v", err)
	}
}

// TestRevokedTokenStore_RevokeAndCheck membuktikan migrasi 005 + SQL store benar:
// denylist jti round-trip, jti lain tak terdampak, dan Revoke idempoten.
func TestRevokedTokenStore_RevokeAndCheck(t *testing.T) {
	pool, ctx := setupIdentityDB(t)
	applyRevokedTokensMigration(t, pool, ctx)

	persons := db.NewPersonRepo(pool)
	store := db.NewRevokedTokenStore(pool)

	// Person sebagai target FK person_id.
	p := &domain.Person{ID: uuid.New(), NIK: "3578010101900001", NamaLengkap: "Budi", IsActive: true}
	if err := persons.Save(ctx, p); err != nil {
		t.Fatalf("save person: %v", err)
	}

	jti := uuid.New()
	other := uuid.New()
	exp := time.Now().Add(time.Hour)

	// Sebelum dicabut → false.
	if revoked, err := store.IsRevoked(ctx, jti); err != nil || revoked {
		t.Fatalf("jti belum dicabut harus false (err=%v, revoked=%v)", err, revoked)
	}

	// Cabut jti → true; jti lain tetap false.
	if err := store.Revoke(ctx, jti, p.ID, exp, "uji"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if revoked, err := store.IsRevoked(ctx, jti); err != nil || !revoked {
		t.Fatalf("jti dicabut harus true (err=%v, revoked=%v)", err, revoked)
	}
	if revoked, err := store.IsRevoked(ctx, other); err != nil || revoked {
		t.Fatalf("jti lain harus false (err=%v, revoked=%v)", err, revoked)
	}

	// Idempoten: mencabut ulang jti yang sama bukan error (ON CONFLICT DO NOTHING).
	if err := store.Revoke(ctx, jti, p.ID, exp, "uji-ulang"); err != nil {
		t.Fatalf("revoke idempoten gagal: %v", err)
	}
}
