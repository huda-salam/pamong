//go:build integration

package db_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/identity/adapter/db"
	"github.com/huda-salam/pamong/identity/domain"
	infradb "github.com/huda-salam/pamong/infra/db"
	"github.com/jackc/pgx/v5/pgxpool"
)

// setupIdentityDB membuka pool ke DB uji (PAMONG_TEST_DB_DSN), menerapkan migrasi
// schema id dari file migrasi nyata, lalu membersihkannya saat test selesai.
func setupIdentityDB(t *testing.T) (*infradb.Pool, context.Context) {
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
		_, _ = pool.Exec(context.Background(), `DROP SCHEMA IF EXISTS id CASCADE`)
		pgpool.Close()
	})

	if _, err := pool.Exec(ctx, `DROP SCHEMA IF EXISTS id CASCADE`); err != nil {
		t.Fatalf("reset: %v", err)
	}
	upSQL, err := os.ReadFile("../../migrations/001_create_identity.up.sql")
	if err != nil {
		t.Fatalf("baca migrasi: %v", err)
	}
	if _, err := pool.Exec(ctx, string(upSQL)); err != nil {
		t.Fatalf("apply migrasi: %v", err)
	}
	return pool, ctx
}

func TestIdentityRepos_CreatePersonEmploymentCredential(t *testing.T) {
	pool, ctx := setupIdentityDB(t)
	persons := db.NewPersonRepo(pool)
	employments := db.NewEmploymentRepo(pool)
	credentials := db.NewCredentialRepo(pool)

	// 1. Buat person (anchor NIK).
	p := &domain.Person{
		ID: uuid.New(), NIK: "3578010101900001", NamaLengkap: "Budi Santoso",
		Email: "budi@example.go.id", IsActive: true,
	}
	if err := persons.Save(ctx, p); err != nil {
		t.Fatalf("save person: %v", err)
	}

	// 2. Tambah employment ASN (wajib NIP).
	emp := &domain.Employment{
		ID: uuid.New(), PersonID: p.ID, Status: domain.StatusASN,
		NIP: "199001012015011001", InstansiAsal: "Pemkot Surabaya", IsActive: true,
	}
	if err := employments.Save(ctx, emp); err != nil {
		t.Fatalf("save employment: %v", err)
	}

	// 3. Tambah credential NIP + email.
	for _, c := range []*domain.Credential{
		{ID: uuid.New(), PersonID: p.ID, CredType: domain.CredNIP, CredValue: emp.NIP, IsPrimary: true},
		{ID: uuid.New(), PersonID: p.ID, CredType: domain.CredEmail, CredValue: p.Email},
	} {
		if err := credentials.Save(ctx, c); err != nil {
			t.Fatalf("save credential %s: %v", c.CredType, err)
		}
	}

	// Resolve by NIK.
	gotP, err := persons.FindByNIK(ctx, "3578010101900001")
	if err != nil {
		t.Fatalf("findByNIK: %v", err)
	}
	if gotP.ID != p.ID || gotP.NamaLengkap != "Budi Santoso" {
		t.Fatalf("person by NIK salah: %+v", gotP)
	}

	// Resolve by NIP → employment → person yang sama.
	gotE, err := employments.FindByNIP(ctx, emp.NIP)
	if err != nil {
		t.Fatalf("findByNIP: %v", err)
	}
	if gotE.PersonID != p.ID || gotE.Status != domain.StatusASN {
		t.Fatalf("employment by NIP salah: %+v", gotE)
	}

	// Credential resolve & list.
	gotC, err := credentials.FindByTypeValue(ctx, domain.CredNIP, emp.NIP)
	if err != nil || gotC.PersonID != p.ID {
		t.Fatalf("findByTypeValue: %v / %+v", err, gotC)
	}
	creds, err := credentials.ListByPerson(ctx, p.ID)
	if err != nil || len(creds) != 2 {
		t.Fatalf("listByPerson: %v / jumlah=%d", err, len(creds))
	}
}

func TestIdentityRepos_NonASN_NIPNull(t *testing.T) {
	pool, ctx := setupIdentityDB(t)
	persons := db.NewPersonRepo(pool)
	employments := db.NewEmploymentRepo(pool)

	p := &domain.Person{ID: uuid.New(), NIK: "3578010101900002", NamaLengkap: "Siti", IsActive: true}
	if err := persons.Save(ctx, p); err != nil {
		t.Fatalf("save person: %v", err)
	}
	// non-ASN: NIP kosong → tersimpan NULL, tidak menabrak unique antar non-ASN lain.
	for i := 0; i < 2; i++ {
		emp := &domain.Employment{
			ID: uuid.New(), PersonID: p.ID, Status: domain.StatusNonASN,
			InstansiAsal: "Honorer", IsActive: true,
		}
		if err := employments.Save(ctx, emp); err != nil {
			t.Fatalf("save non-asn %d: %v", i, err)
		}
	}
	list, err := employments.ListByPerson(ctx, p.ID)
	if err != nil || len(list) != 2 {
		t.Fatalf("dua employment non-ASN harus tersimpan: %v / %d", err, len(list))
	}
	if list[0].NIP != "" {
		t.Fatalf("NIP non-ASN harus kosong, dapat %q", list[0].NIP)
	}
}

func TestIdentityRepos_DuplicateNIK_Conflict(t *testing.T) {
	pool, ctx := setupIdentityDB(t)
	persons := db.NewPersonRepo(pool)

	p1 := &domain.Person{ID: uuid.New(), NIK: "3578010101900003", NamaLengkap: "A", IsActive: true}
	if err := persons.Save(ctx, p1); err != nil {
		t.Fatalf("save p1: %v", err)
	}
	p2 := &domain.Person{ID: uuid.New(), NIK: "3578010101900003", NamaLengkap: "B", IsActive: true}
	err := persons.Save(ctx, p2)
	var fe *core.FrameworkError
	if !errors.As(err, &fe) || fe.Code != "CONFLICT" {
		t.Fatalf("NIK duplikat harus CONFLICT, dapat: %v", err)
	}
}
