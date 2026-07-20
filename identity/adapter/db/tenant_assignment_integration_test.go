//go:build integration

package db_test

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core/audit"
	"github.com/huda-salam/pamong/identity/adapter/db"
	"github.com/huda-salam/pamong/identity/domain"
	"github.com/huda-salam/pamong/identity/usecase"
	infradb "github.com/huda-salam/pamong/infra/db"
	"github.com/huda-salam/pamong/testkit"
)

// applyAssignmentMigration menerapkan migrasi 003 (id.tenant_assignments) di atas schema
// id yang sudah disiapkan setupIdentityDB (001).
func applyAssignmentMigration(t *testing.T, pool *infradb.Pool, ctx context.Context) {
	t.Helper()
	upSQL, err := os.ReadFile("../../migrations/003_create_tenant_assignments.up.sql")
	if err != nil {
		t.Fatalf("baca migrasi 003: %v", err)
	}
	if _, err := pool.Exec(ctx, string(upSQL)); err != nil {
		t.Fatalf("apply migrasi 003: %v", err)
	}
}

func TestTenantAssignmentRepo_SaveAndList(t *testing.T) {
	pool, ctx := setupIdentityDB(t)
	applyAssignmentMigration(t, pool, ctx)

	persons := db.NewPersonRepo(pool)
	employments := db.NewEmploymentRepo(pool)
	assignments := db.NewTenantAssignmentRepo(pool)

	// Person + employment (FK target untuk assignment).
	p := &domain.Person{ID: uuid.New(), NIK: "3578010101900001", NamaLengkap: "Budi", IsActive: true}
	if err := persons.Save(ctx, p); err != nil {
		t.Fatalf("save person: %v", err)
	}
	emp := &domain.Employment{
		ID: uuid.New(), PersonID: p.ID, Status: domain.StatusASN, NIP: "199001012015011001", IsActive: true,
	}
	if err := employments.Save(ctx, emp); err != nil {
		t.Fatalf("save employment: %v", err)
	}

	a := &domain.TenantAssignment{
		ID: uuid.New(), EmploymentID: emp.ID, TenantID: "pemkot-surabaya",
		IsHomeTenant: true, AssignedBy: p.ID,
	}
	if err := assignments.Save(ctx, a); err != nil {
		t.Fatalf("save assignment: %v", err)
	}

	list, err := assignments.ListByEmployment(ctx, emp.ID)
	if err != nil {
		t.Fatalf("listByEmployment: %v", err)
	}
	if len(list) != 1 || list[0].TenantID != "pemkot-surabaya" || !list[0].IsHomeTenant {
		t.Fatalf("assignment tidak sesuai: %+v", list)
	}
}

// TestTenantAssignment_Audited membuktikan penugasan ke tenant lewat use case ter-audit
// otomatis (ADR-003: semua mutasi identity ter-audit), tanpa kode audit di use case.
func TestTenantAssignment_Audited(t *testing.T) {
	pool, ctx := setupIdentityDB(t)
	applyAssignmentMigration(t, pool, ctx)
	// Registry tenant dibutuhkan sejak PR-2.4.5: validateAssignment menolak penugasan
	// ke tenant yang tidak terdaftar atau tidak aktif.
	applyTenantMigration(t, pool)

	auditStore := db.NewAuditStore(pool)
	if err := auditStore.EnsureSchema(ctx); err != nil {
		t.Fatalf("ensure audit schema: %v", err)
	}
	engine := audit.NewEngine(auditStore)

	// Repo dibungkus dekorator audit.
	pub := testkit.NewMockPublisher()
	persons := db.NewAuditedPersonRepo(db.NewPersonRepo(pool), engine)
	employments := db.NewAuditedEmploymentRepo(db.NewEmploymentRepo(pool), engine)
	assignments := db.NewAuditedTenantAssignmentRepo(db.NewTenantAssignmentRepo(pool), engine)
	registry := db.NewTenantRepo(pool)

	// Tenant tujuan harus terdaftar & aktif agar validateAssignment lulus.
	if err := registry.Save(ctx, &domain.Tenant{
		TenantID: "pemkot-surabaya", Nama: "Pemkot Surabaya", Tier: 1,
		DBHost: "db1", DBName: "gov_pemkot_surabaya", IsActive: true,
	}); err != nil {
		t.Fatalf("seed tenant registry: %v", err)
	}

	// Actor penugasan harus eksis sebagai person (FK assigned_by → id.persons).
	actor := uuid.New()
	if err := db.NewPersonRepo(pool).Save(ctx, &domain.Person{
		ID: actor, NIK: "3500000000000001", NamaLengkap: "Admin BKD", IsActive: true,
	}); err != nil {
		t.Fatalf("seed actor person: %v", err)
	}
	actx := testkit.Ctx(t,
		testkit.WithPersonID(actor),
		testkit.WithPermission(domain.PermPersonBuat),
		testkit.WithPermission(domain.PermEmploymentLampir),
		testkit.WithPermission(domain.PermAssignmentTugaskan),
	)

	p, err := usecase.NewCreatePerson(persons, pub).Execute(actx, usecase.CreatePersonInput{
		NIK: "3578010101900001", NamaLengkap: "Budi",
	})
	if err != nil {
		t.Fatalf("create person: %v", err)
	}
	emp, err := usecase.NewAttachEmployment(persons, employments, pub).Execute(actx, usecase.AttachEmploymentInput{
		PersonID: p.ID, Status: domain.StatusASN, NIP: "199001012015011001",
	})
	if err != nil {
		t.Fatalf("attach employment: %v", err)
	}

	a, err := usecase.NewAssignEmploymentToTenant(persons, employments, assignments, registry, pub).Execute(actx,
		usecase.AssignEmploymentToTenantInput{EmploymentID: emp.ID, TenantID: "pemkot-surabaya"})
	if err != nil {
		t.Fatalf("assign tenant: %v", err)
	}

	entries, err := auditStore.ByEntity(ctx, "identity.TenantAssignment", a.ID)
	if err != nil {
		t.Fatalf("byEntity assignment: %v", err)
	}
	if len(entries) != 1 || entries[0].Action != audit.ActionCreate || entries[0].ActorID != actor {
		t.Fatalf("audit penugasan salah: %+v", entries)
	}
	var tenantRecorded bool
	for _, d := range entries[0].Diff {
		if d.Field == "tenant_id" && d.After == "pemkot-surabaya" {
			tenantRecorded = true
		}
	}
	if !tenantRecorded {
		t.Fatalf("penugasan harus mencatat tenant_id di diff: %+v", entries[0].Diff)
	}
}
