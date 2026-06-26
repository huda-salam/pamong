//go:build integration

package db_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core/audit"
	"github.com/huda-salam/pamong/core/permission"
	deldb "github.com/huda-salam/pamong/delegation/adapter/db"
	"github.com/huda-salam/pamong/delegation/domain"
	"github.com/huda-salam/pamong/delegation/usecase"
	infradb "github.com/huda-salam/pamong/infra/db"
	"github.com/huda-salam/pamong/testkit"
	"github.com/jackc/pgx/v5/pgxpool"
)

const permSpmBaca = "keuangan:spm:baca"

// setupDelegationDB membuka pool ke DB uji lalu membersihkan gov.delegations PER-TABEL.
// JANGAN DROP SCHEMA gov CASCADE: schema dipakai bersama lintas-paket (gov.audit_logs).
func setupDelegationDB(t *testing.T) (*infradb.Pool, context.Context) {
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
	drop := func() { _, _ = pool.Exec(context.Background(), `DROP TABLE IF EXISTS gov.delegations`) }
	drop()
	t.Cleanup(func() {
		drop()
		pgpool.Close()
	})
	return pool, ctx
}

// TestDelegation_ScopedGrant_EndToEnd membuktikan delegasi aktif memberi scoped-grant ke
// delegatee yang menembus ScopedEngine (jalur delegasi mandiri — delegatee tak punya role),
// dan DoD PR-2.3.5b: delegasi kedaluwarsa otomatis tak berlaku.
func TestDelegation_ScopedGrant_EndToEnd(t *testing.T) {
	pool, ctx := setupDelegationDB(t)
	repo := deldb.NewDelegationRepo(pool)
	grants := deldb.NewDelegationScopedGrantResolver(repo)
	// Engine tanpa role apa pun → jalur role pasti gagal; hanya delegasi yang bisa mengizinkan.
	eng := permission.NewScopedEngine(permission.NewEngine(permission.NewMemoryCatalog()), nopHierarchy{})

	actx := testkit.Ctx(t, testkit.WithPermission(domain.PermDelegasiBuat))
	pejabat, plt := uuid.New(), uuid.New()
	unit := uuid.New()

	// Delegasi AKTIF: pejabat → PLT, perm baca, scope unit, window aktif.
	if _, err := usecase.NewCreateDelegation(repo, domain.NewNonDelegableSet()).Execute(actx, usecase.CreateDelegationInput{
		FromUserID: pejabat, ToUserID: plt, Permissions: []string{permSpmBaca},
		UnitKerjaID: &unit, ValidUntil: time.Now().Add(24 * time.Hour),
	}); err != nil {
		t.Fatalf("create delegasi aktif: %v", err)
	}

	gr, err := grants.Grants(ctx, plt)
	if err != nil {
		t.Fatalf("resolve grants: %v", err)
	}
	auth := permission.Authority{DelegatedGrants: gr}
	ok, err := eng.AllowsInUnit(ctx, auth, permSpmBaca, permission.ResourceScope{UnitKerjaID: unit})
	if err != nil {
		t.Fatalf("AllowsInUnit: %v", err)
	}
	if !ok {
		t.Error("delegatee dengan delegasi aktif harus boleh akses di unit delegasi")
	}
	// Di luar scope unit → ditolak.
	if ok, _ := eng.AllowsInUnit(ctx, auth, permSpmBaca, permission.ResourceScope{UnitKerjaID: uuid.New()}); ok {
		t.Error("delegasi unit-scope tak boleh menjangkau unit lain")
	}

	// DoD b — delegasi KEDALUWARSA: valid_until lampau → tak ikut ter-resolve → akses hilang.
	plt2 := uuid.New()
	past := time.Now().Add(-time.Hour)
	if _, err := usecase.NewCreateDelegation(repo, domain.NewNonDelegableSet()).Execute(actx, usecase.CreateDelegationInput{
		FromUserID: pejabat, ToUserID: plt2, Permissions: []string{permSpmBaca},
		UnitKerjaID: &unit, ValidFrom: past.Add(-time.Hour), ValidUntil: past,
	}); err != nil {
		t.Fatalf("create delegasi kedaluwarsa: %v", err)
	}
	grExpired, err := grants.Grants(ctx, plt2)
	if err != nil {
		t.Fatalf("resolve grants kedaluwarsa: %v", err)
	}
	if len(grExpired) != 0 {
		t.Fatalf("delegasi kedaluwarsa tak boleh memberi grant, dapat: %v", grExpired)
	}
}

// TestDelegation_Audited membuktikan pembuatan delegasi ter-audit otomatis ke gov.audit_logs
// tenant DB (ADR-003 / PRD F5), tanpa kode audit di use case.
func TestDelegation_Audited(t *testing.T) {
	pool, ctx := setupDelegationDB(t)

	auditRepo := infradb.NewAuditRepo(pool) // schema gov
	if err := auditRepo.EnsureSchema(ctx); err != nil {
		t.Fatalf("ensure audit schema: %v", err)
	}
	engine := audit.NewEngine(auditRepo)
	repo := deldb.NewAuditedDelegationRepo(deldb.NewDelegationRepo(pool), engine)

	actor := uuid.New()
	tenantID := "tenant-deleg-" + uuid.NewString()
	actx := testkit.Ctx(t,
		testkit.WithPersonID(actor),
		testkit.WithTenant(tenantID),
		testkit.WithPermission(domain.PermDelegasiBuat),
	)
	d, err := usecase.NewCreateDelegation(repo, domain.NewNonDelegableSet()).Execute(actx, usecase.CreateDelegationInput{
		FromUserID: uuid.New(), ToUserID: uuid.New(), Permissions: []string{permSpmBaca},
		ValidUntil: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("create delegasi: %v", err)
	}

	entries, err := auditRepo.ByEntity(ctx, "delegation.Delegation", d.ID)
	if err != nil {
		t.Fatalf("byEntity: %v", err)
	}
	if len(entries) != 1 || entries[0].Action != audit.ActionCreate || entries[0].ActorID != actor {
		t.Fatalf("audit pembuatan delegasi salah: %+v", entries)
	}
}

// nopHierarchy: tak ada keturunan (uji delegasi tak butuh subtree).
type nopHierarchy struct{}

func (nopHierarchy) IsWithin(_ context.Context, root, unit uuid.UUID) (bool, error) {
	return root == unit, nil
}
