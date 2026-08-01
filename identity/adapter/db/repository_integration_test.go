//go:build integration

package db_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/core/audit"
	"github.com/huda-salam/pamong/core/config"
	"github.com/huda-salam/pamong/identity/adapter/db"
	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/infra/crypto"
	infradb "github.com/huda-salam/pamong/infra/db"
	"github.com/huda-salam/pamong/port"
	"github.com/jackc/pgx/v5/pgxpool"
)

// setupIdentityDB membuka pool ke DB uji (PAMONG_TEST_DB_DSN), menerapkan migrasi schema id
// dari file migrasi NYATA (bukan DDL tulis tangan — yang diuji justru sambungan migrasi ↔
// repo), merakit kripto, lalu membersihkannya saat test selesai.
//
// Migrasi 007/008 ikut diterapkan karena DEK ter-wrap & kolom custody hidup di sana, dan
// 009 karena sejak itu pengenal identity tersimpan terenkripsi. 002 (tenant_registry) ikut
// sejak awal supaya tak ada test yang harus menerapkannya sendiri.
func setupIdentityDB(t *testing.T) (*infradb.Pool, port.CryptoPort, context.Context) {
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
	for _, name := range []string{
		"001_create_identity.up.sql",
		"002_create_tenant_registry.up.sql",
		"007_create_data_keys.up.sql",
		"008_add_key_custody_tenant_registry.up.sql",
		"009_encrypt_identity_identifiers.up.sql",
	} {
		upSQL, err := os.ReadFile("../../migrations/" + name)
		if err != nil {
			t.Fatalf("baca migrasi %s: %v", name, err)
		}
		if _, err := pool.Exec(ctx, string(upSQL)); err != nil {
			t.Fatalf("apply migrasi %s: %v", name, err)
		}
	}
	return pool, newIdentityCryptoSvc(t, pool), ctx
}

// newIdentityCryptoSvc merakit CryptoPort produksi (driver static, envelope in-app) di atas
// identity DB uji. Sengaja BUKAN mock: yang harus terbukti di sini adalah bahwa realm
// sentral benar-benar bisa membuat & memakai DEK-nya sendiri di id.data_keys — termasuk
// bahwa custody-nya dijawab TANPA baris di id.tenant_registry (ADR-017 §3).
func newIdentityCryptoSvc(t *testing.T, pool *infradb.Pool) *crypto.Service {
	t.Helper()
	svc, err := crypto.NewFromConfig(&config.AppConfig{
		Env: "production", // driver static = jalur produksi Tier 1/2, bukan jalur dev
		Crypto: config.CryptoConfig{
			KMSDriver:   crypto.DriverStatic,
			MasterKey:   base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x3C}, 32)),
			DEKCacheTTL: time.Minute,
		},
	}, pool)
	if err != nil {
		t.Fatalf("crypto.NewFromConfig: %v", err)
	}
	return svc
}

// Konstruktor repo identity kini mengembalikan error (CryptoPort wajib). Helper ini
// menjaga test tetap terbaca tanpa menyembunyikan kewajiban itu.
func mustPersonRepo(t *testing.T, pool *infradb.Pool, c port.CryptoPort) *db.PersonRepo {
	t.Helper()
	r, err := db.NewPersonRepo(pool, c)
	if err != nil {
		t.Fatalf("NewPersonRepo: %v", err)
	}
	return r
}

func mustEmploymentRepo(t *testing.T, pool *infradb.Pool, c port.CryptoPort) *db.EmploymentRepo {
	t.Helper()
	r, err := db.NewEmploymentRepo(pool, c)
	if err != nil {
		t.Fatalf("NewEmploymentRepo: %v", err)
	}
	return r
}

func mustCredentialRepo(t *testing.T, pool *infradb.Pool, c port.CryptoPort) *db.CredentialRepo {
	t.Helper()
	r, err := db.NewCredentialRepo(pool, c)
	if err != nil {
		t.Fatalf("NewCredentialRepo: %v", err)
	}
	return r
}

func mustAuditedPersonRepo(t *testing.T, inner domain.PersonRepository, e *audit.Engine, c port.CryptoPort) domain.PersonRepository {
	t.Helper()
	r, err := db.NewAuditedPersonRepo(inner, e, c)
	if err != nil {
		t.Fatalf("NewAuditedPersonRepo: %v", err)
	}
	return r
}

func mustAuditedEmploymentRepo(t *testing.T, inner domain.EmploymentRepository, e *audit.Engine, c port.CryptoPort) domain.EmploymentRepository {
	t.Helper()
	r, err := db.NewAuditedEmploymentRepo(inner, e, c)
	if err != nil {
		t.Fatalf("NewAuditedEmploymentRepo: %v", err)
	}
	return r
}

func TestIdentityRepos_CreatePersonEmploymentCredential(t *testing.T) {
	pool, cr, ctx := setupIdentityDB(t)
	persons := mustPersonRepo(t, pool, cr)
	employments := mustEmploymentRepo(t, pool, cr)
	credentials := mustCredentialRepo(t, pool, cr)

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
	pool, cr, ctx := setupIdentityDB(t)
	persons := mustPersonRepo(t, pool, cr)
	employments := mustEmploymentRepo(t, pool, cr)

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
	pool, cr, ctx := setupIdentityDB(t)
	persons := mustPersonRepo(t, pool, cr)

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
